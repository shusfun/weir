package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/ai"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/user"
)

// invoke 以确认者的身份调用手动操作的处理函数：同一份校验、同一份写入、同一条审计。
// 请求只在进程内构造，不经过路由，外部无法伪造。
func invoke(x *execCtx, handler gin.HandlerFunc, method string, params gin.Params, body any) (int, map[string]any) {
	return call(x.actor, handler, method, params, nil, body, false)
}

// call 在进程内调用处理函数。dry 为真时是试运行：处理函数做完全部校验与实测，在第一次写入之前
// 用 dryRun 返回预演并停下（没接入试运行的处理函数不能登记成草稿能力，见 capability_ops.go）。
func call(actor *gin.Context, handler gin.HandlerFunc, method string, params gin.Params, query url.Values, body any, dry bool) (int, map[string]any) {
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	c, eng := gin.CreateTestContext(w)
	_ = eng.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	req := httptest.NewRequest(method, "/internal?"+query.Encode(), strings.NewReader(string(b))).WithContext(actor.Request.Context())
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = actor.Request.RemoteAddr
	for _, h := range []string{"X-Forwarded-For", "X-Real-IP"} {
		if v := actor.GetHeader(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	c.Request, c.Params = req, params
	for _, k := range []string{"user_id", "account", "role", "session_key", "session"} {
		if v, ok := actor.Get(k); ok {
			c.Set(k, v)
		}
	}
	if dry {
		c.Set(dryRunKey, true)
	}
	handler(c)
	c.Writer.WriteHeaderNow()
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	// 进程内调用不经过请求日志中间件：处理函数的 5xx 在这里记下真实原因，编号用外层请求或 MCP 调用的排查编号。
	if w.Code >= 500 {
		attrs := []any{"trace", firstNonEmpty(actor.GetString("request_id"), traceOf(actor.Request.Context())), "method", method, "status", w.Code, "dry_run", dry, "code", c.GetString("error_code")}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "err", redact(strings.Join(c.Errors.Errors(), "; ")))
		}
		slog.Error("draft step handler failed", attrs...)
	}
	return w.Code, out
}

// systemActor 是试运行与只读调用用的身份：没有登录会话，来源是本机回环。
// 只读和试运行不写数据，审计里的操作者以确认时点确认的管理员为准。
func systemActor(ctx context.Context) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/internal", nil).WithContext(ctx)
	c.Request.RemoteAddr = "127.0.0.1:0"
	return c
}

const dryRunKey = "weir_dry_run"

// dryResult 是试运行的预演：Pin 是系统这次替草稿定下的值（自动分配的端口、实测选中的伪装目标），
// 写回草稿参数，确认时原样使用、不再重新挑选；Preview 是给人看的预演；Result 是实测结果（如延迟）。
type dryResult struct {
	Pin     map[string]any `json:"pin,omitempty"`
	Preview []string       `json:"preview,omitempty"`
	Result  map[string]any `json:"result,omitempty"`
}

// dryRun 在处理函数第一次写入之前调用：试运行时返回预演并结束请求（返回 true），手动操作时什么也不做。
func dryRun(c *gin.Context, r dryResult) bool {
	if !c.GetBool(dryRunKey) {
		return false
	}
	c.JSON(200, gin.H{"dry_run": r})
	return true
}

// handlerError 取出处理函数返回的错误说明。
func handlerError(code int, out map[string]any) error {
	if e, ok := out["error"].(map[string]any); ok {
		s := func(k string) string { v, _ := e[k].(string); return v }
		return errors.New(firstNonEmpty(s("detail"), s("message"), s("code"), fmt.Sprintf("HTTP %d", code)))
	}
	return fmt.Errorf("HTTP %d", code)
}

// resolve 把参数里的 "$名字" 换成前面步骤新建对象的编号（执行时）。
func (x *execCtx) resolve(v any) any {
	switch t := v.(type) {
	case string:
		if strings.HasPrefix(t, "$") {
			if id, ok := x.created[t[1:]]; ok {
				return id
			}
		}
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = x.resolve(item)
		}
		return out
	case float64:
		return int(t)
	}
	return v
}

