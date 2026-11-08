package center

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

// 代理页：按类型开关、类型出口、分流规则校验、自动选择（中心排序）、Weir 格式；链接的生效时间；拉取统计。
func TestProxyPolicyWindowsAndPulls(t *testing.T) {
	s, admin, g := setupGate(t)
	gid := int(g.id)
	stop := make(chan struct{})
	defer close(stop)
	g.serveChainProbes("203.0.113.9", stop)

	// 外部连接都通（不真的连外部地址），排序时有延迟可比

	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "skip_probe": true}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "skip_probe": true}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "ss2022", "skip_probe": true}, 200)
	created := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "bob@example.test", "password": "Passw0rd-bob", "all_nodes": true}, 200)
	uid := int(created["id"].(float64))
	bob := login(t, s, "bob@example.test", "Passw0rd-bob")
	links := mustJSON(t, bob, "GET", "/api/v1/me/links", nil, 200)["items"].([]any)
	tok := links[0].(map[string]any)["token"].(string)
	lid := int(links[0].(map[string]any)["id"].(float64))

	// 类型：两个 REALITY 节点归为一类，SS2022 一类。
	view := mustJSON(t, bob, "GET", "/api/v1/proxy", nil, 200)
	types := view["types"].([]any)
	if len(types) != 2 || types[0].(map[string]any)["key"] != "vless-reality" || len(types[0].(map[string]any)["nodes"].([]any)) != 2 {
		t.Fatalf("types = %v", types)
	}
	if code, uris := subLinks(t, bob, tok); code != 200 || len(uris) != 3 {
		t.Fatalf("default sub = %d %v", code, uris)
	}

	mustJSON(t, bob, "PUT", "/api/v1/proxy", map[string]any{"revision": 0, "auto": map[string]any{"refresh_minutes": 10}}, 400)
	mustJSON(t, bob, "PUT", "/api/v1/proxy", map[string]any{"revision": 0, "disabled": []string{"ss2022"}, "auto": map[string]any{"refresh_minutes": 60}}, 200)
	if code, uris := subLinks(t, bob, tok); code != 200 || len(uris) != 2 {
		t.Fatal(code, uris)
	}
	w := bob.do("GET", "/sub/"+tok+"?target=weir", nil, nil)
	var ws contracts.WeirSubscription
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ws) != nil || ws.Version != 2 || ws.Groups[0].Name != "PROXY" || ws.Refresh != 60 {
		t.Fatal(w.Code, w.Body.String())
	}
	if v := mustJSON(t, admin, "GET", "/api/v1/proxy?user="+itoa(uid), nil, 200); len(v["types"].([]any)) != 2 {
		t.Fatal(v)
	}
	for range 3 {
		bob.do("GET", "/sub/"+tok, nil, map[string]string{"User-Agent": "clash-verge/v2.0"})
	}
	// 生效时间：开始晚于结束、结束晚于用户到期都不保存；未开始与已结束拉不到节点，Gate 的凭据带同样的时段。
	now := time.Now().UTC().Truncate(time.Second)
	mustJSON(t, bob, "PATCH", "/api/v1/me/links/"+itoa(lid), map[string]any{"starts_at": now.Add(2 * time.Hour), "ends_at": now.Add(time.Hour)}, 400)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(uid), map[string]any{"expires_at": now.Add(48 * time.Hour)}, 200)
	if w := bob.do("PATCH", "/api/v1/me/links/"+itoa(lid), map[string]any{"ends_at": now.Add(72 * time.Hour)}, nil); w.Code != 400 || !strings.Contains(w.Body.String(), "link_window_exceeds_plan") {
		t.Fatalf("window past plan = %d %s", w.Code, w.Body.String())
	}
	start := now.Add(time.Hour)
	l := mustJSON(t, bob, "PATCH", "/api/v1/me/links/"+itoa(lid), map[string]any{"starts_at": start}, 200)
	if l["state"] != "not_started" || l["effective_ends_at"] == nil {
		t.Fatalf("window view = %v", l)
	}
	if w := bob.do("GET", "/sub/"+tok, nil, map[string]string{"User-Agent": "v2rayN/7.0"}); w.Code != 403 || !strings.Contains(w.Body.String(), "subscription_not_started") {
		t.Fatalf("not started = %d %s", w.Code, w.Body.String())
	}
	found := false
	for _, c := range g.config(0).Credentials {
		if c.UserKey == userKey(uid) {
			found = true
			if c.StartsAt == nil || !c.StartsAt.Equal(start) || c.ExpiresAt == nil || !c.ExpiresAt.Equal(now.Add(48*time.Hour)) || c.InWindow(time.Now()) {
				t.Fatalf("gate credential window = %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("no credentials for bob on the gate")
	}
	mustJSON(t, bob, "PATCH", "/api/v1/me/links/"+itoa(lid), map[string]any{"starts_at": nil, "ends_at": nil}, 200)

	// 拉取统计：客户端名称、次数、被拒绝的次数；只能看自己的。
	bob.do("GET", "/sub/"+tok, nil, map[string]string{"User-Agent": "clash-verge/v2.0"})
	stats := mustJSON(t, bob, "GET", "/api/v1/me/link-stats", nil, 200)["items"].([]any)
	st := stats[0].(map[string]any)
	if st["pulls_30d"].(float64) < 4 || st["refused_30d"].(float64) != 1 || st["last_pull"].(map[string]any)["client"] != "Clash Verge" {
		t.Fatalf("stats = %v", st)
	}
	if items := mustJSON(t, bob, "GET", "/api/v1/me/links/"+itoa(lid)+"/pulls", nil, 200)["items"].([]any); len(items) < 4 {
		t.Fatalf("pulls = %v", items)
	}
	mustJSON(t, bob, "GET", "/api/v1/admin/link-stats", nil, 403)
	all := mustJSON(t, admin, "GET", "/api/v1/admin/link-stats?user="+itoa(uid), nil, 200)["items"].([]any)
	if len(all) != 1 || all[0].(map[string]any)["user_id"].(float64) != float64(uid) {
		t.Fatalf("admin stats = %v", all)
	}
}
