package center

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/audit"
	entnode "github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/user"
	"github.com/shusfun/weir/packages/contracts"
)

// 声明式能力：dsh 把建节点、建用户、建链接拼成一份草稿；试运行调用同一个处理函数但不写入，
// 实测只做一次，确认时按试运行定下的端口保存；任一步失败已执行的步骤撤回。
func TestComposedDraftCreatesNodeUserLinkAndRollsBack(t *testing.T) {
	s, admin, g := setupGate(t)
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	ctx := t.Context()
	var seen []map[string]any
	stop := make(chan struct{})
	defer func() {
		close(stop)
		s.enqueueAction(ctx, int(g.id), contracts.ActionDiagnostics, nil, time.Second)
	}()
	g.serveNodeTests(t, 0, &seen, stop)

	if text, isErr := toolText(t, h, m.webToken, "list_presets", map[string]any{}); isErr || !strings.Contains(text, "vless-reality") || strings.Contains(text, `"key": "<redacted>"`) {
		t.Fatalf("list_presets = %q", text)
	}
	submit := func(steps ...any) (string, bool) {
		return toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "建节点并开用户", "steps": steps})
	}
	text, isErr := submit(
		map[string]any{"action": "node.create", "as": "n", "args": map[string]any{"gate_id": g.id, "preset": "vless-reality", "name": "HK-REALITY", "reality_target": "www.apple.com"}},
		map[string]any{"action": "user.create", "as": "u", "args": map[string]any{"email": "dsh@example.test", "node_ids": []any{"$n"}}},
		map[string]any{"action": "link.create", "args": map[string]any{"user_id": "$u", "name": "手机"}},
	)
	if isErr {
		t.Fatal(text)
	}
	// 试运行没有写入任何数据，只在 VPS 上实测了一次。
	if n, _ := s.Store.Ent.Node.Query().Count(ctx); n != 0 || len(seen) != 1 {
		t.Fatalf("dry run wrote %d nodes, gate tests %d", n, len(seen))
	}
	d := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)[0].(map[string]any)
	first := d["steps"].([]any)[0].(map[string]any)
	port := 0
	for _, f := range first["fields"].([]any) {
		if fm := f.(map[string]any); fm["key"] == "port" {
			port = int(fm["value"].(float64))
		}
	}
	// REALITY 自动选中 443 时不写回（手动端口要 ≥ 1024），确认时再次选中。
	if port != 0 || first["result"] == nil || !strings.Contains(first["title"].(string), "HK-REALITY") || !strings.Contains(mustString(first["preview"]), "443") {
		t.Fatalf("node step = %v", first)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", nil, 200)
	n, err := s.Store.Ent.Node.Query().Only(ctx)
	if err != nil || n.Port != 443 || n.Settings["reality_dest"] != "www.apple.com:443" {
		t.Fatalf("node = %+v %v", n, err)
	}
	if len(seen) != 1 {
		t.Fatalf("confirming a tested draft must not test again: %d", len(seen))
	}
	u, err := s.Store.Ent.User.Query().Where(user.EmailEQ("dsh@example.test")).Only(ctx)
	if err != nil || len(u.NodeIds) != 1 || u.NodeIds[0] != n.ID {
		t.Fatalf("user = %+v %v", u, err)
	}

	// 第三步失败（邮箱重复）：前面新建的节点与用户都撤回。
	text, isErr = submit(
		map[string]any{"action": "node.create", "as": "n2", "args": map[string]any{"gate_id": g.id, "preset": "ss2022"}},
		map[string]any{"action": "user.create", "args": map[string]any{"email": "twin2@example.test", "node_ids": []any{"$n2"}}},
		map[string]any{"action": "user.create", "args": map[string]any{"email": "twin2@example.test"}},
	)
	if isErr {
		t.Fatal(text)
	}
	list := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)
	w := admin.do("POST", "/api/v1/admin/ai/drafts/"+list[len(list)-1].(map[string]any)["id"].(string)+"/apply", nil, nil)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "已撤回") {
		t.Fatalf("failing draft = %d %s", w.Code, w.Body.String())
	}
	if c, _ := s.Store.Ent.Node.Query().Count(ctx); c != 1 {
		t.Fatalf("nodes after rollback = %d", c)
	}
	if c, _ := s.Store.Ent.User.Query().Where(user.EmailEQ("twin2@example.test")).Count(ctx); c != 0 {
		t.Fatalf("users after rollback = %d", c)
	}

	// 自动分配的随机端口写回草稿：确认时用同一个端口，不重新挑选。
	if text, isErr := submit(map[string]any{"action": "node.create", "args": map[string]any{"gate_id": g.id, "preset": "ss2022"}}); isErr {
		t.Fatal(text)
	}
	list = mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)
	ss := list[len(list)-1].(map[string]any)
	pinned := 0
	for _, f := range ss["steps"].([]any)[0].(map[string]any)["fields"].([]any) {
		if fm := f.(map[string]any); fm["key"] == "port" {
			pinned = int(fm["value"].(float64))
		}
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+ss["id"].(string)+"/apply", nil, 200)
	if pinned < 1024 {
		t.Fatalf("pinned port = %d", pinned)
	}
	if c, _ := s.Store.Ent.Node.Query().Where(entnode.PortEQ(pinned)).Count(ctx); c != 1 {
		t.Fatalf("node with pinned port %d not created", pinned)
	}

	// 删除只能是最后一步，且要当前密码与二次验证码。
	if text, isErr := submit(map[string]any{"action": "node.delete", "args": map[string]any{"node_id": n.ID}}, map[string]any{"action": "user.reset_usage", "args": map[string]any{"user_id": u.ID}}); !isErr || !strings.Contains(text, "最后一步") {
		t.Fatalf("delete not last = %q", text)
	}
	if text, isErr := submit(map[string]any{"action": "node.delete", "args": map[string]any{"node_id": n.ID}}); isErr || !strings.Contains(text, "管理员确认") {
		t.Fatalf("delete draft = %q", text)
	}
	list = mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)
	if last := list[len(list)-1].(map[string]any); last["danger"] != true {
		t.Fatalf("delete draft must be dangerous: %v", last)
	}
}