// planBody 把套餐参数换成处理函数的请求字段：GB → 字节，天数 → 到期时间。
func (x *execCtx) planBody(args map[string]any) map[string]any {
	body := map[string]any{}
	for _, k := range []string{"name", "monthly_reset", "all_nodes", "allow_chain", "enabled"} {
		if v, ok := args[k]; ok {
			body[k] = v
		}
	}
	if v, ok := args["quota_gb"].(float64); ok {
		body["quota"] = int64(v * (1 << 30))
	}
	if v, ok := args["expires_days"].(float64); ok {
		if v > 0 {
			body["expires_at"] = time.Now().Add(time.Duration(v*24) * time.Hour).UTC()
		} else {
			body["expires_at"] = nil
		}
	}
	if v, ok := args["node_ids"]; ok {
		body["node_ids"] = x.resolve(v)
	}
	return body
}

// —— 用户 ——

func planUserCreate(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
	email := strings.ToLower(strings.TrimSpace(fmt.Sprint(st.Args["email"])))
	if !validEmail(email) {
		return errors.New("邮箱格式不对")
	}
	if n, _ := s.Store.Ent.User.Query().Where(user.EmailEQ(email)).Count(ctx); n > 0 {
		return fmt.Errorf("邮箱 %s 已被使用", email)
	}
	st.Args["email"] = email
	st.Title = "新建用户 " + email
	st.Preview = []string{"新建普通用户 " + email + "（初始密码由系统生成，确认后只显示一次）", "自动生成一条包含其可用节点的订阅链接"}
	return nil
}

func execUserCreate(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
	body := x.planBody(st.Args)
	body["email"] = st.Args["email"]
	code, out := invoke(x, s.createUser, "POST", nil, body)
	if code != 200 {
		return nil, handlerError(code, out)
	}
	id := numberOf(out["id"])
	if st.As != "" {
		x.created[st.As] = id
	}
	x.outputs = append(x.outputs, map[string]any{"step": st.Title, "user_id": id, "email": out["email"], "password": out["password"]})
	return func(context.Context) error {
		code, out := invoke(x, s.deleteUser, "DELETE", gin.Params{{Key: "uid", Value: itoa(id)}}, nil)
		if code >= 300 {
			return handlerError(code, out)
		}
		return nil
	}, nil
}

// userBefore 是用户当前的套餐字段（与参数同名同单位），确认窗据此显示改前 / 改后。
func userBefore(u *entUser) map[string]any {
	days := float64(0)
	if u.ExpiresAt != nil {
		days = float64(int(time.Until(*u.ExpiresAt).Hours()/24 + 0.5))
	}
	ids := make([]any, 0, len(u.NodeIds))
	for _, id := range u.NodeIds {
		ids = append(ids, float64(id))
	}
	return map[string]any{"name": u.Name, "quota_gb": float64(u.Quota) / (1 << 30), "expires_days": days, "monthly_reset": u.MonthlyReset,
		"all_nodes": u.AllNodes, "node_ids": ids, "allow_chain": u.AllowChain, "enabled": u.Enabled}
}

func planUserUpdate(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
	if ref, ok := st.Args["user_id"].(string); ok && strings.HasPrefix(ref, "$") {
		st.Title = "修改新建的用户"
		return nil
	}
	u, err := s.Store.Ent.User.Get(ctx, intArg(st.Args, "user_id"))
	if err != nil {
		return errors.New("用户不存在")
	}
	if u.Role == user.RoleAdmin {
		return errors.New("管理员账户不在能力目录里，不能通过草稿修改")
	}
	before, changed := userBefore(u), 0
	for i := range st.Fields {
		f := &st.Fields[i]
		if b, ok := before[f.Key]; ok {
			f.Before = s.display(ctx, p, f.Param, b)
			f.Changed = f.Before != f.Display
			if f.Changed {
				changed++
			}
		}
	}
	if changed == 0 {
		return errors.New("草稿与用户现在的设置相同")
	}
	st.Title = "修改用户 " + firstNonEmpty(u.Name, u.Email)
	return nil
}

