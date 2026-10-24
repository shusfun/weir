package center

import (
	"context"
	"encoding/base64"
	neturl "net/url"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/subscription"
	"github.com/shusfun/weir/packages/contracts"
)

func setupGate(t *testing.T) (*Server, *testClient, *fakeGate) {
	t.Helper()
	s, admin := newInstalledServer(t)
	for _, k := range [][2]string{{"xray", "26.3.27"}, {"sing-box", "1.12.4"}} {
		if _, err := s.kernelCache().Put(k[0], k[1], "amd64", []byte(k[0]+"-binary")); err != nil {
			t.Fatal(err)
		}
	}
	g := pairFakeGate(t, admin)
	if code, _ := g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Version: "test", Hostname: "hk", Arch: "amd64"}); code != 200 {
		t.Fatalf("heartbeat = %d", code)
	}
	return s, admin, g
}

func mustJSON(t *testing.T, tc *testClient, method, path string, body any, want int) map[string]any {
	t.Helper()
	w := tc.do(method, path, body, nil)
	if w.Code != want {
		t.Fatalf("%s %s = %d %s", method, path, w.Code, w.Body.String())
	}
	if w.Body.Len() == 0 {
		return nil
	}
	return decode(t, w)
}

func login(t *testing.T, s *Server, email, password string) *testClient {
	t.Helper()
	tc := &testClient{t: t, router: s.Router()}
	mustJSON(t, tc, "POST", "/api/v1/auth/login", map[string]string{"email": email, "password": password}, 200)
	if tc.cookie == nil {
		t.Fatal("login did not set a session")
	}
	return tc
}

