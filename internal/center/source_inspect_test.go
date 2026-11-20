package center

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/subgroup"
	"github.com/shusfun/weir/internal/subscription"
	"github.com/shusfun/weir/packages/contracts"
)

func trojanLine(i int, name string) string {
	return fmt.Sprintf("trojan://p%d@198.51.100.%d:443?sni=example.com#%s", i, i, name)
}

// 没有可用 Gate 时：首选与默认 Gate 都记下跳过原因，最后由中心直连，并标明未检测。
func TestSourceFetchPlanOrder(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	offline := pairFakeGate(t, admin)
	s.Store.Ent.Gate.UpdateOneID(int(offline.id)).SetName("东京").ClearLastSeenAt().ExecX(ctx)
	route, err := s.fetchRouteFor(ctx, int(offline.id))
	if err != nil {
		t.Fatal(err)
	}
	if len(route.gates) != 0 || len(route.notes) != 1 || !strings.Contains(route.notes[0], "东京") {
		t.Fatalf("offline preferred gate must be skipped with a reason: %+v", route)
	}
	s.AllowPrivateSources = true
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(trojanLine(1, "HK-01")))
	}))
	defer upstream.Close()
	resp, err := s.fetchSubscriptionRouted(ctx, upstream.URL, "clash-meta/2.5.0", route)
	if err != nil || resp.Location != "center-fallback" || !strings.Contains(resp.RouteReason, "东京") || !strings.Contains(resp.RouteReason, "中心直接下载") {
		t.Fatalf("fallback location=%q reason=%q err=%v", resp.Location, resp.RouteReason, err)
	}
}

// 中心兜底拉取不做检测：全部节点记为未检测，并写明原因。
func TestCenterFallbackMarksUnchecked(t *testing.T) {
	s, _ := newInstalledServer(t)
	raw := []string{trojanLine(1, "香港 01 x2"), trojanLine(2, "日本 02")}
	in, err := s.probeSourceNodes(t.Context(), 7, raw, "center-fallback", nil)
	if err != nil {
		t.Fatal(err)
	}
	if in.Summary.Total != 2 || in.Summary.Unchecked != 2 || in.Summary.Alive != 0 || in.Summary.Reason == "" {
		t.Fatalf("summary=%+v", in.Summary)
	}
	if in.Summary.Multipliers["2"] != 1 || in.Summary.Multipliers["未标注"] != 1 {
		t.Fatalf("multipliers=%v", in.Summary.Multipliers)
	}
}

// 检测结果按连接身份与内容版本对齐：改名仍对得上，地址变了算新节点，内容版本变了旧结果作废。
func TestHealthKeyedByIdentityAndGeneration(t *testing.T) {
	a := trojanLine(1, "HK-01")
	renamed := trojanLine(1, "香港-01")
	if subscription.InventoryIdentity(a) != subscription.InventoryIdentity(renamed) {
		t.Fatal("rename must keep the connection identity")
	}
	moved := trojanLine(9, "HK-01")
	if subscription.InventoryIdentity(a) == subscription.InventoryIdentity(moved) {
		t.Fatal("address change must be a new node")
	}
	gen := sourceGeneration([]string{a})
	h := &sourceHealth{Generation: gen, Nodes: map[string]sourceNodeHealth{subscription.InventoryIdentity(a): {Status: nodeAlive}}}
	if h.nodeStatusFor(gen, subscription.InventoryIdentity(renamed)).Status != nodeAlive {
		t.Fatal("same generation must match by identity")
	}
	if h.nodeStatusFor(sourceGeneration([]string{renamed}), subscription.InventoryIdentity(renamed)).Status != nodeUnchecked {
		t.Fatal("changed content must invalidate old results")
	}
}