func execUserUpdate(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
	uid := numberOf(x.resolve(st.Args["user_id"]))
	u, err := s.Store.Ent.User.Get(ctx, uid)
	if err != nil {
		return nil, errors.New("用户不存在")
	}
	if u.Role == user.RoleAdmin {
		return nil, errors.New("不能修改管理员账户")
	}
	params := gin.Params{{Key: "uid", Value: itoa(uid)}}
	code, out := invoke(x, s.updateUser, "PATCH", params, x.planBody(st.Args))
	if code != 200 {
		return nil, handlerError(code, out)
	}
	// 撤回：把本步改过的字段恢复成改前的值（到期时间按原值恢复，不按天数换算）。
	prev := map[string]any{"name": u.Name, "quota": u.Quota, "expires_at": u.ExpiresAt, "monthly_reset": u.MonthlyReset,
		"all_nodes": u.AllNodes, "node_ids": u.NodeIds, "allow_chain": u.AllowChain, "enabled": u.Enabled}
	return func(context.Context) error {
		if code, out := invoke(x, s.updateUser, "PATCH", params, prev); code >= 300 {
			return handlerError(code, out)
		}
		return nil
	}, nil
}

func planUserDelete(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
	u, err := s.Store.Ent.User.Get(ctx, intArg(st.Args, "user_id"))
	if err != nil {
		return errors.New("用户不存在")
	}
	if u.Role == user.RoleAdmin {
		return errors.New("管理员账户不在能力目录里，不能通过草稿删除")
	}
	links, _ := s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).Count(ctx)
	st.Title = "删除用户 " + firstNonEmpty(u.Name, u.Email)
	st.Preview = []string{fmt.Sprintf("删除用户 %s、他的 %d 条订阅链接与自建链式代理；已导入客户端的订阅随即失效", u.Email, links), "删除不能撤回"}
	return nil
}

func execUserDelete(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
	uid := numberOf(x.resolve(st.Args["user_id"]))
	if u, err := s.Store.Ent.User.Get(ctx, uid); err != nil || u.Role == user.RoleAdmin {
		return nil, errors.New("不能删除这个账户")
	}
	if code, out := invoke(x, s.deleteUser, "DELETE", gin.Params{{Key: "uid", Value: itoa(uid)}}, nil); code >= 300 {
		return nil, handlerError(code, out)
	}
	return nil, nil
}

// —— 订阅链接 ——

func planLinkCreate(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
	name, _ := s.refName(ctx, p, "user", st.Args["user_id"])
	st.Title = "为 " + name + " 新建订阅链接"
	st.Preview = []string{"生成一个新的订阅令牌；订阅地址确认后在「订阅管理」里查看"}
	// 生效时间在提交草稿时就校验（与手动新建的规则相同），不等到确认时才报错
	start, _, err := optionalTime(rawArg(st.Args["starts_at"]))
	if err != nil {
		return errors.New("开始时间格式无效，要 RFC 3339 时间")
	}
	end, _, err := optionalTime(rawArg(st.Args["ends_at"]))
	if err != nil {
		return errors.New("结束时间格式无效，要 RFC 3339 时间")
	}
	if uid, ok := st.Args["user_id"].(float64); ok {
		if u, err := s.Store.Ent.User.Get(ctx, int(uid)); err == nil {
			switch err := checkLinkWindow(u, start, end); {
			case err == nil:
			case err.Error() == "link_window_order":
				return errors.New("结束时间要晚于开始时间")
			default:
				return errors.New("生效时间不能超过用户到期时间")
			}
		}
	}
	if start != nil || end != nil {
		st.Preview = append(st.Preview, "生效时间："+windowPreview(start, end)+"；不在这段时间里拉不到节点，VPS 也停用这条链接的凭据")
	}
	return nil
}

