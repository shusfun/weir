package center

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/subgrant"
	"github.com/shusfun/weir/internal/subgroup"
	"github.com/shusfun/weir/internal/subscription"
)

func TestPreviewNodesUsesCache(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(500)
	}))
	defer upstream.Close()
	s.AllowPrivateSources = true
	cached := s.Store.Ent.SubSource.Create().SetName("机场A").SetURL(upstream.URL + "/a").SetNodeCache(true).SaveX(ctx)
	empty := s.Store.Ent.SubSource.Create().SetName("机场B").SetURL(upstream.URL + "/b").SaveX(ctx)
	raw := []string{
		"trojan://p1@198.51.100.1:443?sni=example.com#香港 01 x0.5",
		"trojan://p2@198.51.100.2:443?sni=example.com#香港 02 x2",
		"trojan://p3@198.51.100.3:443?sni=example.com#日本 01",
		"trojan://p4@198.51.100.4:443?sni=example.com#香港 03",
	}
	source := toSource(cached)
	kvCache{s}.ForSource(source, subgroup.SourceUserAgent(source, "clash-meta/2.5.0")).Store(ctx, cached.ID, raw)
	latency := 42
	checked := time.Now().UTC()
	if err := s.saveSourceHealth(ctx, sourceHealth{SourceID: cached.ID, Generation: sourceGeneration(raw), CheckedAt: &checked, Location: "center",
		Nodes: map[string]sourceNodeHealth{
			subscription.InventoryIdentity(raw[0]): {Status: nodeAlive, LatencyMS: &latency},
			subscription.InventoryIdentity(raw[3]): {Status: nodeFailed, Why: "timeout"},
		}}); err != nil {
		t.Fatal(err)
	}

	draft := map[string]any{"name": "草稿", "source_ids": []int{cached.ID, empty.ID}, "settings": map[string]any{
		"pick": map[string]any{"regions": []string{"香港"}, "max_multiplier": 1, "include_unlabeled_multiplier": true, "only_alive": true}}}
	out := mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/profiles/preview-nodes", draft, 200)
	if hits.Load() != 0 {
		t.Fatalf("preview fetched upstream %d times", hits.Load())
	}
	nodes := out["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %v", nodes)
	}
	node := nodes[0].(map[string]any)
	if node["name"] != "🇭🇰 机场A - 香港 01 x0.5" || node["region"] != "香港" || node["multiplier"].(float64) != 0.5 || node["status"] != nodeAlive ||
		node["latency_ms"].(float64) != 42 || node["source_id"].(float64) != float64(cached.ID) || node["source_name"] != "机场A" {
		t.Fatalf("node = %v", node)
	}
	removed := map[string]float64{}
	for _, step := range out["steps"].([]any) {
		m := step.(map[string]any)
		removed[m["step"].(string)] = m["removed"].(float64)
	}
	if removed[subgroup.StepRegion] != 1 || removed[subgroup.StepMultiplier] != 1 || removed[subgroup.StepAlive] != 1 {
		t.Fatalf("steps = %v", out["steps"])
	}
	if out["merged"].(float64) != 4 || len(out["available"].(map[string]any)["regions"].([]any)) != 2 {
		t.Fatalf("summary = %v %v", out["merged"], out["available"])
	}
	var missing map[string]any
	for _, item := range out["sources"].([]any) {
		if m := item.(map[string]any); m["source_id"].(float64) == float64(empty.ID) {
			missing = m
		}
	}
	if missing == nil || missing["error"] != "source_not_cached" {
		t.Fatalf("uncached source = %v", out["sources"])
	}
	// 预览不改源状态，也不写输出缓存。
	if got := s.Store.Ent.SubSource.GetX(ctx, empty.ID); got.LastError != "" {
		t.Fatalf("preview recorded source error: %q", got.LastError)
	}
	// 坏的挑选条件在入口拒绝。
	draft["settings"] = map[string]any{"pick": map[string]any{"max_multiplier": 0}}
	mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/profiles/preview-nodes", draft, 400)
	draft["settings"] = map[string]any{"pick": map[string]any{"unknown": true}}
	mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/profiles/preview-nodes", draft, 400)
	if hits.Load() != 0 {
		t.Fatalf("preview fetched upstream %d times", hits.Load())
	}
}