// 两次拉取之间的变化按身份统计：新增、移除、改名。
func TestSourceChangeSummary(t *testing.T) {
	prev := sourceHealth{Nodes: map[string]sourceNodeHealth{"a": {Name: "A"}, "b": {Name: "B"}, "c": {Name: "C"}}}
	next := sourceHealth{Nodes: map[string]sourceNodeHealth{"a": {Name: "A"}, "b": {Name: "B2"}, "d": {Name: "D"}, "e": {Name: "E"}}}
	ch := diffSourceNodes(&prev, next)
	if ch.Added != 2 || ch.Removed != 1 || ch.Renamed != 1 {
		t.Fatalf("change=%+v", ch)
	}
	if got := diffSourceNodes(nil, next); got.Added != 0 || got.Removed != 0 {
		t.Fatalf("first fetch has no baseline: %+v", got)
	}
}

// 按源刷新：源自己的间隔优先，未设置跟随全局，两者都为 0 不自动刷新；从没尝试过立即到期。
func TestRefreshSourcesPerSourceInterval(t *testing.T) {
	now := time.Now()
	past := now.Add(-3 * time.Hour)
	cases := []struct {
		name        string
		src         ent.SubSource
		global      int
		wantHours   int
		wantDue     bool
		wantNoRefsh bool
	}{
		{"own interval", ent.SubSource{Enabled: true, RefreshHours: 2, LastAttemptAt: &past}, 24, 2, true, false},
		{"follow global", ent.SubSource{Enabled: true, LastAttemptAt: &past}, 24, 24, false, false},
		{"never attempted", ent.SubSource{Enabled: true}, 6, 6, true, false},
		{"disabled auto", ent.SubSource{Enabled: true, LastAttemptAt: &past}, 0, 0, false, true},
		{"source disabled", ent.SubSource{Enabled: false, RefreshHours: 1}, 1, 1, false, true},
	}
	for _, c := range cases {
		hours := sourceRefreshHours(&c.src, SubscriptionSettings{SourceRefresh: c.global})
		next := sourceNextRefresh(&c.src, hours)
		immediate := next != nil && c.src.LastAttemptAt == nil && next.Sub(now) < time.Second
		due := next != nil && (immediate || !next.After(now))
		if hours != c.wantHours || (next == nil) != c.wantNoRefsh || due != c.wantDue {
			t.Errorf("%s: hours=%d next=%v", c.name, hours, next)
		}
	}
}

// 试拉后保存：带上 inspection_id 时不再拉取，节点数、检测摘要与首选 Gate 都写入。
func TestInspectThenSaveReusesResult(t *testing.T) {
	s, admin := newInstalledServer(t)
	s.AllowPrivateSources = true
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=1073741824; expire=1999999999")
		lines := []string{trojanLine(1, "香港 01"), trojanLine(2, "日本 02 x0.5")}
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))))
	}))
	defer upstream.Close()
	in := mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/sources/inspect", map[string]any{"url": upstream.URL}, 200)
	if in["ok"] != true || len(in["nodes"].([]any)) != 2 || in["location"] != "center-fallback" {
		t.Fatalf("inspect=%v", in)
	}
	saved := mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/sources", map[string]any{"name": "机场", "url": upstream.URL, "inspection_id": in["inspection_id"], "refresh_hours": 6}, 200)
	if hits.Load() != 1 {
		t.Fatalf("save with inspection must not refetch, hits=%d", hits.Load())
	}
	if saved["node_count"].(float64) != 2 || saved["refresh_hours"].(float64) != 6 || saved["refreshing"] == true {
		t.Fatalf("saved=%v", saved)
	}
	if h, _ := saved["health"].(map[string]any); h == nil || h["total"].(float64) != 2 {
		t.Fatalf("health=%v", saved["health"])
	}
}