// 机密参数不经过 dsh：草稿里填了就拒绝；由管理员在确认窗里填，审计与只读工具都不出现原值。
func TestSecretParamsFilledOnlyInConfirmWindow(t *testing.T) {
	s, admin := newInstalledServer(t)
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	ctx := t.Context()
	const secretURL = "https://sub.example.com/api/v1/client/subscribe?token=TOPSECRET123"
	submit := func(args map[string]any) (string, bool) {
		return toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "加订阅源", "steps": []any{map[string]any{"action": "source.create", "as": "src", "args": args}}})
	}
	if text, isErr := submit(map[string]any{"name": "机场 A", "url": secretURL}); !isErr || !strings.Contains(text, "机密") {
		t.Fatalf("secret from model = %q", text)
	}
	if text, isErr := submit(map[string]any{"name": "机场 A"}); isErr {
		t.Fatal(text)
	}
	d := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)[0].(map[string]any)
	id := d["id"].(string)
	step := d["steps"].([]any)[0].(map[string]any)
	found := false
	for _, f := range step["fields"].([]any) {
		if fm := f.(map[string]any); fm["key"] == "url" {
			found = fm["secret"] == true && fm["display"] == "未填"
		}
	}
	if !found || !strings.Contains(mustString(step["preview"]), "不经过 dsh") {
		t.Fatalf("secret field = %v", step)
	}
	// 管理员没填就确认：要求填写；填了才保存。
	if w := admin.do("POST", "/api/v1/admin/ai/drafts/"+id+"/apply", nil, nil); w.Code != 422 || !strings.Contains(w.Body.String(), "订阅地址") {
		t.Fatalf("apply without secret = %d %s", w.Code, w.Body.String())
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+id+"/apply", map[string]any{"edits": map[string]any{"0": map[string]any{"url": secretURL}}}, 200)
	src, err := s.Store.Ent.SubSource.Query().Only(ctx)
	if err != nil || src.URL != secretURL || src.Name != "机场 A" {
		t.Fatalf("source = %+v %v", src, err)
	}
	a, err := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("ai.apply")).Only(ctx)
	if err != nil || strings.Contains(mustString(a.Detail), "TOPSECRET") {
		t.Fatalf("audit leaks secret: %v %v", a, err)
	}
	if text, _ := toolText(t, h, m.webToken, "list_sources", map[string]any{}); strings.Contains(text, "TOPSECRET") || !strings.Contains(text, "sub.example.com") {
		t.Fatalf("list_sources = %s", text)
	}
	// 只改名称：订阅地址不给就不改，确认窗里显示「不改」。
	if text, isErr := toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "改名", "steps": []any{map[string]any{"action": "source.update", "args": map[string]any{"source_id": src.ID, "name": "机场 B"}}}}); isErr {
		t.Fatal(text)
	}
	d = mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)[0].(map[string]any)
	if b := mustString(d); strings.Contains(b, "TOPSECRET") || !strings.Contains(b, "不改") {
		t.Fatalf("update draft = %s", b)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", nil, 200)
	if src, _ = s.Store.Ent.SubSource.Get(ctx, src.ID); src.URL != secretURL || src.Name != "机场 B" {
		t.Fatalf("source after rename = %+v", src)
	}
}

