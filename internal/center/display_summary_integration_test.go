package center

import (
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func TestUserNodeSummaryUsesEffectiveExitAndKeepsAdminEndpointPrivate(t *testing.T) {
	s, admin, g := setupGate(t)
	g.serveActions(t, s, nil)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "name": "入口备注", "skip_probe": true, "exit": map[string]any{"kind": "socks5", "remark": "管理员出口", "address": "203.0.113.99", "port": 1080, "password": "admin-private-password"}}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "summary@test.invalid", "password": "SummaryTest2026", "all_nodes": true, "allow_chain": true}, 200)
	u := login(t, s, "summary@test.invalid", "SummaryTest2026")
	check := func(name string) {
		t.Helper()
		w := u.do("GET", "/api/v1/me/nodes", nil, nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), "203.0.113.99") || strings.Contains(w.Body.String(), "admin-private-password") {
			t.Fatalf("unsafe summary: %d %s", w.Code, w.Body)
		}
		v := decode(t, w)["items"].([]any)[0].(map[string]any)
		if v["display_name"] != name || v["exit_summary"].(map[string]any)["name"] != name {
			t.Fatalf("summary=%v", v)
		}
	}
	check("管理员出口")

	mustJSON(t, u, "PUT", "/api/v1/proxy/types/ss2022/exit", map[string]any{"revision": 0, "exit": map[string]any{"kind": "socks5", "remark": "我的出口", "address": "203.0.113.5", "port": 1080}}, 200)
	check("我的出口")
	links := mustJSON(t, u, "GET", "/api/v1/me/links", nil, 200)["items"].([]any)
	token := links[0].(map[string]any)["token"].(string)
	assertSub := func(name string) {
		t.Helper()
		info := mustJSON(t, u, "GET", "/api/v1/sub-info/"+token, nil, 200)
		for _, item := range info["nodes"].([]any) {
			if got := item.(map[string]any)["name"]; got != "[链式] "+name {
				t.Fatalf("subscription=%v", got)
			}
		}
		if w := u.do("GET", "/sub/"+token+"?target=singbox", nil, nil); w.Code != 200 || !strings.Contains(w.Body.String(), name) || strings.Contains(w.Body.String(), "管理员出口") {
			t.Fatalf("output=%d %s", w.Code, w.Body)
		}
	}
	assertSub("我的出口")
	es := mustJSON(t, u, "GET", "/api/v1/me/exits", nil, 200)["items"].([]any)
	eid := int(es[0].(map[string]any)["id"].(float64))
	mustJSON(t, u, "PATCH", "/api/v1/me/exits/"+itoa(eid), map[string]any{"remark": "203.0.113.88"}, 200)
	check("203.0.113.88")
	assertSub("203.0.113.88")
	if w := u.do("GET", "/api/v1/me/exits/"+itoa(int(n["exit_id"].(float64))), nil, nil); w.Code != 404 {
		t.Fatalf("admin exit access=%d", w.Code)
	}
}

func TestDownstreamChangeInvalidatesExitIPObservation(t *testing.T) {
	s, admin, g := setupGate(t)
	g.serveActions(t, s, nil)
	g2 := pairFakeGate(t, admin)
	heartbeat(t, g2, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	makeNode := func(gid int, name string, exit any) map[string]any {
		return mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "name": name, "skip_probe": true, "exit": exit}, 200)
	}
	n := makeNode(int(g2.id), "下游", map[string]any{"kind": "freedom"})
	next := makeNode(int(g.id), "上游", map[string]any{"kind": "node", "node_id": n["id"]})
	owner := exitOwner(int(next["exit_id"].(float64)))
	s.saveExitProbe(t.Context(), owner, &exitProbe{OK: true, Mode: "proxy", ExitIP: "198.51.100.77", At: time.Now()})
	path := "/api/v1/admin/nodes/" + itoa(int(n["id"].(float64)))
	mustJSON(t, admin, "PATCH", path, map[string]any{"name": "改备注"}, 200)
	if _, ok := s.loadExitProbe(t.Context(), owner); !ok {
		t.Fatal("remark invalidated unchanged path")
	}
	mustJSON(t, admin, "PATCH", path, map[string]any{"name": "表单改备注", "port": n["port"], "exit_id": n["exit_id"], "settings": n["settings"]}, 200)
	if _, ok := s.loadExitProbe(t.Context(), owner); !ok {
		t.Fatal("resubmitting unchanged node fields invalidated downstream observation")
	}
	upstreamPath := "/api/v1/admin/nodes/" + itoa(int(next["id"].(float64)))
	mustJSON(t, admin, "PATCH", upstreamPath, map[string]any{"name": "上游改备注", "exit_id": next["exit_id"]}, 200)
	if _, ok := s.loadExitProbe(t.Context(), owner); !ok {
		t.Fatal("reusing selected exit invalidated its observation")
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "name": "复用出口", "exit_id": next["exit_id"], "skip_probe": true}, 200)
	if _, ok := s.loadExitProbe(t.Context(), owner); !ok {
		t.Fatal("creating another node with the same exit invalidated its observation")
	}
	mustJSON(t, admin, "PATCH", path, map[string]any{"port": 23456}, 200)
	if _, ok := s.loadExitProbe(t.Context(), owner); ok {
		t.Fatal("old downstream observation survived")
	}
}
