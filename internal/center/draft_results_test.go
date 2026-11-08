package center

import (
	"fmt"
	"strings"
	"testing"

	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/packages/contracts"
)

const drawerA, drawerB = "drawer-aaaaaaaaaaaaaaaa", "drawer-bbbbbbbbbbbbbbbb"

func reportDrawer(t *testing.T, admin *testClient, drawer string) {
	t.Helper()
	w := admin.do("POST", "/api/v1/admin/dsh/context", map[string]any{"drawer": drawer, "visible": true, "drawer_open": true, "page": "VPS 与节点", "declared": true}, nil)
	if w.Code != 204 {
		t.Fatalf("report context = %d %s", w.Code, w.Body.String())
	}
}

func latestDraft(t *testing.T, admin *testClient) map[string]any {
	t.Helper()
	list := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)
	if len(list) == 0 {
		t.Fatal("no pending draft")
	}
	return list[len(list)-1].(map[string]any)
}

// 节点的全部流量经代理：密码只在确认窗里填；没填不能确认；确认时 VPS 真连，不通就不生效；结果 dsh 下一轮读得到。
func TestNodeChainProxyDraft(t *testing.T) {
	s, admin, g := setupGate(t)
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	ctx := t.Context()
	stop := make(chan struct{})
	defer close(stop)
	g.serveChainProbes("203.0.113.66", stop)
	reportDrawer(t, admin, drawerA)

	args := func(addr string) map[string]any {
		return map[string]any{"gate_id": g.id, "preset": "ss2022", "name": "经代理", "chain_kind": "http", "chain_address": addr, "chain_port": 8080, "chain_username": "u"}
	}
	submit := func(a map[string]any) (string, bool) {
		return toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "经代理的节点", "steps": []any{map[string]any{"action": "node.create", "args": a}}})
	}
	// 直连节点的确认窗不问代理密码。
	if text, isErr := submit(map[string]any{"gate_id": g.id, "preset": "ss2022", "name": "直连"}); isErr {
		t.Fatal(text)
	}
	direct := latestDraft(t, admin)
	if b := mustString(direct); strings.Contains(b, "代理密码") || strings.Contains(b, "出口节点链接") {
		t.Fatalf("direct node asks for proxy secrets: %s", b)
	}
	mustJSON(t, admin, "DELETE", "/api/v1/admin/ai/drafts/"+direct["id"].(string), nil, 204)
	withSecret := args("203.0.113.5")
	withSecret["chain_password"] = "PROXY-SECRET"
	if text, isErr := submit(withSecret); !isErr || !strings.Contains(text, "机密") {
		t.Fatalf("secret from model = %q", text)
	}
	if text, isErr := submit(args("203.0.113.66")); isErr {
		t.Fatal(text)
	}
	d := latestDraft(t, admin)
	apply := "/api/v1/admin/ai/drafts/" + d["id"].(string) + "/apply"
	if w := admin.do("POST", apply, nil, nil); w.Code != 422 || !strings.Contains(w.Body.String(), "代理密码") {
		t.Fatalf("apply without password = %d %s", w.Code, w.Body.String())
	}
	// 未通过的出口只保存候选，不创建节点，结果只含脱敏状态。
	queued := mustJSON(t, admin, "POST", apply, map[string]any{"edits": map[string]any{"0": map[string]any{"chain_password": "PROXY-SECRET"}}}, 200)
	if len(queued["outputs"].([]any)) != 1 || strings.Contains(mustString(queued), "PROXY-SECRET") {
		t.Fatal(queued)
	}
	if s.Store.Ent.Node.Query().CountX(ctx) != 0 {
		t.Fatal("未验证出口提前创建节点")
	}
	text, _ := toolText(t, h, m.webToken, "get_console_context", map[string]any{})
	if strings.Contains(text, "PROXY-SECRET") {
		t.Fatal("秘密进入上下文")
	}

	if text, isErr := submit(args("203.0.113.5")); isErr {
		t.Fatal(text)
	}
	d = latestDraft(t, admin)
	out := mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", map[string]any{"edits": map[string]any{"0": map[string]any{"chain_password": "PROXY-SECRET"}}}, 200)
	n := s.Store.Ent.Node.Query().OnlyX(ctx)
	ex := s.Store.Ent.Exit.GetX(ctx, n.ExitID)
	if ex.Kind != entexit.KindHTTP || ex.Address != "203.0.113.5" || ex.Username != "u" {
		t.Fatalf("node chain = %+v", n)
	}
	if pw, _ := s.reveal(ex.Password); pw != "PROXY-SECRET" {
		t.Fatalf("chain password = %q", pw)
	}
	if ids, _ := out["node_ids"].([]any); len(ids) != 1 || int(ids[0].(float64)) != n.ID {
		t.Fatalf("apply result = %v", out)
	}
	text, _ = toolText(t, h, m.webToken, "get_console_context", map[string]any{})
	if !strings.Contains(text, draftApplied) || !strings.Contains(text, "经代理") || !strings.Contains(text, `"nodes"`) || strings.Contains(text, "PROXY-SECRET") {
		t.Fatalf("context after apply = %s", text)
	}
}