// 处理函数整体覆盖的对象（出口）：只改名称时，没给的字段与密码用现值补齐，不被清空；只读工具不给密码。
func TestOutboundUpdateKeepsPasswordAndHidesIt(t *testing.T) {
	s, admin, g := setupGate(t)
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	gid := itoa(int(g.id))
	o := mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+gid+"/outbounds", map[string]any{"tag": "home", "protocol": "socks", "address": "203.0.113.9", "port": 1080, "username": "u", "password": "PW-SECRET"}, 200)
	oid := int(o["id"].(float64))
	if text, _ := toolText(t, h, m.webToken, "list_outbounds", map[string]any{"gate_id": g.id}); strings.Contains(text, "PW-SECRET") || !strings.Contains(text, `"has_password": true`) {
		t.Fatalf("list_outbounds = %s", text)
	}
	if text, isErr := toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "改名", "steps": []any{map[string]any{"action": "outbound.update", "args": map[string]any{"outbound_id": oid, "name": "家宽"}}}}); isErr {
		t.Fatal(text)
	}
	d := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)[0].(map[string]any)
	if strings.Contains(mustString(d), "PW-SECRET") {
		t.Fatalf("draft leaks password: %v", d)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", nil, 200)
	got, _ := s.Store.Ent.Outbound.Get(t.Context(), oid)
	if pw, _ := s.reveal(got.Password); pw != "PW-SECRET" || got.Name != "家宽" || got.Address != "203.0.113.9" || got.Username != "u" {
		t.Fatalf("outbound = %+v pw=%q", got, pw)
	}
}

// 每项声明式草稿能力的试运行都在第一次写入前停下：只提交、不确认时数据库与任务都不变。
func TestOpDraftDryRunsWriteNothing(t *testing.T) {
	s, admin, g := setupGate(t)
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	ctx := t.Context()
	gid := itoa(int(g.id))
	out := mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+gid+"/outbounds", map[string]any{"tag": "home", "protocol": "socks", "address": "203.0.113.9", "port": 1080}, 200)
	rt := mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+gid+"/routes", map[string]any{"outbound_tag": "home", "domain": "domain:a.com"}, 200)
	src := mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/sources", map[string]any{"name": "A", "url": "https://sub.example.com/x"}, 200)
	prof := mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/profiles", map[string]any{"name": "P"}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "dry@example.test"}, 200)
	yaml := "proxy-groups:\n  - name: P\n    type: select\n    proxies: [DIRECT]\nrules:\n  - MATCH,P\n"
	snapshot := func() string {
		count := func(n int, _ error) int { return n }
		return mustString([]any{
			count(s.Store.Ent.Gate.Query().Count(ctx)), s.mustGateName(t, int(g.id)),
			count(s.Store.Ent.Outbound.Query().Count(ctx)), count(s.Store.Ent.Route.Query().Count(ctx)),
			count(s.Store.Ent.SubSource.Query().Count(ctx)), count(s.Store.Ent.SubProfile.Query().Count(ctx)), count(s.Store.Ent.SubGrant.Query().Count(ctx)),
			count(s.Store.Ent.RuleTemplate.Query().Count(ctx)), count(s.Store.Ent.Link.Query().Count(ctx)), len(s.jobs.List("")),
		})
	}
	before := snapshot()
	steps := []map[string]any{
		{"action": "gate.update", "args": map[string]any{"gate_id": g.id, "name": "HK-2"}},
		{"action": "outbound.create", "args": map[string]any{"gate_id": g.id, "tag": "home2", "protocol": "freedom"}},
		{"action": "outbound.update", "args": map[string]any{"outbound_id": out["id"], "name": "x"}},
		{"action": "route.create", "args": map[string]any{"gate_id": g.id, "outbound_tag": "home", "domain": "domain:b.com"}},
		{"action": "route.update", "args": map[string]any{"route_id": rt["id"], "priority": 5}},
		{"action": "source.update", "args": map[string]any{"source_id": src["id"], "name": "B"}},
		{"action": "profile.create", "args": map[string]any{"name": "Q"}},
		{"action": "profile.grant", "args": map[string]any{"profile_id": prof["id"], "user_ids": []any{u["id"]}}},
		{"action": "template.create", "args": map[string]any{"name": "T", "format": "yaml", "content": yaml}},
		{"action": "kernel.fetch", "args": map[string]any{"kernel": "xray", "version": "26.3.28"}},
	}
	for _, st := range steps {
		if text, isErr := toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "试运行", "steps": []any{st}}); isErr {
			t.Fatalf("%s: %s", st["action"], text)
		}
		if after := snapshot(); after != before {
			t.Fatalf("%s dry run wrote data: %s → %s", st["action"], before, after)
		}
	}
	for _, st := range []map[string]any{
		{"action": "outbound.delete", "args": map[string]any{"outbound_id": out["id"]}},
		{"action": "source.delete", "args": map[string]any{"source_id": src["id"]}},
		{"action": "profile.delete", "args": map[string]any{"profile_id": prof["id"]}},
	} {
		text, _ := toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "试运行", "steps": []any{st}})
		if after := snapshot(); after != before {
			t.Fatalf("%s dry run wrote data (%s): %s → %s", st["action"], text, before, after)
		}
	}
}

