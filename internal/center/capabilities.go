package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/ai"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/link"
	entnode "github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/user"
)

// 能力目录：dsh 能做什么由后端在这里登记、运行时发现；参数由 dsh 自己拼；前端只有一个通用确认窗，按参数说明展示。
// 后端新增一项能力，dsh 立即可用。只读能力、提交草稿的能力、界面导航分开列出。
// 执行时调用与手动操作同一个处理函数（见 action_drafts.go 的 invoke），不另写一套逻辑。
// 以下永远不进目录：登录与重新验证身份；密钥类设置（SMTP、1Panel、WebDAV、API 令牌）；备份导出与恢复；
// dsh 自身设置；中心地址；管理员账户与角色变更。

// Param 是一个参数的说明：类型之外带给人看的信息（中文名、单位、枚举显示名、引用对象类型）。
type Param struct {
	Key      string      `json:"key"`
	Label    string      `json:"label"`
	Kind     string      `json:"kind"` // string / text（长文本，如驱动原文）/ int / bool / enum / gb / days / ref / refs / object
	Unit     string      `json:"unit,omitempty"`
	Ref      string      `json:"ref,omitempty"` // 引用对象类型：user / node / gate / link / event
	Options  [][2]string `json:"options,omitempty"`
	Required bool        `json:"required,omitempty"`
	Hint     string      `json:"hint,omitempty"`
	Secret   bool        `json:"secret,omitempty"` // 密码、密钥类：不进上下文、不发给模型，只标已填 / 未填
	Min      *float64    `json:"min,omitempty"`
	Max      *float64    `json:"max,omitempty"`
}

// Capability 是目录里的一项能力。
type Capability struct {
	Name    string  `json:"name"`
	Title   string  `json:"title"`
	Purpose string  `json:"purpose"`
	Kind    string  `json:"kind"` // read / draft
	Risk    string  `json:"risk"` // normal / danger
	Role    string  `json:"role"` // admin
	Params  []Param `json:"params"`
	// Object 是这项能力作用的对象类型；Creates 为真表示它新建对象，后面的步骤可以用 $名字 引用。
	Object  string `json:"object,omitempty"`
	Creates bool   `json:"creates,omitempty"`
	// Last 表示做了就不能撤回（如删除）：只能是一份草稿的最后一步。
	Last bool `json:"last,omitempty"`
	// Sources 是可以使用的来源（dsh 窗口里的对话 / analysis 后台分析）；为空表示只有 dsh。
	Sources []string `json:"-"`
	// Probe 表示这项只读能力会让 VPS 实际发起连接（实测、探测、读诊断）：不改数据，每次调用记审计。
	Probe bool `json:"probe,omitempty"`

	// op 是声明式登记的后端操作（见 capability_ops.go）：plan / exec 由通用逻辑生成。
	op   *opSpec
	read func(ctx context.Context, s *Server, args map[string]any) (any, error)
	// plan 校验并试运行（不改任何数据）：填好每个字段的改前值与预演结果，记下所依据的数据版本。
	plan func(ctx context.Context, s *Server, p *planCtx, st *DraftStep) error
	// exec 以确认者身份调用手动操作的处理函数执行；返回撤回函数（多步草稿某步失败时倒序撤回）。
	exec func(ctx context.Context, s *Server, x *execCtx, st *DraftStep) (undo func(context.Context) error, err error)
}

// DraftStep 是草稿里的一步：用哪项能力、参数，以及给人看的字段（中文名、单位、引用名称、改前 / 改后）。
type DraftStep struct {
	Action string         `json:"action"`
	As     string         `json:"as,omitempty"` // 新建对象的占位名，后面的步骤用 "$名字" 引用
	Args   map[string]any `json:"args"`
	Title  string         `json:"title"`
	Risk   string         `json:"risk"`
	Fields []StepField    `json:"fields"`
	// Result 是试运行结果（如节点实测延迟）；Preview 是预演（将新建的对象、额度变化、生成的凭据数量）。
	Result  map[string]any `json:"result,omitempty"`
	Preview []string       `json:"preview,omitempty"`
	Edited  []string       `json:"edited,omitempty"` // 管理员在确认窗里改过的字段
	// Tested 是已实测通过的参数摘要：确认时参数没变就不再实测。
	Tested string `json:"tested,omitempty"`
	// Diff 是长文本参数（如驱动原文）与当前版本的逐行差异，确认窗按差异显示。
	Diff []diffLine `json:"diff,omitempty"`
	// Checked 是试运行的预演（声明式能力）：确认时参数没变就沿用，不再试运行。
	Checked []string `json:"checked,omitempty"`
}

