package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/ai"
)

// 声明式登记的后端操作：每项只写对应的处理函数、参数说明和怎么取改前的值；
// 试运行、执行、撤回由这里的通用逻辑完成，所以 dsh 能把任意几项拼进一份草稿。
// - 试运行：以试运行模式调用同一个处理函数，它做完全部校验与实测、在第一次写入前用 dryRun 停下；
//   系统替草稿定下的值（自动分配的端口、实测选中的伪装目标）写回参数，确认时原样使用。
// - 执行：以点确认的管理员身份调用同一个处理函数（同一份写入与审计）。
// - 撤回：新建的调用对应的删除；修改的把改过的字段写回改前的值；删除只能是最后一步。
// 处理函数没接入 dryRun 时试运行直接报错，不会退回成真的执行。

type opSpec struct {
	kind    string // create / update / delete / action
	method  string
	handler func(s *Server) gin.HandlerFunc
	// path 由参数算出路由参数（参数里的引用此时都已是真实编号）；pathKeys 是只用于路由的参数，不进请求体。
	path     func(ctx context.Context, s *Server, args map[string]any) (gin.Params, error)
	pathKeys []string
	query    func(args map[string]any) url.Values
	// body 把参数换成处理函数的请求字段；不给时参数去掉 pathKeys 后原样作请求体。
	body func(args map[string]any) map[string]any
	// self 是指向被修改 / 删除对象的参数；load 读出它现在的值（与参数同名同单位），用于显示改前值、撤回。
	self string
	load func(ctx context.Context, s *Server, args map[string]any) (map[string]any, error)
	// full 表示处理函数整体覆盖对象：没给的字段用改前的值补齐，否则会被清空。
	full bool
	// undo 是撤回新建对象用的删除能力；idKey 是它的对象参数名。
	undo, idKey string
	// probe 表示处理函数支持 skip_probe：试运行已实测通过时，执行不再重复实测。
	probe bool
	// needs 返回这次必须由管理员在确认窗里填的机密参数（按协议等条件才需要的）。
	needs func(args map[string]any) []string
	// noUndo 表示这项修改没有可靠的撤回办法（如重新分配会换新的订阅地址）。
	noUndo bool
	title  func(ctx context.Context, s *Server, p *planCtx, args map[string]any) string
}

// keys 生成从参数取路由参数的 path：keys("gate_id", "id") 表示参数 gate_id 填进路由的 :id。
func keys(pairs ...string) func(context.Context, *Server, map[string]any) (gin.Params, error) {
	return func(_ context.Context, _ *Server, args map[string]any) (gin.Params, error) {
		var out gin.Params
		for i := 0; i+1 < len(pairs); i += 2 {
			out = append(out, gin.Param{Key: pairs[i+1], Value: itoa(numberOf(args[pairs[i]]))})
		}
		return out, nil
	}
}

// reqBody 是给处理函数的请求体。
func (o *opSpec) reqBody(args map[string]any) map[string]any {
	if o.body != nil {
		return o.body(args)
	}
	out := map[string]any{}
	for k, v := range args {
		if !containsStr(o.pathKeys, k) {
			out[k] = v
		}
	}
	return out
}

// pendingRefs 是引用了前面步骤新建对象（"$名字"）的参数：这些对象确认时才存在。
func pendingRefs(params []Param, args map[string]any) []string {
	var out []string
	isRef := func(v any) bool { s, ok := v.(string); return ok && strings.HasPrefix(s, "$") }
	for _, pa := range params {
		switch v := args[pa.Key]; pa.Kind {
		case "ref":
			if isRef(v) {
				out = append(out, pa.Key)
			}
		case "refs":
			list, _ := v.([]any)
			for _, item := range list {
				if isRef(item) {
					out = append(out, pa.Key)
					break
				}
			}
		}
	}
	return out
}

// resolveRefs 把引用参数里的 "$名字" 换成执行时新建对象的编号；其他参数原样保留。
func resolveRefs(x *execCtx, params []Param, args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	sub := func(v any) any {
		if s, ok := v.(string); ok && strings.HasPrefix(s, "$") {
			if id, ok := x.created[s[1:]]; ok {
				return float64(id)
			}
		}
		return v
	}
	for _, pa := range params {
		switch v := out[pa.Key]; pa.Kind {
		case "ref":
			out[pa.Key] = sub(v)
		case "refs":
			if list, ok := v.([]any); ok {
				ids := make([]any, len(list))
				for i, item := range list {
					ids[i] = sub(item)
				}
				out[pa.Key] = ids
			}
		}
	}
	return out
}

func paramByKey(params []Param, key string) (Param, bool) {
	for _, pa := range params {
		if pa.Key == key {
			return pa, true
		}
	}
	return Param{}, false
}

