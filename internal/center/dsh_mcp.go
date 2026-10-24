package center

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/imzyb/MiGate/internal/ai"
	"github.com/imzyb/MiGate/internal/store/ent"
	entnode "github.com/imzyb/MiGate/internal/store/ent/node"
)

// MiGate 给 dsh 的 MCP 工具（Streamable HTTP，JSON 响应）：只监听本机回环的随机端口，不经过反代；
// 令牌每次中心启动重新生成，只能读取脱敏后的状态、提交草稿，不能确认或执行任何改动。
// Web 抽屉与后台分析各用一个令牌，草稿据此标明来源。

type mcpServer struct {
	addr         string
	webToken     string
	analystToken string
}

func (s *Server) ensureMCP() (*mcpServer, error) {
	s.dsh.mu.Lock()
	defer s.dsh.mu.Unlock()
	if s.dsh.mcp != nil {
		return s.dsh.mcp, nil
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	m := &mcpServer{addr: l.Addr().String(), webToken: randomToken(32), analystToken: randomToken(32)}
	srv := &http.Server{Handler: s.mcpHandler(m), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 4 * time.Minute}
	go func() { _ = srv.Serve(l) }()
	s.dsh.mcp = m
	return m, nil
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// obj 生成工具参数的 JSON Schema。required 必须是数组：Go 的 nil 切片会编码成 null，
// MCP SDK 会因此拒绝整个工具列表（dsh 里一个 MiGate 工具都看不到）。
func obj(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

var mcpTools = []mcpTool{
	{"get_console_context", "管理员在 MiGate 控制台里正在看哪个节点或页面（从那里打开了 dsh）。对话开始先调用。", obj(map[string]any{})},
	{"list_nodes", "列出 MiGate 的全部节点（id、名称、所在 VPS、内核与协议）。", obj(map[string]any{})},
	{"get_node_state", "读取一个节点的当前参数（密码、UUID、密钥已替换为 <redacted>）、内核版本、VPS 最近的错误，以及草稿允许修改的字段。", obj(map[string]any{"node_id": map[string]any{"type": "integer"}}, "node_id")},
	{"submit_node_draft", "提交节点参数草稿。MiGate 会在节点所在 VPS 上用真实内核实测；通过后弹窗请管理员确认，未通过会返回原因，可以修改后重新提交。草稿不会自动生效。", obj(map[string]any{
		"node_id": map[string]any{"type": "integer"},
		"set":     map[string]any{"type": "object", "description": "要修改的字段与新值；值为 null 表示删除该字段。只能使用 get_node_state 返回的 editable_fields。"},
		"explain": map[string]any{"type": "string", "description": "一句中文说明：为什么这样改。"},
	}, "node_id", "set", "explain")},
	{"list_security_events", "列出最近的安全事件（来源地址已替换为代号）与 Laya 的判断。", obj(map[string]any{})},
	{"get_security_event", "读取一个安全事件的详情。", obj(map[string]any{"event_id": map[string]any{"type": "integer"}}, "event_id")},
	{"submit_defense_draft", "针对一个安全事件提交防御草稿：暂时封禁该事件的来源。草稿会弹窗请管理员确认，不会自动执行。不需要处置时不要调用。", obj(map[string]any{
		"event_id": map[string]any{"type": "integer"},
		"action":   map[string]any{"type": "string", "enum": []string{"block_source"}},
		"minutes":  map[string]any{"type": "integer", "minimum": 5, "maximum": maxBlockMinutes},
		"explain":  map[string]any{"type": "string", "description": "一句中文说明：判断依据与建议。"},
	}, "event_id", "action", "minutes", "explain")},
}

// mcpVersions 是支持的经典 MCP 版本（新到旧）。
var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const mcpInstructions = "这些工具连接 MiGate 中心。节点参数里的机密值已替换为 <redacted>，不要尝试修改它们。所有草稿都要经过系统实测与管理员确认，你无法直接改动任何配置。"

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		ProtocolVersion string          `json:"protocolVersion"`
		Name            string          `json:"name"`
		Arguments       json.RawMessage `json:"arguments"`
	} `json:"params"`
}

func (s *Server) mcpHandler(m *mcpServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		source := ""
		switch {
		case subtle.ConstantTimeCompare([]byte(auth), []byte(m.webToken)) == 1:
			source = "dsh"
		case subtle.ConstantTimeCompare([]byte(auth), []byte(m.analystToken)) == 1:
			source = "analysis"
		default:
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req rpcRequest
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if len(req.ID) == 0 { // 通知
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		var rpcErr map[string]any
		switch req.Method {
		case "initialize":
			// 客户端要的版本在支持范围内就照用，否则回最新的经典版本（由客户端决定是否接受）。
			v := mcpVersions[0]
			for _, s := range mcpVersions {
				if s == req.Params.ProtocolVersion {
					v = s
				}
			}
			result = map[string]any{"protocolVersion": v, "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "migate", "version": "1"}, "instructions": mcpInstructions}
		case "server/discover":
			// MCP 2026-07-28 起服务端必须实现；只声明经典版本，客户端据此走 initialize 握手。
			result = map[string]any{"supportedVersions": mcpVersions, "capabilities": map[string]any{"tools": map[string]any{}}, "instructions": mcpInstructions}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": mcpTools}
		case "tools/call":
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
			text, isErr := s.callTool(ctx, source, req.Params.Name, req.Params.Arguments)
			cancel()
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": isErr}
		default:
			rpcErr = map[string]any{"code": -32601, "message": "method not found"}
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func jsonText(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

// callTool 执行一个工具，返回给模型的文本与是否出错。
func (s *Server) callTool(ctx context.Context, source, name string, raw json.RawMessage) (string, bool) {
	s.state.RLock()
	ready := s.Store != nil
	s.state.RUnlock()
	if !ready {
		return "MiGate 尚未初始化", true
	}
	var args struct {
		NodeID  int            `json:"node_id"`
		EventID int            `json:"event_id"`
		Set     map[string]any `json:"set"`
		Explain string         `json:"explain"`
		Action  string         `json:"action"`
		Minutes int            `json:"minutes"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &args) != nil {
		return "参数不是有效的 JSON 对象", true
	}
	switch name {
	case "get_console_context":
		var hint struct {
			NodeID int       `json:"node_id"`
			Path   string    `json:"path"`
			At     time.Time `json:"at"`
		}
		raw, ok, err := s.Store.KV.Get(ctx, "dsh-context")
		if err != nil || !ok || json.Unmarshal([]byte(raw), &hint) != nil {
			return jsonText(map[string]any{"page": "未知", "note": "管理员是从控制台右下角直接打开的 dsh，没有附带页面信息。"}), false
		}
		out := map[string]any{"page": hint.Path, "opened_at": hint.At}
		if hint.NodeID > 0 {
			if n, err := s.Store.Ent.Node.Get(ctx, hint.NodeID); err == nil {
				out["node"] = map[string]any{"id": n.ID, "name": n.Name, "kernel": n.Kernel, "protocol": n.Protocol}
				out["note"] = fmt.Sprintf("管理员是在节点「%s」（id %d）上点「AI 调整」打开的 dsh。", n.Name, n.ID)
			}
		}
		return jsonText(out), false
	case "list_nodes":
		nodes, err := s.Store.Ent.Node.Query().Order(ent.Asc(entnode.FieldID)).Limit(500).All(ctx)
		if err != nil {
			return "读取节点失败", true
		}
		gates := map[int]string{}
		if gs, err := s.Store.Ent.Gate.Query().All(ctx); err == nil {
			for _, g := range gs {
				gates[g.ID] = g.Name
			}
		}
		list := make([]map[string]any, 0, len(nodes))
		for _, n := range nodes {
			list = append(list, map[string]any{"id": n.ID, "name": n.Name, "vps": gates[n.GateID], "kernel": n.Kernel, "protocol": n.Protocol, "network": n.Network, "security": n.Security, "enabled": n.Enabled})
		}
		return jsonText(list), false
	case "get_node_state":
		n, err := s.Store.Ent.Node.Get(ctx, args.NodeID)
		if err != nil {
			return "节点不存在", true
		}
		state := map[string]any{"node_id": n.ID, "name": n.Name, "kernel": n.Kernel, "protocol": n.Protocol, "network": n.Network, "security": n.Security, "enabled": n.Enabled,
			"settings": ai.Redact(n.Settings), "editable_fields": editableFields}
		if g, err := s.Store.Ent.Gate.Get(ctx, n.GateID); err == nil {
			state["vps"], state["vps_last_error"] = g.Name, g.LastError
			if ks, _, err := s.gateKernels(ctx, g, map[string]bool{n.Kernel: true}); err == nil {
				state["kernel_version"] = ks[n.Kernel].Version
			}
		}
		return jsonText(state), false
	case "submit_node_draft":
		d, err := s.submitNodeDraft(ctx, source, args.NodeID, args.Set, args.Explain)
		if err != nil {
			return "草稿被拒绝：" + err.Error(), true
		}
		if d.Status != "passed" {
			text := "草稿未通过实测，节点保持原样：" + d.Error
			if d.Result != nil {
				text += "\n实测详情：" + jsonText(d.Result)
			}
			return text + "\n请根据原因修改后重新提交。", true
		}
		return fmt.Sprintf("实测通过，已弹窗请管理员确认（草稿 %s）。在管理员确认前不会生效。", d.ID), false
	case "list_security_events":
		events, err := s.recentEvents(ctx, 20)
		if err != nil {
			return "读取安全事件失败", true
		}
		list := make([]map[string]any, 0, len(events))
		for _, e := range events {
			list = append(list, e.modelView())
		}
		return jsonText(list), false
	case "get_security_event":
		e, err := s.loadEvent(ctx, args.EventID)
		if err != nil {
			return "安全事件不存在", true
		}
		return jsonText(e.modelView()), false
	case "submit_defense_draft":
		d, err := s.submitDefenseDraft(ctx, source, args.EventID, args.Action, args.Minutes, args.Explain)
		if err != nil {
			return "草稿被拒绝：" + err.Error(), true
		}
		return fmt.Sprintf("已弹窗请管理员确认（草稿 %s）。在管理员确认前不会执行。", d.ID), false
	}
	return "未知工具 " + name, true
}
