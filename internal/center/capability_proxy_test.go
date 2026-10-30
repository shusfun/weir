package center

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/link"
)

// dsh 的代理页与订阅管理能力：读代理设置与速率、读拉取统计（不含来源 IP）；草稿改代理设置（没给的保持原样、可撤回）、
// 给类型加出口（代理密码只在确认窗里填）、去掉类型出口；新建链接带生效时间，提交时就校验。
func TestProxyCapabilities(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	stop := make(chan struct{})
	defer close(stop)
	g.serveChainProbes("203.0.113.9", stop)
	old := reachDial
	reachDial = func(context.Context, string) (net.Conn, error) { a, b := net.Pipe(); b.Close(); return a, nil }
	defer func() { reachDial = old }()
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	created := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "dave@example.test", "password": "Passw0rd-dave", "all_nodes": true}, 200)
	uid := int(created["id"].(float64))
	tok := s.Store.Ent.Link.Query().Where(link.UserIDEQ(uid)).OnlyX(ctx).Token
	dave := login(t, s, "dave@example.test", "Passw0rd-dave")
	dave.do("GET", "/sub/"+tok, nil, map[string]string{"User-Agent": "clash-verge/v2.0"})

	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	latest := func() map[string]any {
		return mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)[0].(map[string]any)
	}
	submit := func(steps ...map[string]any) (string, bool) {
		list := []any{}
		for _, st := range steps {
			list = append(list, st)
		}
		return toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "代理", "steps": list})
	}

	// 只读：代理设置带类型与速率；拉取统计有客户端、没有来源 IP
	text, isErr := toolText(t, h, m.webToken, "get_proxy", map[string]any{"user_id": uid})
	if isErr || !strings.Contains(text, `"vless-reality"`) || !strings.Contains(text, `"p90_bps"`) || !strings.Contains(text, `"categories"`) {
		t.Fatalf("get_proxy = %s", text)
	}
	text, isErr = toolText(t, h, m.webToken, "get_link_stats", map[string]any{"user_id": uid})
	if isErr || !strings.Contains(text, "Clash Verge") || strings.Contains(text, `"ip"`) || strings.Contains(text, "192.0.2") || strings.Contains(text, "user_agent") {
		t.Fatalf("get_link_stats = %s", text)
	}

	// proxy.update：只改两项，其余保持原样；试运行不写；确认后生效；校验与手动相同
	if text, isErr := submit(map[string]any{"action": "proxy.update", "args": map[string]any{"user_id": uid, "refresh_minutes": 10}}); !isErr || !strings.Contains(text, "超出范围") {
		t.Fatalf("refresh below min = %q", text)
	}
	if text, isErr := submit(map[string]any{"action": "proxy.update", "args": map[string]any{"user_id": uid, "rule_final": "reject"}}); !isErr {
		t.Fatalf("final reject must be refused at submit: %q", text)
	}
	if text, isErr := submit(map[string]any{"action": "proxy.update", "args": map[string]any{"user_id": uid, "disabled": "ss2022", "auto_mode": "ranked"}}); isErr {
		t.Fatal(text)
	}
	if u := s.Store.Ent.User.GetX(ctx, uid); len(u.ProxyPolicy) != 0 {
		t.Fatalf("dry run wrote policy: %v", u.ProxyPolicy)
	}
	d := latest()
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", nil, 200)
	pol := loadPolicy(s.Store.Ent.User.GetX(ctx, uid))
	if len(pol.Disabled) != 1 || pol.Disabled[0] != "ss2022" || pol.Auto.Mode != "ranked" || pol.Auto.RefreshMinutes != 30 || pol.Rules["cn"] != "" {
		t.Fatalf("policy after apply = %+v", pol)
	}
	if code, uris := subLinks(t, dave, tok); code != 200 || len(uris) != 1 {
		t.Fatalf("sub after proxy.update = %d %v", code, uris)
	}

	// proxy.type_exit：代理密码不能由 dsh 填；管理员在确认窗里填后保存，订阅换成经出口的版本
	exit := map[string]any{"user_id": uid, "type_key": "vless-reality", "kind": "socks5", "address": "203.0.113.5", "port": 1080, "username": "u"}
	withPW := map[string]any{}
	for k, v := range exit {
		withPW[k] = v
	}
	withPW["password"] = "HOME-SECRET"
	if text, isErr := submit(map[string]any{"action": "proxy.type_exit", "args": withPW}); !isErr || !strings.Contains(text, "机密") {
		t.Fatalf("secret from model = %q", text)
	}
	if text, isErr := submit(map[string]any{"action": "proxy.type_exit", "args": exit}); isErr {
		t.Fatal(text)
	}
	if n := s.Store.Ent.Chain.Query().CountX(ctx); n != 0 {
		t.Fatalf("type_exit dry run wrote chains: %d", n)
	}
	d = latest()
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", map[string]any{"edits": map[string]any{"0": map[string]any{"password": "HOME-SECRET"}}}, 200)
	ch := s.Store.Ent.Chain.Query().Where(chain.PolicyTypeEQ("vless-reality")).OnlyX(ctx)
	if pw, _ := s.reveal(ch.ExitPassword); pw != "HOME-SECRET" || ch.ExitAddress != "203.0.113.5" {
		t.Fatalf("type exit chain = %+v pw=%q", ch, pw)
	}
	if text, _ := toolText(t, h, m.webToken, "get_proxy", map[string]any{"user_id": uid}); strings.Contains(text, "HOME-SECRET") {
		t.Fatalf("get_proxy leaks exit password: %s", text)
	}
	if code, uris := subLinks(t, dave, tok); code != 200 || len(uris) != 1 || !strings.Contains(uris[0], "%E7%BB%8F") {
		t.Fatalf("sub after type_exit = %d %v", code, uris)
	}

	// proxy.type_exit_delete：去掉后恢复直连
	if text, isErr := submit(map[string]any{"action": "proxy.type_exit_delete", "args": map[string]any{"user_id": uid, "type_key": "vless-reality"}}); isErr {
		t.Fatal(text)
	}
	if n := s.Store.Ent.Chain.Query().CountX(ctx); n != 1 {
		t.Fatalf("type_exit_delete dry run deleted chains: %d", n)
	}
	d = latest()
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", nil, 200)
	if n := s.Store.Ent.Chain.Query().CountX(ctx); n != 0 {
		t.Fatalf("type exit not removed: %d", n)
	}

	// link.create 带生效时间：结束晚于用户到期在提交时就拒绝；合法的确认后建出带时段的链接，get_user 里能看到状态
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(uid), map[string]any{"expires_at": time.Now().Add(48 * time.Hour).UTC()}, 200)
	if text, isErr := submit(map[string]any{"action": "link.create", "args": map[string]any{"user_id": uid, "name": "平板", "ends_at": time.Now().Add(96 * time.Hour).UTC().Format(time.RFC3339)}}); !isErr || !strings.Contains(text, "用户到期") {
		t.Fatalf("link window past plan = %q", text)
	}
	start := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	if text, isErr := submit(map[string]any{"action": "link.create", "args": map[string]any{"user_id": uid, "name": "平板", "starts_at": start.Format(time.RFC3339)}}); isErr {
		t.Fatal(text)
	}
	d = latest()
	if !strings.Contains(mustString(d), "生效时间") {
		t.Fatalf("link.create preview = %v", d)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", nil, 200)
	l := s.Store.Ent.Link.Query().Where(link.UserIDEQ(uid), link.NameEQ("平板")).OnlyX(ctx)
	if l.StartsAt == nil || !l.StartsAt.Equal(start) {
		t.Fatalf("link window = %v", l.StartsAt)
	}
	if text, _ := toolText(t, h, m.webToken, "get_user", map[string]any{"user_id": uid}); !strings.Contains(text, `"not_started"`) || !strings.Contains(text, `"effective_ends_at"`) {
		t.Fatalf("get_user = %s", text)
	}
}
