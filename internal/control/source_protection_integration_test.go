package control

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/imzyb/MiGate/internal/subscription"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testSourceProtectionIntegration(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()
	r := s.Store.Resource.Create().SetKind("sources").SetKey(randomToken(16)).SetData(map[string]any{"url": "https://example.com/sub?key=a%2Bb", "node_cache": true, "max_stale_seconds": 3600}).SaveX(ctx)
	defer s.Store.Resource.DeleteOne(r).Exec(ctx)
	n := 10
	body := ""
	calls := 0
	fetch := func(_ context.Context, address, ua string) (subscription.FetchedInventory, error) {
		calls++
		u, _ := url.Parse(address)
		if u.Query().Get("key") != "a+b" || u.Query().Get("_t") == "" {
			t.Error("protective fetch address invalid")
		}
		text := body
		if text == "" {
			for i := 0; i < n; i++ {
				text += fmt.Sprintf("trojan://synthetic@example.com:443#node-%d\n", i)
			}
		}
		return subscription.FetchedInventory{Body: []byte(text), Usage: subscription.ParseUsageMetadata([]string{fmt.Sprintf("download=%d;total=100", calls)})}, nil
	}
	r, err := s.refreshSourceCache(ctx, r.ID, fetch)
	if err != nil {
		t.Fatal(err)
	}
	good := r.Data["last_update"]
	n = 2
	r, err = s.refreshSourceCache(ctx, r.ID, fetch)
	if err == nil || r.Data["last_update"] != good || number(r.Data["node_count"]) != 10 || r.Data["last_rejection"] != "suspicious_node_drop" {
		t.Fatal("shrink replaced known-good cache", err)
	}
	var usage subscription.UsageMetadata
	raw, _ := json.Marshal(r.Data["upstream_usage"])
	json.Unmarshal(raw, &usage)
	if usage.Download == nil || *usage.Download != "1" {
		t.Fatal("refused response overwrote usage")
	}
	links, stale, err := sourceLinks(r.Data, time.Now())
	if err != nil || !stale || len(links) != 10 {
		t.Fatal("refused response unavailable fallback", err)
	}
	body = "trojan://synthetic@127.0.0.1:443#traffic"
	r, err = s.refreshSourceCache(ctx, r.ID, fetch)
	if err == nil || number(r.Data["node_count"]) != 10 {
		t.Fatal("placeholder overwrote cache")
	}
	body = ""
	n = 3
	r, err = s.refreshSourceCache(ctx, r.ID, fetch)
	if err != nil || number(r.Data["node_count"]) != 3 || r.Data["last_rejection"] != nil || strings.TrimSpace(text(r.Data["last_error"])) != "" {
		t.Fatal("30 percent boundary or recovery failed", err)
	}
}