// rawArg 把草稿参数换成 optionalTime 认的 JSON；没给时为空（不改）。
func rawArg(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func windowPreview(start, end *time.Time) string {
	from, to := "现在起", "不限结束"
	if start != nil {
		from = start.Local().Format("2006-01-02 15:04") + " 起"
	}
	if end != nil {
		to = "到 " + end.Local().Format("2006-01-02 15:04")
	}
	return from + "，" + to
}

func execLinkCreate(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
	uid := numberOf(x.resolve(st.Args["user_id"]))
	body := map[string]any{}
	for _, k := range []string{"name", "all_nodes", "starts_at", "ends_at"} {
		if v, ok := st.Args[k]; ok {
			body[k] = v
		}
	}
	if v, ok := st.Args["node_ids"]; ok {
		body["node_ids"] = x.resolve(v)
	}
	handler := func(c *gin.Context) { s.createLink(c, true) }
	code, out := invoke(x, handler, "POST", gin.Params{{Key: "uid", Value: itoa(uid)}}, body)
	if code != 200 {
		return nil, handlerError(code, out)
	}
	lid := numberOf(out["id"])
	if st.As != "" {
		x.created[st.As] = lid
	}
	return func(context.Context) error {
		del := func(c *gin.Context) { s.deleteLink(c, true) }
		if code, out := invoke(x, del, "DELETE", gin.Params{{Key: "uid", Value: itoa(uid)}, {Key: "lid", Value: itoa(lid)}}, nil); code >= 300 {
			return handlerError(code, out)
		}
		return nil
	}, nil
}

// —— 节点：只能改 editable_fields；在节点所在 VPS 上用真实内核实测，通过才可确认 ——

// fieldLabel 是节点字段的中文名（来自内置方案的高级字段），没有登记的用字段名。
func (s *Server) fieldLabel(ctx context.Context, key string) (string, string) {
	for _, p := range s.presets(ctx) {
		for _, f := range p.Fields {
			if f.Key == key {
				return f.Label, f.Unit
			}
		}
	}
	extra := map[string]string{"reality_dest": "REALITY 目标", "reality_server_names": "REALITY SNI", "tls_sni": "TLS SNI", "ss_method": "加密方法"}
	if l, ok := extra[key]; ok {
		return l, ""
	}
	return key, ""
}

func valueKind(v any) string {
	switch v.(type) {
	case bool:
		return "bool"
	case float64:
		return "int"
	case string, nil:
		return "string"
	}
	return "object"
}

func planNodeUpdate(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
	n, err := s.Store.Ent.Node.Get(ctx, intArg(st.Args, "node_id"))
	if err != nil {
		return errors.New("节点不存在")
	}
	set, _ := st.Args["set"].(map[string]any)
	if err = ai.CheckPatch(ai.Patch{Set: set, Explain: "-"}); err != nil && len(set) > 0 {
		return err
	}
	for k := range set {
		if !editable(k) {
			return fmt.Errorf("字段 %s 不允许修改（只能用 get_node_state 的 editable_fields）", k)
		}
	}
	after := ai.Apply(n.Settings, ai.Patch{Set: set})
	fields := []StepField{}
	for _, f := range st.Fields {
		switch f.Key {
		case "node_id":
			fields = append(fields, f)
		case "name", "enabled":
			old := map[string]any{"name": n.Name, "enabled": n.Enabled}[f.Key]
			f.Before = s.display(ctx, p, f.Param, old)
			f.Changed = f.Before != f.Display
			fields = append(fields, f)
		}
	}
	for _, c := range ai.Diff(n.Settings, after) {
		label, unit := s.fieldLabel(ctx, c.Key)
		pa := Param{Key: "set." + c.Key, Label: label, Kind: valueKind(c.After), Unit: unit}
		fields = append(fields, StepField{Param: pa, Value: c.After, Display: s.display(ctx, p, pa, c.After), Before: s.display(ctx, p, pa, c.Before), Changed: true})
	}
	st.Fields = fields
	changed := len(set) > 0
	for _, f := range fields {
		changed = changed || f.Changed
	}
	if !changed {
		return errors.New("草稿与节点现在的设置相同")
	}
	st.Title = "修改节点 " + n.Name
	if len(set) > 0 {
		if h := settingsHash(after); st.Tested != h {
			g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
			if err != nil {
				return errors.New("节点所在 VPS 不存在")
			}
			if err = s.checkSettings(ctx, g, n.Kernel, n.Protocol, n.Network, n.Security, n.Port, after); err != nil {
				return fmt.Errorf("参数无法生成配置：%v", err)
			}
			res, err := s.testNode(ctx, n, after)
			st.Result = res
			if err != nil {
				return fmt.Errorf("未通过实测，节点保持原样：%v", err)
			}
			st.Tested = h
		}
		st.Preview = append(st.Preview, "已在节点所在 VPS 上用真实内核实测通过")
	}
	return nil
}

func execNodeUpdate(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
	nid := numberOf(x.resolve(st.Args["node_id"]))
	n, err := s.Store.Ent.Node.Get(ctx, nid)
	if err != nil {
		return nil, errors.New("节点不存在")
	}
	set, _ := st.Args["set"].(map[string]any)
	body := map[string]any{"skip_probe": true} // 试运行时已经实测通过
	if len(set) > 0 {
		body["settings"] = set
	}
	for _, k := range []string{"name", "enabled"} {
		if v, ok := st.Args[k]; ok {
			body[k] = v
		}
	}
	params := gin.Params{{Key: "id", Value: itoa(nid)}}
	if code, out := invoke(x, s.updateNode, "PATCH", params, body); code != 200 {
		return nil, handlerError(code, out)
	}
	// 撤回：改过的参数恢复原值，原来没有的字段删除（null）。
	prevSet := map[string]any{}
	for k := range set {
		prevSet[k] = n.Settings[k]
	}
	prev := map[string]any{"name": n.Name, "enabled": n.Enabled, "settings": prevSet, "skip_probe": true}
	return func(context.Context) error {
		if code, out := invoke(x, s.updateNode, "PATCH", params, prev); code >= 300 {
			return handlerError(code, out)
		}
		return nil
	}, nil
}

// —— 防御：封禁一个安全事件的来源 ——

func planDefenseBlock(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error {
	e, err := s.loadEvent(ctx, intArg(st.Args, "event_id"))
	if err != nil {
		return errors.New("安全事件不存在")
	}
	switch {
	case e.Status == "blocked" || e.Status == "dismissed":
		return errors.New("该事件已处理")
	case !blockable(e.SourceIP):
		return errors.New("该来源是本机或无效地址，不能封禁")
	}
	st.Title = fmt.Sprintf("封禁安全事件 #%d 的来源", e.ID)
	// 来源地址只出现在管理员的确认窗里（草稿内容不返回给 dsh）。
	st.Preview = []string{fmt.Sprintf("封禁来源 %s %s，到期自动解封；可在「系统设置 → 安全防御」提前解封", e.SourceIP, duration(intArg(st.Args, "minutes")))}
	if e.Laya != nil {
		st.Preview = append(st.Preview, fmt.Sprintf("Laya 判断：%s（置信度 %d%%）", e.Laya.Category, int(e.Laya.Confidence*100)))
	} else {
		st.Preview = append(st.Preview, "Laya 判断：未知")
	}
	return nil
}

func duration(m int) string {
	switch {
	case m >= 1440 && m%1440 == 0:
		return fmt.Sprintf("%d 天", m/1440)
	case m >= 60 && m%60 == 0:
		return fmt.Sprintf("%d 小时", m/60)
	}
	return fmt.Sprintf("%d 分钟", m)
}

func execDefenseBlock(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (func(context.Context) error, error) {
	e, err := s.loadEvent(ctx, intArg(st.Args, "event_id"))
	if err != nil {
		return nil, errors.New("安全事件不存在")
	}
	key, err := s.blockSource(x.actor, e, intArg(st.Args, "minutes"), st.Title)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		_ = s.Store.KV.Del(ctx, key)
		s.markEvent(ctx, e.ID, "new", "")
		return nil
	}, nil
}