func TestGrantsBothSidesKeepTokens(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	a := s.Store.Ent.User.Create().SetEmail("both-a@example.test").SetName("甲").SetPasswordHash("fixture").SaveX(ctx)
	b := s.Store.Ent.User.Create().SetEmail("both-b@example.test").SetName("乙").SetPasswordHash("fixture").SaveX(ctx)
	p1 := s.Store.Ent.SubProfile.Create().SetName("日常").SaveX(ctx)
	p2 := s.Store.Ent.SubProfile.Create().SetName("游戏").SaveX(ctx)
	token := func(pid, uid int) string {
		t.Helper()
		g, err := s.Store.Ent.SubGrant.Query().Where(subgrant.ProfileIDEQ(pid), subgrant.UserIDEQ(uid)).Only(ctx)
		if err != nil {
			return ""
		}
		return g.Token
	}

	mustJSON(t, admin, "PUT", fmt.Sprintf("/api/v1/admin/subscriptions/profiles/%d/grants", p1.ID), map[string]any{"user_ids": []int{a.ID, b.ID}}, 200)
	keepA, keepB := token(p1.ID, a.ID), token(p1.ID, b.ID)
	if keepA == "" || keepB == "" || keepA == keepB {
		t.Fatal("profile side did not create independent grants")
	}

	// 从用户一侧加上 p2：p1 的令牌不变。
	view := mustJSON(t, admin, "PUT", fmt.Sprintf("/api/v1/admin/users/%d/subscription-groups", a.ID), map[string]any{"profile_ids": []int{p1.ID, p2.ID}}, 200)
	granted := 0
	for _, item := range view["items"].([]any) {
		if item.(map[string]any)["granted"] == true {
			granted++
		}
	}
	if granted != 2 || token(p1.ID, a.ID) != keepA || token(p2.ID, a.ID) == "" {
		t.Fatal("user side rotated or missed grants")
	}
	keepA2 := token(p2.ID, a.ID)

	// 从订阅组一侧去掉乙：甲在两个组里的令牌都不变。
	mustJSON(t, admin, "PUT", fmt.Sprintf("/api/v1/admin/subscriptions/profiles/%d/grants", p1.ID), map[string]any{"user_ids": []int{a.ID}}, 200)
	if token(p1.ID, b.ID) != "" || token(p1.ID, a.ID) != keepA || token(p2.ID, a.ID) != keepA2 {
		t.Fatal("profile side touched unrelated grants")
	}

	// 从用户一侧去掉 p1：只删这一条。
	mustJSON(t, admin, "PUT", fmt.Sprintf("/api/v1/admin/users/%d/subscription-groups", a.ID), map[string]any{"profile_ids": []int{p2.ID}}, 200)
	if token(p1.ID, a.ID) != "" || token(p2.ID, a.ID) != keepA2 {
		t.Fatal("user side removal touched kept grant")
	}
	got := mustJSON(t, admin, "GET", fmt.Sprintf("/api/v1/admin/users/%d/subscription-groups", a.ID), nil, 200)
	if items := got["items"].([]any); len(items) != 2 {
		t.Fatalf("items = %v", items)
	}

	mustJSON(t, admin, "PUT", fmt.Sprintf("/api/v1/admin/users/%d/subscription-groups", a.ID), map[string]any{"profile_ids": []int{99999}}, 400)
	mustJSON(t, admin, "PUT", "/api/v1/admin/users/99999/subscription-groups", map[string]any{"profile_ids": []int{}}, 404)
	if token(p2.ID, a.ID) != keepA2 {
		t.Fatal("rejected request changed grants")
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "both-c@example.test", "password": "Passw0rd"}, 200)
	viewer := login(t, s, "both-c@example.test", "Passw0rd")
	mustJSON(t, viewer, "PUT", fmt.Sprintf("/api/v1/admin/users/%d/subscription-groups", a.ID), map[string]any{"profile_ids": []int{}}, 403)
}
