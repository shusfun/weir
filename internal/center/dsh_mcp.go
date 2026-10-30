package center

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Weir 给 dsh 的 MCP 工具（Streamable HTTP，JSON 响应）：只监听本机回环的随机端口，不经过反代；
// 令牌每次中心启动重新生成，只能读取脱敏后的状态、提交草稿，不能确认或执行任何改动。
// dsh 窗口与后台分析各用一个令牌，草稿据此标明来源。

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
// MCP SDK 会因此拒绝整个工具列表（dsh 里一个 Weir 工具都看不到）。
func obj(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

// paramSchema 把参数说明转成 JSON Schema；说明里带中文名、单位、可选值的显示名与引用对象类型。
func paramSchema(pa Param) map[string]any {
	desc := pa.Label
	if pa.Unit != "" {
		desc += "（" + pa.Unit + "）"
	}
	if pa.Hint != "" {
		desc += "。" + pa.Hint
	}
	if pa.Secret {
		desc += "。机密：不要填，留空提交，由管理员在确认窗里填"
	}
	m := map[string]any{}
	switch pa.Kind {
	case "bool":
		m["type"] = "boolean"
	case "int", "gb", "days":
		m["type"] = "number"
		if pa.Min != nil {
			m["minimum"] = *pa.Min
		}
		if pa.Max != nil {
			m["maximum"] = *pa.Max
		}
	case "enum":
		m["type"] = "string"
		vals := []string{}
		for _, o := range pa.Options {
			vals = append(vals, o[0])
			desc += fmt.Sprintf("；%s=%s", o[0], o[1])
		}
		m["enum"] = vals
	// 引用写明整数类型：只写描述时模型会把编号当字符串传（"1"），也会照描述去填 "$名字"。
	// 这里只用于只读工具；草稿的参数在 submit_draft 的 args 里，"$名字" 的说明在 submit_draft 与 list_actions 里。
	case "ref":
		m["type"], m["minimum"] = "integer", 1
		desc += "：" + objectNames[pa.Ref] + "编号（数字）" + listHint(pa.Ref)
	case "refs":
		m["type"], m["items"] = "array", map[string]any{"type": "integer", "minimum": 1}
		desc += "：" + objectNames[pa.Ref] + "编号列表（数字）" + listHint(pa.Ref)
	case "object":
		m["type"] = "object"
	default:
		m["type"] = "string"
	}
	m["description"] = desc
	return m
}

func paramsSchema(params []Param) map[string]any {
	props, req := map[string]any{}, []string{}
	for _, pa := range params {
		props[pa.Key] = paramSchema(pa)
		if pa.Required {
			req = append(req, pa.Key)
		}
	}
	return obj(props, req...)
}

// mcpToolsFor 由能力目录生成工具列表：只读能力各一个工具；提交草稿统一用 submit_draft（list_actions 列出可提交的能力）。
// 界面导航与控制台上下文只给 dsh 窗口里的对话。
func mcpToolsFor(source string) []mcpTool {
	tools := []mcpTool{}
	if source == "dsh" {
		tools = append(tools, consoleTools...)
	}
	var actions []string
	for _, c := range catalog(source) {
		if c.Kind == "read" {
			tools = append(tools, mcpTool{c.Name, c.Purpose, paramsSchema(c.Params)})
		} else {
			actions = append(actions, c.Name+"（"+c.Title+"）")
		}
	}
	tools = append(tools,
		mcpTool{"list_actions", "列出可以提交草稿的能力：名称、用途、风险、参数说明（中文名、单位、可选值、引用类型）。拼草稿前先读。", obj(map[string]any{})},
		mcpTool{"submit_draft", "提交草稿。Weir 用手动操作的同一套校验试运行：不通过会返回原因（可以修改后重新提交）；通过后弹出 Weir 的确认窗，管理员确认才生效，你无法确认。" +
			fmt.Sprintf("一份草稿可以有 1 到 %d 步，把多项能力拼成一件事（如建节点 → 建用户分配这个节点 → 建链接）：新建对象的步骤用 as 起名，后面的步骤用 \"$名字\" 引用；任一步失败，能撤回的全部撤回。", maxDraftSteps) +
			"标了机密（secret）的参数不要填，留空提交，由管理员在确认窗里直接填。可提交的能力：" + strings.Join(actions, "、"),
			obj(map[string]any{
				"explain": map[string]any{"type": "string", "description": "一句中文说明：为什么这样做。"},
				"steps": map[string]any{"type": "array", "description": fmt.Sprintf("1 到 %d 步", maxDraftSteps), "items": obj(map[string]any{
					"action": map[string]any{"type": "string", "description": "能力名称，见 list_actions"},
					"as":     map[string]any{"type": "string", "description": "可选：新建对象的占位名（小写字母开头）"},
					"args":   map[string]any{"type": "object", "description": "参数，见 list_actions"},
				}, "action", "args")},
			}, "explain", "steps")},
	)
	return tools
}

// mcpVersions 是支持的经典 MCP 版本（新到旧）。
var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const mcpInstructions = "这些工具连接 Weir 中心。节点参数里的机密值已替换为 <redacted>，不要尝试修改它们。所有草稿都要经过系统实测与管理员确认，你无法直接改动任何配置。"

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
				"serverInfo": map[string]any{"name": "weir", "version": "1"}, "instructions": mcpInstructions}
		case "server/discover":
			// MCP 2026-07-28 起服务端必须实现；只声明经典版本，客户端据此走 initialize 握手。
			result = map[string]any{"supportedVersions": mcpVersions, "capabilities": map[string]any{"tools": map[string]any{}}, "instructions": mcpInstructions}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": mcpToolsFor(source)}
		case "tools/call":
			// 每次工具调用一个排查编号：失败时写进中心日志（泳道「Weir MCP」）并附在返回给 dsh 的文字里，
			// 这样 dsh 会话里的那次调用能和中心日志、试运行里处理函数的 5xx 对上。
			trace := requestID()
			start := time.Now()
			ctx, cancel := context.WithTimeout(withTrace(r.Context(), trace), 3*time.Minute)
			text, isErr := s.callTool(ctx, source, req.Params.Name, req.Params.Arguments)
			cancel()
			if isErr {
				slog.Warn("mcp tool failed", "trace", trace, "source", source, "tool", req.Params.Name, "args", argShape(req.Params.Arguments),
					"ms", time.Since(start).Milliseconds(), "err", redact(text))
				text += "\n（排查编号 " + trace + "）"
			}
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