func TestSourceInspectionRejectsChangedInputsAndOlderResults(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	first, second := pairFakeGate(t, admin), pairFakeGate(t, admin)
	upstreamURL := "https://source.example/subscription"
	raw := []string{trojanLine(1, "HK-01")}
	started := time.Now().UTC().Add(-time.Minute)
	inspection := storedInspection{URL: upstreamURL, UserAgent: "release-check", Started: started, Raw: raw,
		Result: subgroup.SourceResult{Nodes: 1, Location: "gate:" + itoa(int(first.id))},
		Health: sourceHealth{Generation: sourceGeneration(raw)}}
	blob, err := json.Marshal(inspection)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, address, ua string
		gateID            int
		newer             bool
		want              bool
	}{
		{"automatic", upstreamURL, "release-check", 0, false, true},
		{"adopt-actual-gate", upstreamURL, "release-check", int(first.id), false, true},
		{"different-gate", upstreamURL, "release-check", int(second.id), false, false},
		{"different-url", upstreamURL + "/changed", "release-check", int(first.id), false, false},
		{"different-ua", upstreamURL, "other-agent", int(first.id), false, false},
		{"newer-refresh", upstreamURL, "release-check", int(first.id), true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := s.Store.Ent.SubSource.Create().SetName(tt.name).SetURL(tt.address).SetUserAgent(tt.ua).SetGateID(tt.gateID).SaveX(ctx)
			if tt.newer {
				if err := s.saveCatalogRefreshStarted(ctx, src.ID, started.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Store.KV.Set(ctx, sourceInspectionKey(tt.name), string(blob), sourceInspectionTTL); err != nil {
				t.Fatal(err)
			}
			applied, err := s.applyInspection(ctx, tt.name, src)
			if err != nil || applied != tt.want {
				t.Fatalf("applied=%v, want %v (err=%v)", applied, tt.want, err)
			}
			if _, cached := (kvCache{s}).Load(ctx, src.ID); cached != tt.want {
				t.Fatalf("cached=%v, want %v", cached, tt.want)
			}
		})
	}
}

func TestSourceDraftTransportErrorDoesNotExposeURL(t *testing.T) {
	s, admin := newInstalledServer(t)
	s.AllowPrivateSources = true
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	const marker = "fixture-subscription-secret-marker"
	address := strings.Replace(upstream.URL, "http://", "https://", 1) + "/" + marker
	result := mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/sources/inspect", map[string]any{"url": address}, 200)
	if result["ok"] != false || result["location"] != "center-fallback" || result["error"] != "source_request_failed" || strings.Contains(fmt.Sprint(result), marker) {
		t.Fatal("draft transport failure did not preserve its safe reason and location")
	}
}

// 旧库加列后原有行与编号不变，新字段取默认值。
func TestSourceSchemaMigrationKeepsRows(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	src := s.Store.Ent.SubSource.Create().SetName("旧源").SetURL("https://source.example/a").SetNodeCount(5).SaveX(ctx)
	got := s.Store.Ent.SubSource.GetX(ctx, src.ID)
	if got.ID != src.ID || got.NodeCount != 5 || got.GateID != 0 || got.RefreshHours != 0 || got.LastAttemptAt != nil || got.LastLocation != "" {
		t.Fatalf("row=%+v", got)
	}
}

// Gate 负载过高时本轮检测让路：不派发检测，节点记为未检测并写明原因。
func TestProbeYieldsUnderLoad(t *testing.T) {
	s, _, g := setupGate(t)
	ctx := t.Context()
	load := 0.95
	if err := s.recordWorkerResources(ctx, int(g.id), contracts.WorkerResources{Load: &load, Concurrency: 4}); err != nil {
		t.Fatal(err)
	}
	if why := s.probeYield(ctx, int(g.id)); !strings.Contains(why, "负载较高") {
		t.Fatalf("yield reason=%q", why)
	}
	in, err := s.probeSourceNodes(ctx, 3, []string{trojanLine(1, "HK-01")}, "gate:"+itoa(int(g.id)), nil)
	if err != nil || in.Summary.Unchecked != 1 || !strings.Contains(in.Summary.Reason, "推迟") {
		t.Fatalf("summary=%+v err=%v", in.Summary, err)
	}
}