// missingSecrets 是这次需要、还没填的机密参数（中文名）。
func missingSecrets(c Capability, args map[string]any) []string {
	var keys []string
	for _, pa := range c.Params {
		if pa.Secret && pa.Required {
			keys = append(keys, pa.Key)
		}
	}
	if c.op.needs != nil {
		keys = append(keys, c.op.needs(args)...)
	}
	var out []string
	for _, k := range keys {
		if v, _ := args[k].(string); strings.TrimSpace(v) == "" {
			if pa, ok := paramByKey(c.Params, k); ok && !containsStr(out, pa.Label) {
				out = append(out, pa.Label)
			}
		}
	}
	return out
}

func (s *Server) opTitle(ctx context.Context, p *planCtx, c Capability, args map[string]any) string {
	if c.op.title != nil {
		return c.op.title(ctx, s, p, args)
	}
	if c.op.self != "" {
		if pa, ok := paramByKey(c.Params, c.op.self); ok {
			if name, err := s.refName(ctx, p, pa.Ref, args[c.op.self]); err == nil {
				return c.Title + " " + name
			}
		}
	}
	for _, k := range []string{"name", "tag", "version"} {
		if v, _ := args[k].(string); v != "" {
			return c.Title + " " + v
		}
	}
	return c.Title
}

