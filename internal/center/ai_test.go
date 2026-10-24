package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imzyb/MiGate/packages/contracts"
)

// fakeGate 处理 node.test 动作：前 n 次返回失败，之后成功；记录收到的测试配置。
func (g *fakeGate) serveNodeTests(t *testing.T, fail int, seen *[]map[string]any, stop chan struct{}) {
	go func() {
		count := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			code, body := g.call("GET", "/api/v1/agent/actions", nil)
			if code != 200 {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			var acts []contracts.Action
			json.Unmarshal(body, &acts)
			for _, a := range acts {
				res := contracts.ActionResult{OK: true, Result: map[string]any{"stage": "connect", "latency_ms": 3}}
				if a.Kind == contracts.ActionNodeTest {
					*seen = append(*seen, a.Payload)
					if count < fail {
						res = contracts.ActionResult{Error: "经候选配置连接失败：EOF", Result: map[string]any{"stage": "connect"}}
					}
					count++
				}
				g.call("POST", "/api/v1/agent/actions/"+itoa(int(a.ID)), res)
			}
		}
	}()
}

// mcpCall 以给定令牌调用 MiGate 给 dsh 的 MCP 接口，返回 result。
func mcpCall(t *testing.T, h http.Handler, token, method string, params any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req := httptest.NewRequest("POST", "http://127.0.0.1/mcp", strings.NewReader(string(b)))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var out struct {
		Result map[string]any `json:"result"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out.Result
}

func toolText(t *testing.T, h http.Handler, token, name string, args any) (string, bool) {
	t.Helper()
	code, res := mcpCall(t, h, token, "tools/call", map[string]any{"name": name, "arguments": args})
	if code != 200 || res == nil {
		t.Fatalf("tools/call %s = %d %v", name, code, res)
	}
	content := res["content"].([]any)[0].(map[string]any)
	return content["text"].(string), res["isError"] == true
}

func TestDSHNodeDraftNeverLeaksSecretsAndNeedsPassingTest(t *testing.T) {
	s, admin, g := setupGate(t)
	node := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "vless-reality", "skip_probe": true}, 200)
	nid := int(node["id"].(float64))
	priv := node["settings"].(map[string]any)["reality_private_key"].(string)
	m, err := s.ensureMCP()
	if err != nil {
		t.Fatal(err)
	}
	h := s.mcpHandler(m)

	// 令牌不对一律拒绝；工具列表只有读取与提交草稿，没有任何「确认 / 执行」。
	if code, _ := mcpCall(t, h, "wrong", "tools/list", nil); code != 401 {
		t.Fatalf("bad token = %d", code)
	}
	_, list := mcpCall(t, h, m.webToken, "tools/list", nil)
	for _, tool := range list["tools"].([]any) {
		name := tool.(map[string]any)["name"].(string)
		// MCP SDK 要求 inputSchema.required 是数组（null 会让整个工具列表被拒绝）。
		if _, ok := tool.(map[string]any)["inputSchema"].(map[string]any)["required"].([]any); !ok {
			t.Fatalf("%s: inputSchema.required must be an array", name)
		}
		if strings.Contains(name, "apply") || strings.Contains(name, "confirm") || strings.Contains(name, "exec") {
			t.Fatalf("dsh must not get an executing tool: %s", name)
		}
	}
	state, _ := toolText(t, h, m.webToken, "get_node_state", map[string]any{"node_id": nid})
	if strings.Contains(state, priv) || !strings.Contains(state, "redacted") || !strings.Contains(state, "editable_fields") {
		t.Fatalf("node state must be redacted: %s", state)
	}

	var seen []map[string]any
	stop := make(chan struct{})
	defer func() {
		close(stop)
		// 唤醒假 Gate 挂起的动作长轮询，让它看到停止信号。
		s.enqueueAction(t.Context(), int(g.id), contracts.ActionDiagnostics, nil, time.Second)
	}()
	g.serveNodeTests(t, 1, &seen, stop)

	// 改私钥被系统拒绝；第一次合法草稿 Gate 实测失败，原因交回 dsh；修改后通过，等待管理员确认。
	if text, isErr := toolText(t, h, m.webToken, "submit_node_draft", map[string]any{"node_id": nid, "set": map[string]any{"reality_private_key": "stolen"}, "explain": "换密钥"}); !isErr || !strings.Contains(text, "机密") {
		t.Fatalf("secret change = %q", text)
	}
	if text, isErr := toolText(t, h, m.webToken, "submit_node_draft", map[string]any{"node_id": nid, "set": map[string]any{"reality_server_names": "www.apple.com", "reality_dest": "www.apple.com:443"}, "explain": "换伪装目标"}); !isErr || !strings.Contains(text, "未通过实测") {
		t.Fatalf("failed draft = %q", text)
	}
	if pending := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any); len(pending) != 0 {
		t.Fatalf("failed drafts must not ask for confirmation: %v", pending)
	}
	text, isErr := toolText(t, h, m.analystToken, "submit_node_draft", map[string]any{"node_id": nid, "set": map[string]any{"reality_server_names": "www.amazon.com", "reality_dest": "www.amazon.com:443"}, "explain": "改用 amazon"})
	if isErr || !strings.Contains(text, "管理员确认") || len(seen) != 2 {
		t.Fatalf("passing draft = %q (gate tests %d)", text, len(seen))
	}
	// Gate 实测用的是测试凭据，不是任何用户凭据；客户端出站由订阅渲染得到。
	if cfg, _ := seen[1]["config"].(map[string]any); cfg == nil || seen[1]["client"] == nil {
		t.Fatalf("node test payload = %v", seen[1])
	}
	pending := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)
	if len(pending) != 1 {
		t.Fatalf("pending = %v", pending)
	}
	d := pending[0].(map[string]any)
	if b, _ := json.Marshal(d); strings.Contains(string(b), priv) || d["source"] != "analysis" || d["node_name"] == "" {
		t.Fatalf("draft view = %s", b)
	}
	id := d["id"].(string)
	applied := mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+id+"/apply", nil, 200)
	st := applied["settings"].(map[string]any)
	if st["reality_server_names"] != "www.amazon.com" || st["reality_private_key"] != priv {
		t.Fatalf("applied settings = %v", st)
	}
	n, _ := s.Store.Ent.Node.Get(t.Context(), nid)
	if n.Settings["reality_dest"] != "www.amazon.com:443" {
		t.Fatalf("node not updated: %v", n.Settings)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+id+"/apply", nil, 404)

	// 节点在起草后被改过：草稿作废；放弃的草稿直接删除。
	toolText(t, h, m.webToken, "submit_node_draft", map[string]any{"node_id": nid, "set": map[string]any{"reality_server_names": "www.apple.com", "reality_dest": "www.apple.com:443"}, "explain": "换回 apple"})
	stale := mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts", nil, 200)["drafts"].([]any)[0].(map[string]any)["id"].(string)
	cur, _ := s.Store.Ent.Node.Get(t.Context(), nid)
	changed := map[string]any{}
	for k, v := range cur.Settings {
		changed[k] = v
	}
	changed["ws_path"] = "/changed"
	if err := s.Store.Ent.Node.UpdateOneID(nid).SetSettings(changed).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+stale+"/apply", nil, 409)
	mustJSON(t, admin, "DELETE", "/api/v1/admin/ai/drafts/"+stale, nil, 204)
	mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts/"+stale, nil, 404)
}

func mustString(v any) string { b, _ := json.Marshal(v); return string(b) }