// argShape 是写进日志的参数概况：只记每个参数的类型（编号传成字符串这类问题一眼能看出来），
// 草稿只记各步的能力名；不记参数值，避免把邮箱、名称或模型误填的机密写进日志。
func argShape(raw json.RawMessage) string {
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil {
		return "(不是 JSON 对象)"
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		switch v := args[k].(type) {
		case []any:
			if k == "steps" {
				var actions []string
				for _, st := range v {
					m, _ := st.(map[string]any)
					a, _ := m["action"].(string)
					actions = append(actions, a)
				}
				parts = append(parts, "steps:["+strings.Join(actions, ",")+"]")
				continue
			}
			parts = append(parts, k+":array")
		case float64:
			parts = append(parts, k+":number")
		case string:
			parts = append(parts, k+":string")
		case bool:
			parts = append(parts, k+":bool")
		case nil:
			parts = append(parts, k+":null")
		default:
			parts = append(parts, k+":object")
		}
	}
	return strings.Join(parts, " ")
}

type traceKey struct{}

// withTrace 把排查编号放进上下文：试运行与只读工具在进程内调用处理函数时，处理函数的 5xx 用同一个编号记日志。
func withTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}

func traceOf(ctx context.Context) string {
	id, _ := ctx.Value(traceKey{}).(string)
	return id
}

// callTool 执行一个工具，返回给模型的文本与是否出错。
func (s *Server) callTool(ctx context.Context, source, name string, raw json.RawMessage) (string, bool) {
	s.state.RLock()
	ready := s.Store != nil
	s.state.RUnlock()
	if !ready {
		return "Weir 尚未初始化", true
	}
	args := map[string]any{}
	if len(raw) > 0 && json.Unmarshal(raw, &args) != nil {
		return "参数不是有效的 JSON 对象", true
	}
	if c, ok := findCap(source, name); ok && c.Kind == "read" {
		if keys := pendingRefs(c.Params, args); len(keys) > 0 {
			return fmt.Sprintf("参数 %s 要填编号（数字，如 1）；\"$名字\" 只能在 submit_draft 里引用前面步骤新建的对象", strings.Join(keys, "、")), true
		}
	}
	switch name {
	case "get_console_context", "navigate_console":
		if source != "dsh" {
			return "后台分析不能使用这个工具", true
		}
		if name == "get_console_context" {
			return jsonText(s.consoleContextForModel(ctx)), false
		}
		if err := s.queueNavigation(ctx, args); err != nil {
			return "不能跳转：" + err.Error(), true
		}
		return "已让控制台打开该页面（只换页面，不改任何数据）。", false
	case "list_actions":
		out := []Capability{}
		for _, c := range catalog(source) {
			if c.Kind == "draft" {
				out = append(out, c)
			}
		}
		return jsonText(out), false
	case "submit_draft":
		var req struct {
			Explain string `json:"explain"`
			Steps   []struct {
				Action string         `json:"action"`
				As     string         `json:"as"`
				Args   map[string]any `json:"args"`
			} `json:"steps"`
		}
		if json.Unmarshal(raw, &req) != nil {
			return "参数格式不对：需要 explain 与 steps", true
		}
		// 只取模型能给的三个字段；标题、字段、预演、实测结果一律由 Weir 重新生成。
		steps := make([]DraftStep, 0, len(req.Steps))
		for _, st := range req.Steps {
			steps = append(steps, DraftStep{Action: st.Action, As: st.As, Args: st.Args})
		}
		d, err := s.submitActionDraft(ctx, source, req.Explain, steps)
		if err != nil {
			return "草稿没有通过，没有改动任何数据：" + err.Error() + "\n请根据原因修改后重新提交。", true
		}
		titles := make([]string, 0, len(d.Steps))
		for _, st := range d.Steps {
			titles = append(titles, st.Title)
		}
		return fmt.Sprintf("试运行通过，已弹窗请管理员确认（草稿 %s）：%s。在管理员确认前不会生效。", d.ID, strings.Join(titles, "；")), false
	}
	c, ok := findCap(source, name)
	if !ok || c.Kind != "read" {
		return "未知工具 " + name, true
	}
	if err := s.checkArgs(ctx, newPlanCtx(source), c.Params, args); err != nil {
		return err.Error(), true
	}
	v, err := c.read(ctx, s, args)
	if c.Probe {
		result := "ok"
		if err != nil {
			result = "failed"
		}
		target, _ := json.Marshal(args)
		_, _ = s.Store.Ent.Audit.Create().SetAction("dsh.probe").SetTarget(name + " " + string(target)).SetResult(result).Save(ctx)
	}
	if err != nil {
		return err.Error(), true
	}
	return jsonText(v), false
}