// 新建出口与分流规则放进一份草稿；出口有用户名时密码必填；不通时一步都不执行。
func TestOutboundAndRouteInOneDraft(t *testing.T) {
	s, admin, g := setupGate(t)
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	ctx := t.Context()
	stop := make(chan struct{})
	defer close(stop)
	g.serveChainProbes("203.0.113.66", stop)

	steps := func(addr string) map[string]any {
		return map[string]any{"explain": "流媒体走家宽", "steps": []any{
			map[string]any{"action": "exit.create", "as": "home", "args": map[string]any{"gate_id": g.id, "remark": "家宽", "kind": "http", "address": addr, "port": 8080, "username": "u"}},
			map[string]any{"action": "route.create", "args": map[string]any{"gate_id": g.id, "exit_id": "$home", "domain": "geosite:netflix"}},
		}}
	}
	if text, isErr := toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "x", "steps": []any{
		map[string]any{"action": "route.create", "args": map[string]any{"gate_id": g.id, "exit_id": "$nope", "domain": "geosite:netflix"}},
	}}); !isErr || !strings.Contains(text, "$nope") {
		t.Fatalf("unknown tag ref = %q", text)
	}
	if text, isErr := toolText(t, h, m.webToken, "submit_draft", steps("203.0.113.66")); isErr {
		t.Fatal(text)
	}
	d := latestDraft(t, admin)
	apply := "/api/v1/admin/ai/drafts/" + d["id"].(string) + "/apply"
	if w := admin.do("POST", apply, nil, nil); w.Code != 422 || !strings.Contains(w.Body.String(), "密码") {
		t.Fatalf("apply without password = %d %s", w.Code, w.Body.String())
	}
	edits := map[string]any{"edits": map[string]any{"0": map[string]any{"password": "PROXY-SECRET"}}}
	if w := admin.do("POST", apply, edits, nil); w.Code != 422 {
		t.Fatalf("apply unreachable = %d %s", w.Code, w.Body.String())
	}
	if n := s.Store.Ent.Exit.Query().Where(entexit.TagNEQ("direct")).CountX(ctx); n != 0 {
		t.Fatalf("unreachable exit saved: %d", n)
	}
	mustJSON(t, admin, "DELETE", "/api/v1/admin/ai/drafts/"+d["id"].(string), nil, 204)

	if text, isErr := toolText(t, h, m.webToken, "submit_draft", steps("203.0.113.5")); isErr {
		t.Fatal(text)
	}
	d = latestDraft(t, admin)
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", edits, 200)
	r := s.Store.Ent.Route.Query().OnlyX(ctx)
	if r.GateID != int(g.id) {
		t.Fatalf("route = %+v", r)
	}
}

// 撤回只限提交它的 dsh 会话；撤回后不再弹确认窗，dsh 读得到结果。
func TestWithdrawDraftOnlyFromSameSession(t *testing.T) {
	s, admin := newInstalledServer(t)
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	reportDrawer(t, admin, drawerA)
	if text, isErr := toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "加订阅源", "steps": []any{map[string]any{"action": "source.create", "args": map[string]any{"name": "机场 A"}}}}); isErr {
		t.Fatal(text)
	}
	id := latestDraft(t, admin)["id"].(string)

	reportDrawer(t, admin, drawerB)
	if text, isErr := toolText(t, h, m.webToken, "withdraw_draft", map[string]any{"draft": id}); !isErr || !strings.Contains(text, "不是这个会话") {
		t.Fatalf("withdraw from other session = %q", text)
	}
	if text, _ := toolText(t, h, m.webToken, "get_console_context", map[string]any{}); strings.Contains(text, id) {
		t.Fatalf("other session sees draft: %s", text)
	}
	reportDrawer(t, admin, drawerA)
	if text, isErr := toolText(t, h, m.webToken, "withdraw_draft", map[string]any{"draft": id}); isErr {
		t.Fatal(text)
	}
	if list := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any); len(list) != 0 {
		t.Fatalf("withdrawn draft still pending: %v", list)
	}
	if text, _ := toolText(t, h, m.webToken, "get_console_context", map[string]any{}); !strings.Contains(text, draftWithdrawn) {
		t.Fatalf("context = %s", text)
	}
}

// 给 VPS 时列出每个方案能不能用及原因，dsh 与建节点页面都不用逐个试。
func TestPresetsForGate(t *testing.T) {
	s, admin, g := setupGate(t)
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	heartbeat(t, g, contracts.Heartbeat{ListeningPorts: []contracts.ListeningPort{{Port: 80, Proto: "tcp", Process: "nginx"}}})
	items := mustJSON(t, admin, "GET", "/api/v1/admin/presets?gate_id="+itoa(int(g.id)), nil, 200)["items"].([]any)
	state := map[string]map[string]any{}
	for _, it := range items {
		v := it.(map[string]any)
		state[v["id"].(string)] = v
	}
	if state["ss2022"]["available"] != true {
		t.Fatalf("ss2022 = %v", state["ss2022"])
	}
	if tr := state["trojan-tls"]; tr["available"] != false || !strings.Contains(fmt.Sprint(tr["why"]), "80 端口") {
		t.Fatalf("trojan = %v", tr)
	}
	if n := s.Store.Ent.Certificate.Query().CountX(t.Context()); n != 0 {
		t.Fatalf("checking availability issued %d certs", n)
	}
	if text, isErr := toolText(t, h, m.webToken, "list_presets", map[string]any{"gate_id": g.id}); isErr || !strings.Contains(text, "available") {
		t.Fatalf("list_presets with gate = %q", text)
	}
}