func TestNodeWizardUsersLinksChainsAndSubscription(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := context.Background()
	gid := int(g.id)

	// 第一步只选协议与 VPS：REALITY 参数、端口由系统生成（skip_probe 时用第一个候选目标）。
	reality := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "skip_probe": true}, 200)
	st := reality["settings"].(map[string]any)
	if st["reality_public_key"] == "" || st["reality_dest"] != "www.microsoft.com:443" || reality["port"].(float64) < portMin {
		t.Fatalf("reality node = %v", reality)
	}
	// 需要证书的预设没有证书时拒绝；Hysteria2 自动自签。
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "trojan-tls", "skip_probe": true}, 400)
	hy2 := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "hysteria2", "skip_probe": true}, 200)
	if hy2["cert_id"] == nil {
		t.Fatalf("hysteria2 must get a self-signed certificate: %v", hy2)
	}
	ss := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "ss2022", "skip_probe": true}, 200)
	// 端口冲突与未知 / 系统字段被拒绝。
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "ss2022", "port": int(ss["port"].(float64)), "skip_probe": true}, 409)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/nodes/"+itoa(int(ss["id"].(float64))), map[string]any{"settings": map[string]any{"clients": []any{}}}, 400)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/nodes/"+itoa(int(ss["id"].(float64))), map[string]any{"settings": map[string]any{"no_such_field": 1}}, 400)

	// 一个窗口建用户：未填密码时生成一次；默认全部节点、自动生成一条链接。
	created := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "alice@example.test", "quota": 20 << 30, "monthly_reset": true}, 200)
	password, _ := created["password"].(string)
	uid := int(created["id"].(float64))
	if password == "" || created["all_nodes"] != true || created["links"].(float64) != 1 {
		t.Fatalf("created user = %v", created)
	}
	detail := mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(uid), nil, 200)
	links := detail["link_items"].([]any)
	first := links[0].(map[string]any)
	entries := first["entries"].([]any)
	if len(entries) != 3 {
		t.Fatalf("default link must cover all 3 nodes: %v", entries)
	}
	// 管理员看到凭据原文。
	if e := entries[0].(map[string]any); e["uuid"] == "" && e["password"] == "" {
		t.Fatalf("admin must see credentials: %v", e)
	}
	token := first["token"].(string)

	// 订阅：按 UA 选格式；浏览器显示订阅页；分享链接能被 MiSub 解析器读回。
	w := admin.do("GET", "/sub/"+token, nil, map[string]string{"User-Agent": "v2rayN/7.0"})
	raw, _ := base64.StdEncoding.DecodeString(w.Body.String())
	uris := strings.Split(string(raw), "\n")
	if w.Code != 200 || len(uris) != 3 || !strings.Contains(w.Header().Get("Subscription-Userinfo"), "total=21474836480") {
		t.Fatalf("base64 sub = %d %q %v", w.Code, raw, w.Header())
	}
	parsed, err := subscription.ParseLinks(uris)
	if err != nil || len(parsed.Nodes) != 3 || len(parsed.Skipped) != 0 {
		t.Fatalf("own share links must parse: %v %+v", err, parsed.Skipped)
	}
	if w = admin.do("GET", "/sub/"+token, nil, map[string]string{"User-Agent": "clash.meta/1.19"}); w.Code != 200 || !strings.Contains(w.Body.String(), "proxies:") || !strings.Contains(w.Body.String(), "fingerprint:") {
		t.Fatalf("clash sub = %d %s", w.Code, w.Body.String())
	}
	if w = admin.do("GET", "/sub/"+token, nil, map[string]string{"User-Agent": "SFA/1.12"}); w.Code != 200 || !strings.Contains(w.Body.String(), `"outbounds"`) {
		t.Fatalf("sing-box sub = %d %s", w.Code, w.Body.String())
	}
	if w = admin.do("GET", "/sub/"+token, nil, map[string]string{"User-Agent": "Mozilla/5.0 Chrome/130", "Accept": "text/html"}); w.Code != 200 || !strings.Contains(w.Body.String(), "console") {
		t.Fatalf("browser sub page = %d %s", w.Code, w.Body.String())
	}
	if w = admin.do("GET", "/sub/"+strings.Repeat("0", 48), nil, nil); w.Code != 404 {
		t.Fatalf("unknown token = %d", w.Code)
	}
	info := mustJSON(t, admin, "GET", "/api/v1/sub-info/"+token, nil, 200)
	if strings.Contains(w.Body.String(), "uuid") || len(info["nodes"].([]any)) != 3 {
		t.Fatalf("sub info = %v", info)
	}

	// Gate 下发：每条链接 × 节点一份凭据，按用户共享额度。
	cfg := g.config(0)
	if len(cfg.Inbounds) != 3 || len(cfg.Credentials) != 3 || cfg.Budgets[userKey(uid)].Allocated != 20<<30 {
		t.Fatalf("gate config inbounds=%d creds=%d budgets=%v meta=%v", len(cfg.Inbounds), len(cfg.Credentials), cfg.Budgets, cfg.Meta)
	}

	// 用户自己：新链接默认全选；最多 10 条；只能用自己的节点。
	alice := login(t, s, "alice@example.test", password)
	only := mustJSON(t, alice, "POST", "/api/v1/me/links", map[string]any{"name": "手机", "all_nodes": false, "node_ids": []int{int(reality["id"].(float64))}}, 200)
	if only["nodes"].(float64) != 1 {
		t.Fatalf("restricted link = %v", only)
	}
	for i := 0; i < 8; i++ {
		mustJSON(t, alice, "POST", "/api/v1/me/links", map[string]any{}, 200)
	}
	mustJSON(t, alice, "POST", "/api/v1/me/links", map[string]any{}, 409)
	if n, _ := s.Store.Ent.Credential.Query().Where(credential.UserIDEQ(uid)).Count(ctx); n != 3+1+8*3 {
		t.Fatalf("credentials = %d", n)
	}
	// 自建链式代理：出口必须是公网地址；管理员可跳过 Gate 实测。
	mustJSON(t, alice, "POST", "/api/v1/me/chains", map[string]any{"name": "家宽", "entry_node_id": reality["id"], "exit": map[string]any{"kind": "socks5", "address": "192.168.1.2", "port": 1080}}, 422)
	// 普通用户不能跳过实测：Gate 离线时直接失败（不会等待探测超时）。
	s.Store.Ent.Gate.UpdateOneID(gid).ClearLastSeenAt().ExecX(ctx)
	if w := alice.do("POST", "/api/v1/me/chains", map[string]any{"name": "家宽", "entry_node_id": reality["id"], "exit": map[string]any{"kind": "socks5", "address": "203.0.113.5", "port": 1080}, "skip_probe": true}, nil); w.Code != 422 || !strings.Contains(w.Body.String(), "gate_offline") {
		t.Fatalf("user chain without probe = %d %s", w.Code, w.Body.String())
	}
	ch := mustJSON(t, admin, "POST", "/api/v1/admin/users/"+itoa(uid)+"/chains", map[string]any{"name": "家宽", "entry_node_id": reality["id"], "exit": map[string]any{"kind": "socks5", "address": "203.0.113.5", "port": 1080, "username": "u", "password": "p"}, "skip_probe": true}, 200)
	if ch["exit_password"] != "p" {
		t.Fatalf("admin must see chain password: %v", ch)
	}
	cfg = g.config(0)
	var chainRoute bool
	for _, r := range cfg.Routes {
		chainRoute = chainRoute || r.OutboundTag == "chain-"+itoa(int(ch["id"].(float64)))
	}
	if !chainRoute {
		t.Fatalf("chain route missing: %+v meta=%v", cfg.Routes, cfg.Meta)
	}
	// 链接全选时包含新链；关闭用户的链式代理后其凭据不再下发。
	w = admin.do("GET", "/sub/"+token, nil, map[string]string{"User-Agent": "v2rayN"})
	raw, _ = base64.StdEncoding.DecodeString(w.Body.String())
	if !strings.Contains(string(raw), neturl.PathEscape("家宽")) {
		t.Fatalf("chain node missing from subscription: %s", raw)
	}
	before := len(cfg.Credentials)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(uid), map[string]any{"allow_chain": false}, 200)
	if after := len(g.config(0).Credentials); after >= before {
		t.Fatalf("chain credentials must be withdrawn: %d -> %d", before, after)
	}

	// 隔离：其他用户看不到、改不了 alice 的链接。
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "bob@example.test", "password": "Passw0rd"}, 200)
	bob := login(t, s, "bob@example.test", "Passw0rd")
	if items := mustJSON(t, bob, "GET", "/api/v1/me/links", nil, 200)["items"].([]any); len(items) != 1 {
		t.Fatalf("bob links = %v", items)
	}
	mustJSON(t, bob, "DELETE", "/api/v1/me/links/"+itoa(int(first["id"].(float64))), nil, 404)
	mustJSON(t, bob, "GET", "/api/v1/admin/users", nil, 403)

	// 删除用户：凭据、链接一并删除，Gate 不再有其凭据。
	mustJSON(t, admin, "DELETE", "/api/v1/admin/users/"+itoa(uid), nil, 204)
	if n, _ := s.Store.Ent.Link.Query().Where(link.UserIDEQ(uid)).Count(ctx); n != 0 {
		t.Fatalf("links left = %d", n)
	}
	for _, c := range g.config(0).Credentials {
		if c.UserKey == userKey(uid) {
			t.Fatal("deleted user's credential still delivered")
		}
	}
}