// StepField 是确认窗里的一行：参数说明 + 现值 / 草稿值的显示文本；Value 是可编辑的原值。
type StepField struct {
	Param
	Value   any    `json:"value"`
	Display string `json:"display"`
	Before  string `json:"before,omitempty"`
	Changed bool   `json:"changed,omitempty"`
}

// planCtx 在一次试运行里记下占位引用与所依据的数据版本。
type planCtx struct {
	source   string
	refs     map[string]string // 占位名 → 对象类型
	names    map[string]string // 占位名 → 显示名
	versions map[string]string // "user:3" → 版本摘要
	// confirm 为真表示管理员正在确认窗里点确认：机密参数此时由管理员填好，必填的不能再缺。
	confirm bool
}

func newPlanCtx(source string) *planCtx {
	return &planCtx{source: source, refs: map[string]string{}, names: map[string]string{}, versions: map[string]string{}}
}

// execCtx 是执行时的上下文：确认者的请求（身份、来源地址）与占位引用解析出的真实编号。
type execCtx struct {
	actor   *gin.Context
	created map[string]int
	outputs []map[string]any // 每步处理函数的返回（如新用户的初始密码，只给确认者看）
}

// version 是对象与草稿相关字段的摘要；确认时相关数据已被改动就拒绝，不在旧数据上强行套用。
func version(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// —— 参数校验与显示（所有能力共用） ——

// checkArgs 按参数说明校验：未知字段、必填、类型、范围、枚举、引用（已有对象的编号或前面步骤的 $名字）。
func (s *Server) checkArgs(ctx context.Context, p *planCtx, params []Param, args map[string]any) error {
	normalizeRefs(params, args)
	known := map[string]Param{}
	for _, pa := range params {
		known[pa.Key] = pa
	}
	for k := range args {
		if _, ok := known[k]; !ok {
			return fmt.Errorf("不认识的参数 %s", k)
		}
	}
	for _, pa := range params {
		v, ok := args[pa.Key]
		// 机密参数（订阅地址、代理密码、分享链接）不经过 dsh：草稿里留空，由管理员在确认窗里填。
		if pa.Secret && !p.confirm && ok && v != nil && v != "" {
			return fmt.Errorf("参数 %s（%s）是机密，不要填写：留空提交，由管理员在 Weir 的确认窗里直接填", pa.Key, pa.Label)
		}
		if !ok || v == nil || pa.Secret && v == "" {
			if pa.Required && (!pa.Secret || p.confirm) {
				if pa.Secret {
					return fmt.Errorf("请在确认窗里填写「%s」", pa.Label)
				}
				return fmt.Errorf("缺少参数 %s（%s）", pa.Key, pa.Label)
			}
			continue
		}
		if err := s.checkValue(ctx, p, pa, v); err != nil {
			return fmt.Errorf("参数 %s（%s）%v", pa.Key, pa.Label, err)
		}
	}
	return nil
}

func (s *Server) checkValue(ctx context.Context, p *planCtx, pa Param, v any) error {
	switch pa.Kind {
	case "string":
		str, ok := v.(string)
		if !ok || len(str) > 2000 {
			return errors.New("必须是不超过 2000 字的文本")
		}
	case "text":
		str, ok := v.(string)
		if !ok || len(str) > 256<<10 {
			return errors.New("必须是不超过 256 KB 的文本")
		}
	case "bool":
		if _, ok := v.(bool); !ok {
			return errors.New("必须是 true 或 false")
		}
	case "int", "gb", "days":
		n, ok := v.(float64)
		if !ok || pa.Kind == "int" && n != float64(int64(n)) {
			return errors.New("必须是数字")
		}
		if pa.Min != nil && n < *pa.Min || pa.Max != nil && n > *pa.Max {
			return fmt.Errorf("超出范围")
		}
	case "enum":
		str, _ := v.(string)
		for _, o := range pa.Options {
			if o[0] == str {
				return nil
			}
		}
		return errors.New("不是可选值之一")
	case "ref":
		_, err := s.refName(ctx, p, pa.Ref, v)
		return err
	case "refs":
		list, ok := v.([]any)
		if !ok || len(list) > 200 {
			return errors.New("必须是编号列表")
		}
		for _, item := range list {
			if _, err := s.refName(ctx, p, pa.Ref, item); err != nil {
				return err
			}
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return errors.New("必须是对象")
		}
	}
	return nil
}

// refName 解析引用并返回显示名：数字是已有对象的编号（同时记下数据版本），"$名字" 是前面步骤新建的对象。
func (s *Server) refName(ctx context.Context, p *planCtx, kind string, v any) (string, error) {
	if str, ok := v.(string); ok && strings.HasPrefix(str, "$") {
		t, ok := p.refs[str[1:]]
		if !ok {
			return "", fmt.Errorf("引用了前面没有的 %s", str)
		}
		if t != kind {
			return "", fmt.Errorf("%s 是%s，这里要%s", str, objectNames[t], objectNames[kind])
		}
		return p.names[str[1:]] + "（新建）", nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) || f <= 0 {
		if s, isStr := v.(string); isStr && strings.TrimSpace(s) != "" {
			return "", fmt.Errorf("要填%s编号（数字，如 1），不能填名称 %q%s；草稿里引用前面新建的对象才用 \"$名字\"", objectNames[kind], s, listHint(kind))
		}
		return "", fmt.Errorf("要填%s编号（正整数，如 1）%s", objectNames[kind], listHint(kind))
	}
	id := int(f)
	key := fmt.Sprintf("%s:%d", kind, id)
	switch kind {
	case "user":
		u, err := s.Store.Ent.User.Get(ctx, id)
		if err != nil {
			return "", fmt.Errorf("用户 %d 不存在", id)
		}
		p.versions[key] = version([]any{u.Name, u.Role, u.Enabled, u.Quota, u.ExpiresAt, u.MonthlyReset, u.AllNodes, u.NodeIds, u.AllowChain})
		return firstNonEmpty(u.Name, u.Email), nil
	case "node":
		n, err := s.Store.Ent.Node.Get(ctx, id)
		if err != nil {
			return "", fmt.Errorf("节点 %d 不存在", id)
		}
		p.versions[key] = version([]any{n.Name, n.Enabled, n.Port, n.Settings, n.CertID})
		return n.Name, nil
	case "gate":
		g, err := s.Store.Ent.Gate.Get(ctx, id)
		if err != nil {
			return "", fmt.Errorf("VPS %d 不存在", id)
		}
		return g.Name, nil
	case "link":
		l, err := s.Store.Ent.Link.Get(ctx, id)
		if err != nil {
			return "", fmt.Errorf("链接 %d 不存在", id)
		}
		p.versions[key] = version([]any{l.Name, l.Enabled, l.AllNodes, l.NodeIds, l.ChainIds})
		return firstNonEmpty(l.Name, "订阅"), nil
	case "event":
		e, err := s.loadEvent(ctx, id)
		if err != nil {
			return "", fmt.Errorf("安全事件 %d 不存在", id)
		}
		// 只看是否已处理：起草、分析中等状态变化不算数据改动。
		p.versions[key] = version(e.Status == "blocked" || e.Status == "dismissed")
		return fmt.Sprintf("安全事件 #%d（%s）", e.ID, e.Kind), nil
	case "outbound":
		o, err := s.Store.Ent.Outbound.Get(ctx, id)
		if err != nil {
			return "", fmt.Errorf("出口 %d 不存在", id)
		}
		p.versions[key] = version([]any{o.Tag, o.Name, o.Protocol, o.Address, o.Port, o.Username, o.Password, o.Enabled, o.Settings})
		return o.Tag, nil
	case "route":
		r, err := s.Store.Ent.Route.Get(ctx, id)
		if err != nil {
			return "", fmt.Errorf("分流规则 %d 不存在", id)
		}
		p.versions[key] = version([]any{r.OutboundTag, r.Domain, r.IP, r.NodeID, r.UserID, r.Enabled, r.Priority})
		return fmt.Sprintf("规则 #%d → %s", r.ID, r.OutboundTag), nil
	case "source":
		v, err := s.Store.Ent.SubSource.Get(ctx, id)
		if err != nil {
			return "", fmt.Errorf("订阅源 %d 不存在", id)
		}
		p.versions[key] = version([]any{v.Name, v.URL, v.Enabled, v.UserAgent, v.FilterInclude, v.FilterExclude, v.PlusAsSpace, v.NodeCache, v.ExcludeTraffic, v.Website, v.Remark})
		return firstNonEmpty(v.Name, fmt.Sprintf("订阅源 #%d", v.ID)), nil
	case "profile":
		v, err := s.Store.Ent.SubProfile.Get(ctx, id)
		if err != nil {
			return "", fmt.Errorf("订阅组 %d 不存在", id)
		}
		p.versions[key] = version([]any{v.Name, v.Enabled, v.AllSources, v.SourceIds, v.NodeIds, v.Settings, v.ExpiresAt})
		return v.Name, nil
	case "template":
		v, err := s.Store.Ent.RuleTemplate.Get(ctx, id)
		if err != nil {
			return "", fmt.Errorf("规则模板 %d 不存在", id)
		}
		p.versions[key] = version([]any{v.Name, v.Format, v.Content})
		return v.Name, nil
	case "manual":
		v, err := s.Store.Ent.SubNode.Get(ctx, id)
		if err != nil {
			return "", fmt.Errorf("手动节点 %d 不存在", id)
		}
		p.versions[key] = version([]any{v.Name, v.URL, v.Group, v.Enabled})
		return firstNonEmpty(v.Name, fmt.Sprintf("手动节点 #%d", v.ID)), nil
	case "cert":
		v, err := s.Store.Ent.Certificate.Get(ctx, id)
		if err != nil {
			return "", fmt.Errorf("证书 %d 不存在", id)
		}
		return firstNonEmpty(v.Name, v.Domain), nil
	}
	return "", fmt.Errorf("不支持的引用类型 %s", kind)
}

var objectNames = map[string]string{"user": "用户", "node": "节点", "gate": "VPS", "link": "链接", "event": "安全事件",
	"outbound": "出口", "route": "分流规则", "source": "订阅源", "profile": "订阅组", "template": "规则模板", "manual": "手动节点", "cert": "证书"}

// listTools 是查各类对象编号的只读工具，写进参数说明与报错里。
var listTools = map[string]string{"user": "list_users", "node": "list_nodes", "gate": "list_gates", "link": "get_user", "event": "list_security_events",
	"outbound": "list_outbounds", "route": "list_routes", "source": "list_sources", "profile": "list_profiles", "template": "list_templates",
	"manual": "list_manual_nodes", "cert": "list_certs"}

func listHint(kind string) string {
	if t := listTools[kind]; t != "" {
		return "，见 " + t
	}
	return ""
}

var digits = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)