func merged(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// planOp 是声明式能力的试运行：显示改前 / 改后，调用处理函数的试运行，记下系统定下的值。
func planOp(c Capability) func(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
	return func(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
		o := c.op
		st.Title = s.opTitle(ctx, p, c, st.Args)
		pending := pendingRefs(c.Params, st.Args)
		var before map[string]any
		if o.load != nil && !containsStr(pending, o.self) {
			var err error
			if before, err = o.load(ctx, s, st.Args); err != nil {
				return err
			}
		}
		if o.kind == "update" && before != nil {
			changed := false
			for i := range st.Fields {
				f := &st.Fields[i]
				if f.Key == o.self || containsStr(o.pathKeys, f.Key) {
					continue
				}
				v, given := st.Args[f.Key]
				switch {
				case f.Secret:
					f.Before = s.display(ctx, p, f.Param, before[f.Key])
					if given && v != "" {
						f.Changed = true
					} else {
						f.Display = "不改"
					}
				case f.Kind == "text":
					old, _ := before[f.Key].(string)
					f.Changed = v != old
				default:
					if b, ok := before[f.Key]; ok {
						f.Before = s.display(ctx, p, f.Param, b)
						f.Changed = f.Before != f.Display
					} else {
						f.Changed = true
					}
				}
				changed = changed || f.Changed
			}
			if !changed {
				return errors.New("草稿与现在的设置相同")
			}
		}
		for _, pa := range c.Params {
			if after, ok := st.Args[pa.Key].(string); ok && pa.Kind == "text" {
				old, _ := before[pa.Key].(string)
				st.Diff = diffLines(old, after)
			}
		}
		eff := st.Args
		if o.full && before != nil {
			eff = merged(before, st.Args)
		}
		missing := missingSecrets(c, eff)
		if len(pending) > 0 || len(missing) > 0 {
			st.Tested, st.Checked, st.Result = "", nil, nil
			if len(missing) > 0 {
				if p.confirm {
					return fmt.Errorf("请在确认窗里填写「%s」", strings.Join(missing, "」「"))
				}
				st.Preview = append(st.Preview, "「"+strings.Join(missing, "」「")+"」不经过 dsh，请在这里直接填写；确认时再试运行")
			}
			if len(pending) > 0 {
				st.Preview = append(st.Preview, "引用了前面步骤新建的对象：确认时按顺序试运行并执行，任一步不通过整份草稿撤回")
			}
			return nil
		}
		request := func(args map[string]any) (gin.Params, url.Values, map[string]any, string, error) {
			params, err := o.path(ctx, s, args)
			if err != nil {
				return nil, nil, nil, "", err
			}
			var q url.Values
			if o.query != nil {
				q = o.query(args)
			}
			body := o.reqBody(args)
			return params, q, body, settingsHash(map[string]any{"path": params, "query": q, "body": body}), nil
		}
		params, q, body, h, err := request(eff)
		if err != nil {
			return err
		}
		if h != st.Tested {
			code, out := call(systemActor(ctx), o.handler(s), o.method, params, q, body, true)
			if code >= 300 {
				return handlerError(code, out)
			}
			raw, ok := out["dry_run"]
			if !ok {
				return fmt.Errorf("%s 还没有接入试运行，不能作为草稿提交", c.Name)
			}
			var r dryResult
			b, _ := json.Marshal(raw)
			_ = json.Unmarshal(b, &r)
			pinned := false
			for k, v := range r.Pin {
				if _, given := st.Args[k]; !given {
					if _, known := paramByKey(c.Params, k); known {
						st.Args[k], pinned = v, true
					}
				}
			}
			if pinned {
				st.Fields = s.fields(ctx, p, c.Params, st.Args)
				if eff = st.Args; o.full && before != nil {
					eff = merged(before, st.Args)
				}
				if _, _, _, h, err = request(eff); err != nil {
					return err
				}
			}
			st.Result, st.Checked, st.Tested = r.Result, r.Preview, h
		}
		st.Preview = append(st.Preview, st.Checked...)
		return nil
	}
}

// execOp 以点确认的管理员身份调用处理函数，返回撤回函数。
func execOp(c Capability) func(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
	return func(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
		o := c.op
		args := resolveRefs(x, c.Params, st.Args)
		var before map[string]any
		if o.load != nil {
			var err error
			if before, err = o.load(ctx, s, args); err != nil {
				return nil, err
			}
		}
		eff := args
		if o.full && before != nil {
			eff = merged(before, args)
		}
		params, err := o.path(ctx, s, eff)
		if err != nil {
			return nil, err
		}
		body := o.reqBody(eff)
		if o.probe && st.Tested != "" {
			body["skip_probe"] = true // 试运行时已经实测通过
		}
		var q url.Values
		if o.query != nil {
			q = o.query(eff)
		}
		if code, out := call(x.actor, o.handler(s), o.method, params, q, body, false); code >= 300 {
			return nil, handlerError(code, out)
		} else if o.kind == "create" {
			id := numberOf(out["id"])
			if st.As != "" {
				x.created[st.As] = id
			}
			del, ok := findOpCap(o.undo)
			if !ok || id == 0 {
				return nil, nil
			}
			return func(ctx context.Context) error {
				p, err := del.op.path(ctx, s, map[string]any{o.idKey: float64(id)})
				if err != nil {
					return err
				}
				if code, out := call(x.actor, del.op.handler(s), del.op.method, p, nil, nil, false); code >= 300 {
					return handlerError(code, out)
				}
				return nil
			}, nil
		}
		if o.kind != "update" || o.noUndo || before == nil {
			return nil, nil
		}
		// 撤回：改过的字段写回改前的值（整体覆盖的处理函数写回全部字段）。
		prev := map[string]any{}
		for k, v := range before {
			if _, given := args[k]; o.full || given {
				prev[k] = v
			}
		}
		ub := o.reqBody(prev)
		if o.probe {
			ub["skip_probe"] = true
		}
		return func(ctx context.Context) error {
			if code, out := call(x.actor, o.handler(s), o.method, params, nil, ub, false); code >= 300 {
				return handlerError(code, out)
			}
			return nil
		}, nil
	}
}

func findOpCap(name string) (Capability, bool) {
	for _, c := range opDraftCaps {
		if c.Name == name {
			return c, true
		}
	}
	return Capability{}, false
}

func init() {
	for i := range opDraftCaps {
		opDraftCaps[i].plan, opDraftCaps[i].exec = planOp(opDraftCaps[i]), execOp(opDraftCaps[i])
	}
}

// —— 只读能力的通用部分 ——

// readVia 以只读方式调用手动操作的处理函数（GET 或实测类），结果去掉机密后交给模型。
func (s *Server) readVia(ctx context.Context, handler gin.HandlerFunc, method string, params gin.Params, q url.Values, body any) (any, error) {
	code, out := call(systemActor(ctx), handler, method, params, q, body, false)
	if code >= 300 {
		err := handlerError(code, out)
		if e, ok := out["error"].(map[string]any); ok && e["result"] != nil {
			b, _ := json.Marshal(modelSafe(e["result"]))
			return nil, fmt.Errorf("%v；结果：%s", err, b)
		}
		return nil, err
	}
	return modelSafe(out), nil
}

// modelSafe 去掉发给模型的数据里的机密：按字段名识别的密码、密钥、令牌、UUID 换成 <redacted>，
// 地址类字段（订阅地址、节点链接）只留协议与主机。
func modelSafe(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			lk := strings.ToLower(k)
			switch {
			case (ai.IsSecret(k) || strings.HasSuffix(lk, "_key") || lk == "key_pem") && !isBool(item):
				if item != nil && item != "" {
					out[k] = "<redacted>"
				} else {
					out[k] = item
				}
			case lk == "url" || lk == "link" || lk == "uri" || strings.HasSuffix(lk, "_url") || strings.HasSuffix(lk, "_link"):
				if str, ok := item.(string); ok {
					out[k] = maskURL(str)
				} else {
					out[k] = modelSafe(item)
				}
			default:
				out[k] = modelSafe(item)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = modelSafe(item)
		}
		return out
	}
	return v
}

func isBool(v any) bool { _, ok := v.(bool); return ok }

// maskURL 只留协议与主机（订阅地址的路径和查询里常带令牌，节点链接的用户名就是密码）。
func maskURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Hostname() == "" || !strings.Contains(u.Hostname(), ".") && !strings.Contains(u.Hostname(), ":") {
		return "<redacted>"
	}
	return u.Scheme + "://" + u.Hostname() + "/<redacted>"
}

// withoutSecrets 是写进审计的参数：机密参数只记已填 / 未填。
func withoutSecrets(params []Param, args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if pa, ok := paramByKey(params, k); ok && pa.Secret {
			if v != nil && v != "" {
				v = "已填"
			}
		}
		out[k] = v
	}
	return out
}

// toModel 把结构体（视图）转成 map 再去掉机密。
func toModel(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return modelSafe(out)
}
