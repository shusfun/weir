package center

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/packages/contracts"
)

// 验收清单里只靠中心就能核对的条目（AUTH / ACL / CHAIN-02 / USER / LINK / SUB-01 / SUB-02 / SEC-02）。

func subLinks(t *testing.T, tc *testClient, token string) (int, []string) {
	t.Helper()
	w := tc.do("GET", "/sub/"+token, nil, map[string]string{"User-Agent": "v2rayN/7.0"})
	if w.Code != 200 {
		return w.Code, nil
	}
	raw, _ := base64.StdEncoding.DecodeString(w.Body.String())
	if strings.TrimSpace(string(raw)) == "" {
		return 200, []string{}
	}
	return 200, strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func auditHas(t *testing.T, s *Server, actor int, action, target, result string) bool {
	t.Helper()
	n, _ := s.Store.Ent.Audit.Query().Where(audit.ActorEQ(actor), audit.ActionEQ(action), audit.TargetEQ(target), audit.ResultEQ(result)).Count(context.Background())
	return n > 0
}

// serveChainProbes 让假 Gate 回应链式代理实测：目标地址在 unreachable 里时报告连接失败。
func (g *fakeGate) serveChainProbes(unreachable string, stop chan struct{}) {
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Version: "test", Hostname: "hk", Arch: "amd64"})
			code, body := g.call("GET", "/api/v1/agent/actions", nil)
			if code != 200 {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			var acts []contracts.Action
			_ = json.Unmarshal(body, &acts)
			for _, a := range acts {
				res := contracts.ActionResult{OK: true, Result: map[string]any{"latency_ms": 5}}
				if a.Payload["address"] == unreachable {
					res = contracts.ActionResult{Error: "chain_probe_failed: 连接出口超时"}
				}
				g.call("POST", "/api/v1/agent/actions/"+itoa(int(a.ID)), res)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
}

func TestAcceptanceSecondFactorAndLoginLimits(t *testing.T) {
	_, admin := newInstalledServer(t)
	secret := decode(t, admin.do("POST", "/api/v1/security/totp/setup", map[string]string{"password": "Passw0rd"}, nil))["secret"].(string)
	opts := totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}
	now := time.Now()
	enroll, _ := totp.GenerateCodeCustom(secret, now, opts)
	codes := decode(t, admin.do("POST", "/api/v1/security/totp/confirm", map[string]string{"code": enroll}, nil))["recovery_codes"].([]any)

	// AUTH-01：只过了密码没有会话，管理接口拒绝；错误、过期、已用过的验证码都被拒绝。
	fresh := &testClient{t: t, router: admin.router}
	challenge := decode(t, fresh.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.test", "password": "Passw0rd"}, nil))["challenge"].(string)
	if w := fresh.do("GET", "/api/v1/admin/users", nil, nil); w.Code != 401 {
		t.Fatalf("admin api after password only = %d", w.Code)
	}
	expired, _ := totp.GenerateCodeCustom(secret, now.Add(-5*time.Minute), opts)
	for _, code := range []string{"000000", expired, enroll} {
		if w := fresh.do("POST", "/api/v1/auth/mfa", map[string]string{"challenge": challenge, "code": code}, nil); w.Code != 401 {
			t.Fatalf("second factor %q = %d", code, w.Code)
		}
	}
	if w := fresh.do("POST", "/api/v1/auth/mfa", map[string]string{"challenge": "forged", "code": codes[0].(string)}, nil); w.Code != 401 {
		t.Fatalf("forged challenge = %d", w.Code)
	}
	// 下一个时间片的验证码可用；同一个验证码不能再登录第二次。
	next, _ := totp.GenerateCodeCustom(secret, now.Add(30*time.Second), opts)
	mustJSON(t, fresh, "POST", "/api/v1/auth/mfa", map[string]string{"challenge": challenge, "code": next}, 200)
	mustJSON(t, fresh, "GET", "/api/v1/admin/users", nil, 200)
	again := &testClient{t: t, router: admin.router}
	challenge = decode(t, again.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.test", "password": "Passw0rd"}, nil))["challenge"].(string)
	if w := again.do("POST", "/api/v1/auth/mfa", map[string]string{"challenge": challenge, "code": next}, nil); w.Code != 401 {
		t.Fatalf("replayed totp = %d", w.Code)
	}

	// AUTH-02：连续登录失败触发限速（同一来源 10 分钟内 20 次）。
	brute := &testClient{t: t, router: admin.router}
	last := 0
	for i := 0; i < 25; i++ {
		last = brute.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.test", "password": "wrong"}, nil).Code
	}
	if last != 429 {
		t.Fatalf("login after repeated failures = %d", last)
	}
}

func TestAcceptanceUserIsolationAuditAndSubscriptions(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := context.Background()
	gid := int(g.id)
	stop := make(chan struct{})
	defer close(stop)
	g.serveChainProbes("203.0.113.9", stop)

	n1 := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "skip_probe": true}, 200)
	n2 := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "ss2022", "skip_probe": true}, 200)
	id1, id2 := int(n1["id"].(float64)), int(n2["id"].(float64))
	me := int(decode(t, admin.do("GET", "/api/v1/me", nil, nil))["id"].(float64))

	// USER-01：一个窗口建用户（套餐 + 节点），凭据立即下发，返回可直接展示的订阅地址；续期、加额度在同一个接口完成。
	alice := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "alice@example.test", "password": "Passw0rd", "quota": 10 << 30, "all_nodes": false, "node_ids": []int{id1}, "allow_chain": true}, 200)
	aid := int(alice["id"].(float64))
	detail := mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(aid), nil, 200)
	first := detail["link_items"].([]any)[0].(map[string]any)
	if !strings.HasSuffix(first["url"].(string), "/sub/"+first["token"].(string)) || first["nodes"].(float64) != 1 {
		t.Fatalf("default link = %v", first)
	}
	if b := g.config(0).Budgets[userKey(aid)]; b.Allocated != 10<<30 {
		t.Fatalf("budget after create = %+v", b)
	}
	until := time.Now().Add(60 * 24 * time.Hour).UTC().Truncate(time.Second)
	renewed := mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(aid), map[string]any{"quota": 30 << 30, "expires_at": until}, 200)
	if renewed["quota"].(float64) != 30<<30 {
		t.Fatalf("renewed = %v", renewed)
	}
	cfg := g.config(0)
	if b := cfg.Budgets[userKey(aid)]; b.Allocated != 30<<30 {
		t.Fatalf("budget after renew = %+v", b)
	}
	for _, c := range cfg.Credentials {
		if c.UserKey == userKey(aid) && (c.ExpiresAt == nil || !c.ExpiresAt.Equal(until)) {
			t.Fatalf("credential expiry after renew = %v", c.ExpiresAt)
		}
	}
	if !auditHas(t, s, me, "user.create", "user:"+itoa(aid), "ok") || !auditHas(t, s, me, "user.update", "user:"+itoa(aid), "ok") {
		t.Fatal("user create / update must be audited")
	}

	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "bob@example.test", "password": "Passw0rd"}, 200)
	ac := login(t, s, "alice@example.test", "Passw0rd")
	bc := login(t, s, "bob@example.test", "Passw0rd")

	// LINK-01：两个链接共用额度、用量分开；删掉一个后它的地址立即失效，另一个不受影响。
	second := mustJSON(t, ac, "POST", "/api/v1/me/links", map[string]any{"name": "平板"}, 200)
	tok1, tok2 := first["token"].(string), second["token"].(string)
	if code, _ := subLinks(t, ac, tok2); code != 200 {
		t.Fatalf("second link = %d", code)
	}
	var keys []string
	for _, c := range g.config(0).Credentials {
		if c.UserKey == userKey(aid) {
			keys = append(keys, c.StatsKey)
		}
	}
	if len(keys) != 2 || keys[0] == keys[1] {
		t.Fatalf("two links must meter separately under one budget: %v", keys)
	}
	mustJSON(t, ac, "DELETE", "/api/v1/me/links/"+itoa(int(second["id"].(float64))), nil, 204)
	if code, _ := subLinks(t, ac, tok2); code != 404 {
		t.Fatalf("deleted link = %d", code)
	}
	if code, uris := subLinks(t, ac, tok1); code != 200 || len(uris) != 1 {
		t.Fatalf("remaining link = %d %v", code, uris)
	}
	if !auditHas(t, s, aid, "link.delete", "link:"+itoa(int(second["id"].(float64))), "ok") {
		t.Fatal("link delete must be audited with the user as actor")
	}

	// CHAIN-02：本机、内网、云元数据地址一律拒绝；不可达时保存失败并写审计；关闭权限后不能新建。
	for _, addr := range []string{"127.0.0.1", "localhost", "10.0.0.8", "192.168.1.2", "169.254.169.254", "[::1]", "fd00::1"} {
		w := ac.do("POST", "/api/v1/me/chains", map[string]any{"name": "家宽", "entry_node_id": id1, "exit": map[string]any{"kind": "socks5", "address": addr, "port": 1080}}, nil)
		if w.Code != 422 || !strings.Contains(w.Body.String(), "exit_not_public") {
			t.Fatalf("chain exit %s = %d %s", addr, w.Code, w.Body.String())
		}
	}
	if w := ac.do("POST", "/api/v1/me/chains", map[string]any{"name": "家宽", "entry_node_id": id1, "exit": map[string]any{"kind": "socks5", "address": "203.0.113.9", "port": 1080}}, nil); w.Code != 422 || !strings.Contains(w.Body.String(), "chain_probe_failed") {
		t.Fatalf("unreachable exit = %d %s", w.Code, w.Body.String())
	}
	if !auditHas(t, s, aid, "chain.create", "user:"+itoa(aid), "failed") {
		t.Fatal("failed chain must be audited")
	}
	ch := mustJSON(t, ac, "POST", "/api/v1/me/chains", map[string]any{"name": "家宽", "entry_node_id": id1, "exit": map[string]any{"kind": "socks5", "address": "203.0.113.5", "port": 1080, "username": "u", "password": "home-secret"}}, 200)
	cid := int(ch["id"].(float64))
	if !auditHas(t, s, aid, "chain.create", "chain:"+itoa(cid), "ok") {
		t.Fatal("chain create must be audited")
	}

	// 管理员关闭自建权限后不能再新建；重新打开后恢复。
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(aid), map[string]any{"allow_chain": false}, 200)
	mustJSON(t, ac, "POST", "/api/v1/me/chains", map[string]any{"name": "x", "entry_node_id": id1, "exit": map[string]any{"kind": "socks5", "address": "203.0.113.5", "port": 1080}}, 403)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(aid), map[string]any{"allow_chain": true}, 200)

	// ACL-01：bob 换成 alice 的链接、链式代理、节点或用户编号，读写全部被拒。
	lid := itoa(int(first["id"].(float64)))
	for _, r := range [][3]string{
		{"PATCH", "/api/v1/me/links/" + lid, `{"name":"x"}`},
		{"DELETE", "/api/v1/me/links/" + lid, ""},
		{"POST", "/api/v1/me/links/" + lid + "/rotate", ""},
		{"PATCH", "/api/v1/me/chains/" + itoa(cid), `{"name":"x"}`},
		{"DELETE", "/api/v1/me/chains/" + itoa(cid), ""},
	} {
		var body any
		if r[2] != "" {
			_ = json.Unmarshal([]byte(r[2]), &body)
		}
		if w := bc.do(r[0], r[1], body, nil); w.Code != 404 {
			t.Fatalf("bob %s %s = %d", r[0], r[1], w.Code)
		}
	}
	for _, path := range []string{"/api/v1/admin/users/" + itoa(aid), "/api/v1/admin/users/" + itoa(aid) + "/links", "/api/v1/admin/users/" + itoa(aid) + "/history", "/api/v1/admin/audit", "/api/v1/admin/backup/export"} {
		if w := bc.do("GET", path, nil, nil); w.Code != 403 {
			t.Fatalf("bob GET %s = %d", path, w.Code)
		}
	}
	bobLinks := mustJSON(t, bc, "GET", "/api/v1/me/links", nil, 200)["items"].([]any)
	bobChains := mustJSON(t, bc, "GET", "/api/v1/me/chains", nil, 200)["items"].([]any)
	if len(bobLinks) != 1 || len(bobChains) != 0 || strings.Contains(bc.do("GET", "/api/v1/me/subscription", nil, nil).Body.String(), tok1) {
		t.Fatalf("bob sees alice's resources: links=%v chains=%v", bobLinks, bobChains)
	}
	// alice 不能把没分给她的节点放进链接或作为链式代理入口。
	mustJSON(t, ac, "POST", "/api/v1/me/links", map[string]any{"all_nodes": false, "node_ids": []int{id2}}, 400)
	mustJSON(t, ac, "POST", "/api/v1/me/chains", map[string]any{"name": "x", "entry_node_id": id2, "exit": map[string]any{"kind": "socks5", "address": "203.0.113.5", "port": 1080}}, 422)

	// ACL-02：管理员看到 alice 的完整订阅地址与出口密码原文，并可停用、重置、删除。
	detail = mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(aid), nil, 200)
	if items := detail["chain_items"].([]any); len(items) != 1 || items[0].(map[string]any)["exit_password"] != "home-secret" {
		t.Fatalf("admin chain view = %v", detail["chain_items"])
	}
	rotated := mustJSON(t, admin, "POST", "/api/v1/admin/users/"+itoa(aid)+"/links/"+lid+"/rotate", nil, 200)
	if rotated["token"] == tok1 {
		t.Fatalf("rotate kept the token: %v", rotated)
	}
	if code, _ := subLinks(t, admin, tok1); code != 404 {
		t.Fatalf("old token after rotate = %d", code)
	}
	tok1 = rotated["token"].(string)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(aid)+"/links/"+lid, map[string]any{"enabled": false}, 200)
	if code, _ := subLinks(t, admin, tok1); code != 404 {
		t.Fatalf("disabled link = %d", code)
	}
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(aid)+"/links/"+lid, map[string]any{"enabled": true}, 200)
	if !auditHas(t, s, me, "link.rotate", "link:"+lid, "ok") || !auditHas(t, s, me, "link.update", "link:"+lid, "ok") {
		t.Fatal("admin link actions must be audited")
	}

	// SUB-02：重复获取订阅不新建凭据、链接或节点。
	count := func() [3]int {
		c, _ := s.Store.Ent.Credential.Query().Count(ctx)
		l, _ := s.Store.Ent.Link.Query().Count(ctx)
		n, _ := s.Store.Ent.Node.Query().Count(ctx)
		return [3]int{c, l, n}
	}
	before := count()
	for _, ua := range []string{"v2rayN/7.0", "clash.meta/1.19", "SFA/1.12", "v2rayN/7.0"} {
		if w := admin.do("GET", "/sub/"+tok1, nil, map[string]string{"User-Agent": ua}); w.Code != 200 {
			t.Fatalf("sub %s = %d", ua, w.Code)
		}
	}
	mustJSON(t, admin, "GET", "/api/v1/sub-info/"+tok1, nil, 200)
	if after := count(); after != before {
		t.Fatalf("fetching subscriptions changed resources: %v -> %v", before, after)
	}

	// SUB-01：订阅里只有本人的凭据；停用的节点不写入；停用的用户订阅 404。
	_, uris := subLinks(t, admin, tok1)
	var mine []string
	creds, _ := s.Store.Ent.Credential.Query().Where(credential.LinkIDEQ(int(first["id"].(float64)))).All(ctx)
	for _, c := range creds {
		mine = append(mine, c.UUID+c.Password)
	}
	others, _ := s.Store.Ent.Credential.Query().Where(credential.UserIDNEQ(aid)).All(ctx)
	for _, u := range uris {
		for _, o := range others {
			if (o.UUID != "" && strings.Contains(u, o.UUID)) || (o.Password != "" && strings.Contains(u, o.Password)) {
				t.Fatalf("subscription leaks another user's credential: %s", u)
			}
		}
	}
	if len(uris) != 2 || len(mine) != 2 { // REALITY 入口 + 家宽链
		t.Fatalf("alice subscription = %v (own creds %d)", uris, len(mine))
	}
	s.Store.Ent.Node.Update().Where(node.IDEQ(id1)).SetEnabled(false).ExecX(ctx)
	if code, uris := subLinks(t, admin, tok1); code != 200 || len(uris) != 0 {
		t.Fatalf("subscription with disabled node = %d %v", code, uris)
	}
	s.Store.Ent.Node.Update().Where(node.IDEQ(id1)).SetEnabled(true).ExecX(ctx)
	// 到期、额度用尽：订阅照常返回节点并在响应头标明用量与到期，由 Gate 拒绝连接（凭据带到期时间，额度为 0 时不再分配）。
	past := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(aid), map[string]any{"expires_at": past}, 200)
	w := admin.do("GET", "/sub/"+tok1, nil, map[string]string{"User-Agent": "v2rayN/7.0"})
	if w.Code != 200 || !strings.Contains(w.Header().Get("Subscription-Userinfo"), "expire="+itoa(int(past.Unix()))) {
		t.Fatalf("expired user's subscription = %d %v", w.Code, w.Header())
	}
	for _, c := range g.config(0).Credentials {
		if c.UserKey == userKey(aid) && (c.ExpiresAt == nil || c.ExpiresAt.After(time.Now())) {
			t.Fatalf("expired user's credential must carry the past expiry: %+v", c)
		}
	}
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(aid), map[string]any{"expires_at": nil}, 200)
	s.Store.Ent.User.UpdateOneID(aid).SetUsed(30 << 30).ExecX(ctx)
	_ = s.SyncUser(ctx, aid)
	if b := g.config(0).Budgets[userKey(aid)]; b.Allocated > b.Used {
		t.Fatalf("exhausted user still has budget: %+v", b)
	}
	mustJSON(t, admin, "PATCH", "/api/v1/admin/users/"+itoa(aid), map[string]any{"enabled": false}, 200)
	if code, _ := subLinks(t, admin, tok1); code != 404 {
		t.Fatalf("disabled user's subscription = %d", code)
	}

	// 删除用户：链接一并删除并写审计。
	mustJSON(t, admin, "DELETE", "/api/v1/admin/users/"+itoa(aid), nil, 204)
	if n, _ := s.Store.Ent.Link.Query().Where(link.UserIDEQ(aid)).Count(ctx); n != 0 || !auditHas(t, s, me, "user.delete", "user:"+itoa(aid), "ok") {
		t.Fatalf("delete user: links=%d", n)
	}
}