// normalizeRefs 把引用参数里写成字符串的编号（"1"）换成数字：不同客户端对没写类型的参数编码不一样，
// 草稿执行时处理函数要的也是数字。名称、"$名字" 原样保留，交给 refName 判断。
func normalizeRefs(params []Param, args map[string]any) {
	num := func(v any) any {
		if s, ok := v.(string); ok && digits.MatchString(strings.TrimSpace(s)) {
			n, _ := strconv.Atoi(strings.TrimSpace(s))
			return float64(n)
		}
		return v
	}
	for _, pa := range params {
		switch v, ok := args[pa.Key]; {
		case !ok:
		case pa.Kind == "ref":
			args[pa.Key] = num(v)
		case pa.Kind == "refs":
			if list, ok := v.([]any); ok {
				for i := range list {
					list[i] = num(list[i])
				}
			}
		}
	}
}

// display 把参数值写成给人看的文本：单位、枚举显示名、引用名称；密码类只标已填 / 未填。
func (s *Server) display(ctx context.Context, p *planCtx, pa Param, v any) string {
	if v == nil {
		return "（不填）"
	}
	if pa.Secret {
		if str, _ := v.(string); str != "" {
			return "已填"
		}
		return "未填"
	}
	switch pa.Kind {
	case "text":
		str, _ := v.(string)
		return fmt.Sprintf("%d 行（内容见下方差异）", strings.Count(strings.TrimRight(str, "\n"), "\n")+1)
	case "bool":
		if v == true {
			return "是"
		}
		return "否"
	case "gb":
		if n, _ := v.(float64); n > 0 {
			return fmt.Sprintf("%g GB", n)
		}
		return "不限"
	case "days":
		if n, _ := v.(float64); n > 0 {
			return fmt.Sprintf("%g 天后到期", n)
		}
		return "不到期"
	case "enum":
		for _, o := range pa.Options {
			if o[0] == v {
				return o[1]
			}
		}
	case "ref":
		name, _ := s.refName(ctx, p, pa.Ref, v)
		return name
	case "refs":
		list, _ := v.([]any)
		names := make([]string, 0, len(list))
		for _, item := range list {
			name, _ := s.refName(ctx, p, pa.Ref, item)
			names = append(names, name)
		}
		if len(names) == 0 {
			return "无"
		}
		return strings.Join(names, "、")
	case "object":
		b, _ := json.Marshal(v)
		return string(b)
	}
	if pa.Unit != "" {
		return fmt.Sprint(v) + " " + pa.Unit
	}
	return fmt.Sprint(v)
}

