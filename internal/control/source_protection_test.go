package control

import (
	"bytes"
	"encoding/json"
	"github.com/imzyb/MiGate/internal/subscription"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSourceDropUpstreamComparison(t *testing.T) {
	if os.Getenv("MIGATE_UPSTREAM_TESTS") != "1" {
		t.Skip("local pinned upstream oracle")
	}
	pairs := [][2]int{{0, 0}, {9, 0}, {10, 0}, {10, 2}, {10, 3}, {11, 3}, {11, 4}, {100, 29}, {100, 30}, {10000, 2999}, {10000, 3000}, {10, 20}}
	input, _ := json.Marshal(pairs)
	cmd := exec.Command("node", filepath.Join("..", "..", "third_party", "misub", "reference", "compare-cache-drop.mjs"))
	cmd.Stdin = bytes.NewReader(input)
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var expected []bool
	if err = json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	for i, p := range pairs {
		if suspiciousSourceDrop(p[0], p[1]) != expected[i] {
			t.Fatalf("upstream mismatch %v", p)
		}
	}
}

func TestSourceProtectionRequestAndFallback(t *testing.T) {
	now := time.Now().UTC()
	d := map[string]any{"url": "https://example.com/sub?key=a%2Bb&_t=old", "node_cache": true, "last_update": now.Add(-2 * time.Hour).Format(time.RFC3339), "links": []string{"trojan://synthetic@example.com:443#healthy"}, "last_error": "refresh_failed"}
	u, _ := url.Parse(sourceRequestAddress(d, now))
	if u.Query().Get("key") != "a+b" || len(u.Query()["_t"]) != 1 || u.Query().Get("_t") == "old" {
		t.Fatal("request parameters changed")
	}
	if links, stale, err := sourceLinks(d, now); err != nil || !stale || len(links) != 1 {
		t.Fatal("bounded protection unavailable", err)
	}
	d["max_stale_seconds"] = 3600
	if _, _, err := sourceLinks(d, now); err == nil {
		t.Fatal("expired protected cache served")
	}
	d["node_cache"] = false
	if sourceRequestAddress(d, now) != d["url"] {
		t.Fatal("disabled protection changed URL")
	}
	nodes := []subscription.ParsedLink{{Address: "localhost"}, {Address: "127.0.0.2"}, {Address: "::1"}, {Address: "0.0.0.0"}, {Address: "example.com"}, {Address: "8.8.8.8"}}
	if len(realSourceNodes(nodes)) != 2 {
		t.Fatal("local placeholder nodes retained")
	}
}