func TestNodeChainThroughAnotherNode(t *testing.T) {
	_, admin, g := setupGate(t)
	gid := int(g.id)
	exit := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "skip_probe": true}, 200)
	entry := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "ss2022", "skip_probe": true, "chain": map[string]any{"kind": "node", "node_id": exit["id"]}}, 200)
	cfg := g.config(0)
	found := false
	for _, c := range cfg.Credentials {
		found = found || c.StatsKey == nodeExitKey(int(entry["id"].(float64))) && c.Internal
	}
	var route bool
	for _, r := range cfg.Routes {
		route = route || r.InboundTag == nodeTag(int(entry["id"].(float64))) && strings.HasPrefix(r.OutboundTag, "nodechain-")
	}
	if !found || !route {
		t.Fatalf("internal exit credential=%v route=%v meta=%v", found, route, cfg.Meta)
	}
	// 出口被引用时不能删除；自己不能作为自己的出口。
	mustJSON(t, admin, "DELETE", "/api/v1/admin/nodes/"+itoa(int(exit["id"].(float64))), nil, 409)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/nodes/"+itoa(int(exit["id"].(float64))), map[string]any{"chain": map[string]any{"kind": "node", "node_id": entry["id"]}, "skip_probe": true}, 422)
}

func TestNextReset(t *testing.T) {
	loc := time.UTC
	cases := [][2]time.Time{
		{time.Date(2026, 1, 31, 8, 0, 0, 0, loc), time.Date(2026, 2, 28, 8, 0, 0, 0, loc)},
		{time.Date(2026, 3, 15, 0, 0, 0, 0, loc), time.Date(2026, 4, 15, 0, 0, 0, 0, loc)},
		{time.Date(2026, 12, 31, 0, 0, 0, 0, loc), time.Date(2027, 1, 31, 0, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		if got := nextReset(c[0]); !got.Equal(c[1]) {
			t.Fatalf("nextReset(%v) = %v, want %v", c[0], got, c[1])
		}
	}
}