// fields 生成确认窗的字段：所有给了值的参数，按目录顺序。
func (s *Server) fields(ctx context.Context, p *planCtx, params []Param, args map[string]any) []StepField {
	out := []StepField{}
	for _, pa := range params {
		v, ok := args[pa.Key]
		if !ok && !pa.Secret {
			continue
		}
		// 机密参数总是列出来，由管理员在确认窗里填；值不回传给页面，只标已填 / 未填。
		f := StepField{Param: pa, Value: v, Display: s.display(ctx, p, pa, v)}
		if pa.Secret {
			f.Value = nil
			if !ok || v == nil || v == "" {
				f.Display = "未填"
			}
		}
		out = append(out, f)
	}
	return out
}

// —— 只读能力：返回脱敏状态（不含节点密码、UUID、订阅令牌、SOCKS5 密码、私钥；可以含用户名、邮箱） ——

func num(v float64) *float64 { return &v }

var readCaps = []Capability{
	{Name: "list_gates", Title: "VPS 列表", Purpose: "列出全部 VPS：名称、公网地址、在线、配置是否生效、运行中的内核版本、1Panel 状态。", Kind: "read",
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			gs, err := s.Store.Ent.Gate.Query().Order(ent.Asc(gate.FieldID)).All(ctx)
			out := []map[string]any{}
			for _, g := range gs {
				v := gateView(g)
				out = append(out, map[string]any{"id": g.ID, "name": g.Name, "public_host": g.PublicHost, "public_host_hint": v["public_host_hint"], "colocated": v["colocated"], "online": v["online"], "state": v["state"], "last_error": g.LastError, "kernels_running": g.KernelsRunning, "panel": g.Panel})
			}
			return out, err
		}},
	{Name: "list_nodes", Title: "节点列表", Purpose: "列出全部节点：编号、名称、所在 VPS、内核、协议、端口、是否启用。", Kind: "read",
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			nodes, err := s.Store.Ent.Node.Query().Order(ent.Asc(entnode.FieldID)).Limit(500).All(ctx)
			names := map[int]string{}
			if gs, e := s.Store.Ent.Gate.Query().All(ctx); e == nil {
				for _, g := range gs {
					names[g.ID] = g.Name
				}
			}
			out := []map[string]any{}
			for _, n := range nodes {
				out = append(out, map[string]any{"id": n.ID, "name": n.Name, "vps": names[n.GateID], "gate_id": n.GateID, "kernel": n.Kernel, "protocol": n.Protocol, "network": n.Network, "security": n.Security, "port": n.Port, "enabled": n.Enabled})
			}
			return out, err
		}},
	{Name: "get_node_state", Title: "节点参数", Purpose: "读取一个节点的当前参数（机密值替换为 <redacted>）、内核版本、VPS 最近的错误，以及 node.update 可以修改的字段。", Kind: "read",
		Params: []Param{{Key: "node_id", Label: "节点", Kind: "ref", Ref: "node", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			n, err := s.Store.Ent.Node.Get(ctx, intArg(args, "node_id"))
			if err != nil {
				return nil, errors.New("节点不存在")
			}
			state := map[string]any{"node_id": n.ID, "name": n.Name, "kernel": n.Kernel, "protocol": n.Protocol, "network": n.Network, "security": n.Security, "port": n.Port, "enabled": n.Enabled,
				"settings": ai.Redact(n.Settings), "editable_fields": editableFields}
			if g, err := s.Store.Ent.Gate.Get(ctx, n.GateID); err == nil {
				state["vps"], state["vps_last_error"] = g.Name, g.LastError
				if ks, _, err := s.gateKernels(ctx, g, map[string]bool{n.Kernel: true}); err == nil {
					state["kernel_version"] = ks[n.Kernel].Version
				}
			}
			return state, nil
		}},
	{Name: "list_users", Title: "用户列表", Purpose: "列出用户：编号、邮箱、名称、角色、是否启用、额度与已用（字节）、到期、是否全部节点、链接数。", Kind: "read",
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			us, err := s.Store.Ent.User.Query().Order(ent.Asc(user.FieldID)).Limit(1000).All(ctx)
			out := []map[string]any{}
			for _, u := range us {
				links, _ := s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).Count(ctx)
				out = append(out, map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role, "enabled": u.Enabled, "quota_bytes": u.Quota, "used_bytes": u.Used,
					"expires_at": u.ExpiresAt, "monthly_reset": u.MonthlyReset, "all_nodes": u.AllNodes, "node_ids": u.NodeIds, "allow_chain": u.AllowChain, "links": links})
			}
			return out, err
		}},
	{Name: "get_user", Title: "用户详情", Purpose: "读取一个用户与他的链接（链接只给名称、编号、包含的节点，不含订阅地址与令牌）。", Kind: "read",
		Params: []Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			u, err := s.Store.Ent.User.Get(ctx, intArg(args, "user_id"))
			if err != nil {
				return nil, errors.New("用户不存在")
			}
			links, _ := s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).Order(ent.Asc(link.FieldID)).All(ctx)
			ls := []map[string]any{}
			for _, l := range links {
				ls = append(ls, map[string]any{"id": l.ID, "name": l.Name, "enabled": l.Enabled, "all_nodes": l.AllNodes, "node_ids": l.NodeIds, "chain_ids": l.ChainIds, "last_used_at": l.LastUsedAt})
			}
			return map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role, "enabled": u.Enabled, "quota_bytes": u.Quota, "used_bytes": u.Used, "expires_at": u.ExpiresAt,
				"monthly_reset": u.MonthlyReset, "all_nodes": u.AllNodes, "node_ids": u.NodeIds, "allow_chain": u.AllowChain, "links": ls}, nil
		}},
	{Name: "list_security_events", Title: "安全事件", Purpose: "列出最近的安全事件（来源地址已替换为代号）与 Laya 的判断。", Kind: "read", Sources: []string{"dsh", "analysis"},
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			events, err := s.recentEvents(ctx, 20)
			out := []map[string]any{}
			for _, e := range events {
				out = append(out, e.modelView())
			}
			return out, err
		}},
	{Name: "get_security_event", Title: "安全事件详情", Purpose: "读取一个安全事件的详情（来源地址已替换为代号）。", Kind: "read", Sources: []string{"dsh", "analysis"},
		Params: []Param{{Key: "event_id", Label: "安全事件", Kind: "ref", Ref: "event", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			e, err := s.loadEvent(ctx, intArg(args, "event_id"))
			if err != nil {
				return nil, errors.New("安全事件不存在")
			}
			return e.modelView(), nil
		}},
}