func (s *Server) mustGateName(t *testing.T, id int) string {
	g, err := s.Store.Ent.Gate.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return g.Name
}

// 只读工具的引用参数：参数说明写明整数类型、不提 "$名字"；模型把编号传成字符串（"1"）也能用，
// 传名称或 "$名字" 时报错说清该填什么；失败的工具调用带排查编号写进中心日志（泳道「Weir MCP」）。
func TestReadToolRefsAcceptStringIDsAndExplainErrors(t *testing.T) {
	s, admin, g := setupGate(t)
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	var seen []map[string]any
	stop := make(chan struct{})
	defer func() {
		close(stop)
		s.enqueueAction(t.Context(), int(g.id), contracts.ActionDiagnostics, nil, time.Second)
	}()
	g.serveNodeTests(t, 0, &seen, stop)
	gid := itoa(int(g.id))
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "ss2022", "skip_probe": true}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "ref@example.test"}, 200)

	_, list := mcpCall(t, h, m.webToken, "tools/list", nil)
	for _, tool := range list["tools"].([]any) {
		tm := tool.(map[string]any)
		for key, p := range tm["inputSchema"].(map[string]any)["properties"].(map[string]any) {
			pm := p.(map[string]any)
			desc, _ := pm["description"].(string)
			if strings.HasSuffix(key, "_id") && tm["name"] != "submit_draft" && (pm["type"] != "integer" || strings.Contains(desc, "$")) {
				t.Fatalf("%s.%s schema = %v", tm["name"], key, pm)
			}
		}
	}

	// dsh 线上实际传的就是字符串编号。
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"get_gate", map[string]any{"gate_id": gid}},
		{"check_port", map[string]any{"gate_id": gid, "reality": true}},
		{"probe_reality", map[string]any{"gate_id": gid}},
		{"list_outbounds", map[string]any{"gate_id": gid}},
		{"list_routes", map[string]any{"gate_id": gid}},
		{"get_kernel_diagnostics", map[string]any{"gate_id": gid, "kernel": "sing-box"}},
		{"get_node_state", map[string]any{"node_id": itoa(int(n["id"].(float64)))}},
		{"test_node", map[string]any{"node_id": itoa(int(n["id"].(float64)))}},
		{"get_user", map[string]any{"user_id": itoa(int(u["id"].(float64)))}},
	} {
		if text, isErr := toolText(t, h, m.webToken, c.tool, c.args); isErr {
			t.Fatalf("%s with string id = %s", c.tool, text)
		}
	}

	var logs strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)
	text, isErr := toolText(t, h, m.webToken, "check_port", map[string]any{"gate_id": "本机"})
	if !isErr || !strings.Contains(text, "不能填名称") || !strings.Contains(text, "list_gates") || !strings.Contains(text, "排查编号") {
		t.Fatalf("name instead of id = %q", text)
	}
	trace := text[strings.LastIndex(text, "排查编号 ")+len("排查编号 "):]
	trace = strings.TrimSuffix(trace, "）")
	if line := logs.String(); !strings.Contains(line, "mcp tool failed") || !strings.Contains(line, "trace="+trace) || !strings.Contains(line, "gate_id:string") {
		t.Fatalf("mcp lane log = %s", line)
	}
	// 模型误填的机密被拒绝，日志里只有参数概况，没有值。
	logs.Reset()
	toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "x", "steps": []any{map[string]any{"action": "source.create", "args": map[string]any{"url": "https://sub.example.com/?token=LEAKME"}}}})
	if line := logs.String(); !strings.Contains(line, "steps:[source.create]") || strings.Contains(line, "LEAKME") {
		t.Fatalf("secret in mcp lane log = %s", line)
	}
	if text, isErr := toolText(t, h, m.webToken, "get_gate", map[string]any{"gate_id": "$1"}); !isErr || !strings.Contains(text, "只能在 submit_draft") {
		t.Fatalf("placeholder in read tool = %q", text)
	}
	// 草稿里字符串编号同样能用，执行时处理函数拿到的是数字。
	if text, isErr := toolText(t, h, m.webToken, "submit_draft", map[string]any{"explain": "改名", "steps": []any{map[string]any{"action": "gate.update", "args": map[string]any{"gate_id": gid, "name": "HK-9"}}}}); isErr {
		t.Fatal(text)
	}
	d := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)[0].(map[string]any)
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d["id"].(string)+"/apply", nil, 200)
	if s.mustGateName(t, int(g.id)) != "HK-9" {
		t.Fatal("gate not renamed")
	}
}
