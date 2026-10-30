package center

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/node"
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
	old := reachDial
	reachDial = func(context.Context, string) (net.Conn, error) { a, b := net.Pipe(); b.Close(); return a, nil }
	defer func() { reachDial = old }()
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
	view := mustJSON(t, bob, "GET", "/api/v1/me/proxy", nil, 200)
	types := view["types"].([]any)
	if len(types) != 2 || types[0].(map[string]any)["key"] != "vless-reality" || len(types[0].(map[string]any)["nodes"].([]any)) != 2 {
		t.Fatalf("types = %v", types)
	}
	if code, uris := subLinks(t, bob, tok); code != 200 || len(uris) != 3 {
		t.Fatalf("default sub = %d %v", code, uris)
	}

	// 规则与刷新间隔的校验：不认识的类别、指向不存在的类型、其余流量拒绝、少于 30 分钟都不保存。
	for _, body := range []map[string]any{
		{"rules": map[string]string{"nope": "auto"}},
		{"rules": map[string]string{"ai": "no-such-type"}},
		{"rules": map[string]string{"final": "reject"}},
		{"auto": map[string]any{"refresh_minutes": 10}},
	} {
		mustJSON(t, bob, "PUT", "/api/v1/me/proxy", body, 400)
	}

	// 中心排序、只放前 2 个进自动选择；Weir 格式带名次与分组。
	mustJSON(t, bob, "PUT", "/api/v1/me/proxy", map[string]any{"rules": map[string]string{"streaming": "ss2022"}, "auto": map[string]any{"mode": "ranked", "top": 2, "refresh_minutes": 60}}, 200)
	w := bob.do("GET", "/sub/"+tok+"?target=weir", nil, nil)
	var ws contracts.WeirSubscription
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ws) != nil {
		t.Fatalf("weir = %d %s", w.Code, w.Body.String())
	}
	countAuto := func(ws contracts.WeirSubscription) (auto int, exploring string) {
		for _, n := range ws.Nodes {
			if n.Auto {
				auto++
			}
			if n.Exploring {
				exploring = n.Name
			}
		}
		return
	}
	// 还没有任何速率数据：全部可用节点都进自动选择
	if auto, _ := countAuto(ws); ws.Version != contracts.WeirSubscriptionVersion || len(ws.Nodes) != 3 || auto != 3 || ws.Refresh != 60 || ws.Groups[0].Name != "节点选择" {
		t.Fatalf("weir sub = %+v", ws)
	}
	// 两个节点有速率（第二个 REALITY 更快）：前 1 个按速率，另 1 个是轮换位（没数据的 SS2022）；更快的排第一
	nodes := s.Store.Ent.Node.Query().Order(ent.Asc(node.FieldID)).AllX(t.Context())
	for i, bps := range []int64{2 << 20, 8 << 20} {
		s.Store.Ent.RateSample.Create().SetUserID(uid).SetNodeID(nodes[i].ID).SetAt(minuteOf(time.Now())).SetBytes(bps * 30).SetSeconds(30).ExecX(t.Context())
	}
	json.Unmarshal(bob.do("GET", "/sub/"+tok+"?target=weir", nil, nil).Body.Bytes(), &ws)
	auto, exploring := countAuto(ws)
	if auto != 2 || ws.Nodes[0].Name != nodes[1].Name || ws.Nodes[0].RateFrom != "self" || exploring != nodes[2].Name {
		t.Fatalf("ranked by rate = %+v", ws.Nodes)
	}
	if w.Header().Get("Profile-Update-Interval") != "1" {
		t.Fatalf("update interval = %q", w.Header().Get("Profile-Update-Interval"))
	}
	ssLabel := types[1].(map[string]any)["label"].(string)
	clash := bob.do("GET", "/sub/"+tok, nil, map[string]string{"User-Agent": "clash-verge/v2.0"}).Body.String()
	if !strings.Contains(clash, "DOMAIN-SUFFIX,netflix.com,"+ssLabel+"\n") || !strings.Contains(clash, "MATCH,节点选择") {
		t.Fatalf("streaming rule must point at the %s group:\n%s", ssLabel, clash)
	}

	// 关掉一个类型：订阅里不再有它。
	mustJSON(t, bob, "PUT", "/api/v1/me/proxy", map[string]any{"disabled": []string{"ss2022"}}, 200)
	if code, uris := subLinks(t, bob, tok); code != 200 || len(uris) != 2 || strings.HasPrefix(uris[0], "ss://") || strings.HasPrefix(uris[1], "ss://") {
		t.Fatalf("disabled type still in sub: %d %v", code, uris)
	}

	// 类型出口：有一个节点连不通就整体不保存并列出原因；连得通时订阅里换成经出口的版本。
	w = bob.do("PUT", "/api/v1/me/proxy/types/vless-reality/exit", map[string]any{"exit": map[string]any{"kind": "socks5", "address": "203.0.113.9", "port": 1080}}, nil)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "type_exit_failed") || !strings.Contains(w.Body.String(), "chain_probe_failed") {
		t.Fatalf("unreachable type exit = %d %s", w.Code, w.Body.String())
	}
	if n, _ := s.Store.Ent.Chain.Query().Count(t.Context()); n != 0 {
		t.Fatalf("failed type exit must not save chains: %d", n)
	}
	mustJSON(t, bob, "PUT", "/api/v1/me/proxy/types/vless-reality/exit", map[string]any{"exit": map[string]any{"kind": "socks5", "address": "203.0.113.5", "port": 1080, "username": "u", "password": "home"}}, 200)
	code, uris := subLinks(t, bob, tok)
	via := 0
	for _, u := range uris {
		if strings.Contains(u, "%E7%BB%8F") { // 「经」：经出口的那条
			via++
		}
	}
	if code != 200 || len(uris) != 2 || via != 2 {
		t.Fatalf("type exit sub = %d %v", code, uris)
	}
	// 类型出口不出现在链式代理列表里、不占 10 条上限。
	if items := mustJSON(t, bob, "GET", "/api/v1/me/chains", nil, 200)["items"].([]any); len(items) != 0 {
		t.Fatalf("policy chains must be hidden: %v", items)
	}
	mustJSON(t, bob, "DELETE", "/api/v1/me/proxy/types/vless-reality/exit", nil, 200)
	if code, uris := subLinks(t, bob, tok); code != 200 || len(uris) != 2 || strings.Contains(uris[0], "%E7%BB%8F") {
		t.Fatalf("after removing type exit = %d %v", code, uris)
	}
	// 管理员看同一个用户的代理页
	if v := mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(uid)+"/proxy", nil, 200); len(v["types"].([]any)) != 2 {
		t.Fatalf("admin proxy view = %v", v)
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