// —— 提交草稿的能力 ——

var planParams = []Param{
	{Key: "name", Label: "名称", Kind: "string"},
	{Key: "quota_gb", Label: "流量额度", Kind: "gb", Unit: "GB", Min: num(0), Hint: "0 为不限"},
	{Key: "expires_days", Label: "有效期", Kind: "days", Unit: "天", Min: num(0), Max: num(3650), Hint: "从确认时起算；0 为不到期"},
	{Key: "monthly_reset", Label: "每月重置用量", Kind: "bool"},
	{Key: "all_nodes", Label: "全部节点", Kind: "bool"},
	{Key: "node_ids", Label: "可用节点", Kind: "refs", Ref: "node"},
	{Key: "allow_chain", Label: "允许自建链式代理", Kind: "bool"},
}

var draftCaps = []Capability{
	{Name: "user.create", Title: "新建用户", Purpose: "新建一个普通用户（初始密码由系统生成，只在确认后显示给管理员），默认生成一条订阅链接。", Kind: "draft", Risk: "normal", Object: "user", Creates: true,
		Params: append([]Param{{Key: "email", Label: "邮箱", Kind: "string", Required: true}}, planParams...), plan: planUserCreate, exec: execUserCreate},
	{Name: "user.update", Title: "修改用户", Purpose: "修改用户的名称、额度、有效期、可用节点、是否启用；不能修改角色与密码。", Kind: "draft", Risk: "normal", Object: "user",
		Params: append(append([]Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true}}, planParams...), Param{Key: "enabled", Label: "启用", Kind: "bool"}), plan: planUserUpdate, exec: execUserUpdate},
	{Name: "link.create", Title: "新建订阅链接", Purpose: "为用户新建一条订阅链接（生成新的订阅令牌）；可以只包含部分节点。", Kind: "draft", Risk: "normal", Object: "link", Creates: true,
		Params: []Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true}, {Key: "name", Label: "名称", Kind: "string"},
			{Key: "all_nodes", Label: "包含全部节点", Kind: "bool"}, {Key: "node_ids", Label: "包含的节点", Kind: "refs", Ref: "node"}}, plan: planLinkCreate, exec: execLinkCreate},
	{Name: "node.update", Title: "修改节点", Purpose: "修改节点参数（只能用 get_node_state 的 editable_fields，值为 null 表示删除）、名称或启用状态。Weir 在节点所在 VPS 上用真实内核实测，通过才可确认。", Kind: "draft", Risk: "normal", Object: "node",
		Params: []Param{{Key: "node_id", Label: "节点", Kind: "ref", Ref: "node", Required: true}, {Key: "name", Label: "名称", Kind: "string"}, {Key: "enabled", Label: "启用", Kind: "bool"},
			{Key: "set", Label: "修改的参数", Kind: "object", Hint: "字段 → 新值"}}, plan: planNodeUpdate, exec: execNodeUpdate},
	{Name: "defense.block", Title: "封禁来源", Purpose: "暂时封禁一个安全事件的来源；到期自动解封。不需要处置时不要提交。", Kind: "draft", Risk: "normal", Object: "event", Sources: []string{"dsh", "analysis"},
		Params: []Param{{Key: "event_id", Label: "安全事件", Kind: "ref", Ref: "event", Required: true}, {Key: "minutes", Label: "封禁时长", Kind: "int", Unit: "分钟", Required: true, Min: num(5), Max: num(maxBlockMinutes)}},
		plan:   planDefenseBlock, exec: execDefenseBlock},
	{Name: "user.delete", Title: "删除用户", Purpose: "删除一个普通用户及其链接、自建链式代理；不能撤回，只能是草稿的最后一步。", Kind: "draft", Risk: "danger", Object: "user", Last: true,
		Params: []Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true}}, plan: planUserDelete, exec: execUserDelete},
}

// catalog 返回某个来源可用的能力（后台分析只能读安全事件、提交封禁草稿）。
func catalog(source string) []Capability {
	var out []Capability
	for _, list := range [][]Capability{readCaps, opReadCaps, driverReadCaps, draftCaps, opDraftCaps, driverDraftCaps} {
		for _, c := range list {
			if c.Role == "" {
				c.Role = "admin"
			}
			if len(c.Sources) == 0 && source == "dsh" || containsStr(c.Sources, source) {
				out = append(out, c)
			}
		}
	}
	return out
}

func findCap(source, name string) (Capability, bool) {
	for _, c := range catalog(source) {
		if c.Name == name {
			return c, true
		}
	}
	return Capability{}, false
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// intArg 取整数参数；写成字符串的编号（"3"）也认，不同客户端对编号的编码不一样。
func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case string:
		if digits.MatchString(strings.TrimSpace(v)) {
			n, _ := strconv.Atoi(strings.TrimSpace(v))
			return n
		}
	}
	return 0
}

// marker: capabilities-append
