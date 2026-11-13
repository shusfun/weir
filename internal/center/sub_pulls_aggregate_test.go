package center

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store"
)

func TestLinkStatsAggregateIsBoundedAndIsolated(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx, now := t.Context(), time.Now().UTC()
	a := s.Store.Ent.User.Create().SetEmail("stats-a@example.test").SetPasswordHash("fixture").SaveX(ctx)
	b := s.Store.Ent.User.Create().SetEmail("stats-b@example.test").SetPasswordHash("fixture").SaveX(ctx)
	for _, uid := range []int{a.ID, b.ID} {
		for i := 0; i < 25; i++ {
			l := s.Store.Ent.Link.Create().SetUserID(uid).SetToken(randomToken(16)).SaveX(ctx)
			for j := 0; j < 5; j++ {
				s.Store.Ent.SubPull.Create().SetLinkID(l.ID).SetUserID(uid).SetAt(now.Add(-time.Duration(j) * time.Hour)).SetIP("192.0.2.1").SetUserAgent("FlClash clash-verge").SaveX(ctx)
			}
			s.Store.Ent.SubPull.Create().SetLinkID(l.ID).SetUserID(uid).SetAt(now.Add(-48 * time.Hour)).SetIP("192.0.2.2").SetStatus("expired").SetUserAgent("unknown-client").SaveX(ctx)
			s.Store.Ent.SubPull.Create().SetLinkID(l.ID).SetUserID(uid).SetAt(now.Add(-31 * 24 * time.Hour)).SaveX(ctx)
		}
	}
	measured, metrics := store.ObserveQueries(ctx)
	items, err := s.collectLinkStats(measured, a.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 25 || metrics.Count.Load() != 6 {
		t.Fatalf("items=%d queries=%d; want 25 and 6", len(items), metrics.Count.Load())
	}
	for _, row := range items {
		if row["user_id"] != a.ID || row["pulls_30d"] != 6 || row["pulls_24h"] != 5 || row["refused_30d"] != 1 || row["sources_30d"] != 2 {
			t.Fatalf("incorrect projection: %v", row)
		}
		clients := row["clients"].([]clientCount)
		if len(clients) != 2 || clients[0].Name != "FlClash" || clients[0].Count != 5 || clients[1].Name != "未识别" {
			t.Fatalf("client recognition changed: %v", clients)
		}
		last := row["last_pull"].(gin.H)
		if _, leaked := last["ip"]; leaked {
			t.Fatal("model projection leaked source")
		}
	}
}
