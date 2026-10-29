package center

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/misub"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/certificate"
	entnode "github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/outbound"
	"github.com/shusfun/weir/internal/store/ent/route"
	"github.com/shusfun/weir/internal/store/ent/ruletemplate"
	"github.com/shusfun/weir/internal/store/ent/subgrant"
	"github.com/shusfun/weir/internal/store/ent/subnode"
	"github.com/shusfun/weir/internal/store/ent/subprofile"
	"github.com/shusfun/weir/internal/store/ent/subsource"
	"github.com/shusfun/weir/packages/contracts"
)

// 声明式登记的能力：草稿能力对应手动操作的处理函数（通用逻辑见 capability_ops.go），
// 只读能力返回去掉机密的状态。参数的中文名、单位与提示尽量与控制台表单一致。

func noPath(context.Context, *Server, map[string]any) (gin.Params, error) { return nil, nil }

// pick 从参数里取给了的字段作请求体。
func pick(args map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := args[k]; ok {
			out[k] = v
		}
	}
	return out
}

func idList[T ~int](ids []T) []any {
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, float64(id))
	}
	return out
}

func refOrNil(id *int) any {
	if id == nil {
		return nil
	}
	return float64(*id)
}

func pathOf(pairs ...string) gin.Params {
	var out gin.Params
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, gin.Param{Key: pairs[i], Value: pairs[i+1]})
	}
	return out
}

// —— 节点 ——

var nodeCreateParams = []Param{
	{Key: "gate_id", Label: "VPS", Kind: "ref", Ref: "gate", Required: true},
	{Key: "preset", Label: "协议方案", Kind: "string", Required: true, Hint: "方案编号，见 list_presets（如 vless-reality、hysteria2、ss2022）"},
	{Key: "name", Label: "节点名称", Kind: "string", Hint: "不填时用「VPS 名 + 方案名」"},
	{Key: "port", Label: "端口", Kind: "int", Min: num(1024), Max: num(65535), Hint: "不填时自动分配（REALITY 优先 443）；先用 check_port 确认没被占用"},
	{Key: "reality_target", Label: "伪装目标", Kind: "string", Hint: "只用于 REALITY 方案，如 www.microsoft.com；不填时由 VPS 实测内置候选站点，取最快且连得通的"},
	{Key: "cert_id", Label: "证书", Kind: "ref", Ref: "cert", Hint: "只用于需要证书的方案；不填时由中心为这台 VPS 准备"},
	{Key: "settings", Label: "高级参数", Kind: "object", Hint: "只能改方案 fields 里列出的字段"},
	{Key: "chain_kind", Label: "出口", Kind: "enum", Options: [][2]string{{"none", "直接出去"}, {"node", "经另一台 VPS 上的节点"}, {"source", "经订阅源里的节点"}},
		Hint: "SOCKS5 / HTTP 代理或分享链接出口带密码，请改用 outbound.create 建出口再加分流规则"},
	{Key: "chain_node_id", Label: "出口节点", Kind: "ref", Ref: "node"},
	{Key: "chain_source_id", Label: "出口订阅源", Kind: "ref", Ref: "source"},
	{Key: "chain_mode", Label: "选择方式", Kind: "enum", Options: [][2]string{{"node", "指定一个节点"}, {"filter", "按名称筛选一组，由内核自动选择"}}},
	{Key: "chain_match", Label: "节点名称或筛选", Kind: "string", Hint: "指定节点时填完整名称；筛选时填正则，如 ^HK"},
}

func nodeCreateBody(args map[string]any) map[string]any {
	body := pick(args, "gate_id", "preset", "name", "port", "reality_target", "cert_id", "settings")
	switch kind, _ := args["chain_kind"].(string); kind {
	case "node":
		body["chain"] = map[string]any{"kind": "node", "node_id": args["chain_node_id"]}
	case "source":
		body["chain"] = map[string]any{"kind": "source", "source_id": args["chain_source_id"], "mode": args["chain_mode"], "match": args["chain_match"]}
	}
	return body
}

func nodeCreateTitle(ctx context.Context, s *Server, p *planCtx, args map[string]any) string {
	gate, _ := s.refName(ctx, p, "gate", args["gate_id"])
	preset, _ := args["preset"].(string)
	if pr, ok := s.presetByID(ctx, preset); ok {
		preset = pr.Name
	}
	if name, _ := args["name"].(string); name != "" {
		return fmt.Sprintf("在 %s 上新建节点 %s（%s）", gate, name, preset)
	}
	return fmt.Sprintf("在 %s 上新建 %s 节点", gate, preset)
}

// —— VPS ——

func loadGate(ctx context.Context, s *Server, args map[string]any) (map[string]any, error) {
	g, err := s.Store.Ent.Gate.Get(ctx, numberOf(args["gate_id"]))
	if err != nil {
		return nil, errors.New("VPS 不存在")
	}
	pins := map[string]any{}
	for k, v := range g.KernelPins {
		pins[k] = v
	}
	return map[string]any{"name": g.Name, "public_host": g.PublicHost, "kernel_pins": pins}, nil
}

// —— 出口与分流规则 ——

func outboundParams(update bool) []Param {
	first := Param{Key: "gate_id", Label: "VPS", Kind: "ref", Ref: "gate", Required: true}
	if update {
		first = Param{Key: "outbound_id", Label: "出口", Kind: "ref", Ref: "outbound", Required: true}
	}
	ps := []Param{first,
		{Key: "tag", Label: "标签", Kind: "string", Required: !update, Hint: "小写字母、数字、- 与 _，分流规则用它引用这个出口"},
		{Key: "name", Label: "名称", Kind: "string"},
		{Key: "protocol", Label: "类型", Kind: "enum", Required: !update, Options: [][2]string{{"socks", "SOCKS5 代理"}, {"http", "HTTP 代理"}, {"https", "HTTPS 代理"},
			{"link", "分享链接"}, {"source", "订阅源里的节点"}, {"freedom", "直连"}, {"blackhole", "丢弃"}}},
		{Key: "address", Label: "地址", Kind: "string", Hint: "代理类填写"},
		{Key: "port", Label: "端口", Kind: "int", Min: num(1), Max: num(65535)},
		{Key: "username", Label: "用户名", Kind: "string"},
		{Key: "password", Label: "密码", Kind: "string", Secret: true, Hint: "代理密码由管理员在确认窗里填，草稿里留空"},
		{Key: "link", Label: "分享链接", Kind: "string", Secret: true, Hint: "一个节点的链接，由管理员在确认窗里粘贴，草稿里留空"},
		{Key: "source_id", Label: "订阅源", Kind: "ref", Ref: "source"},
		{Key: "mode", Label: "选择方式", Kind: "enum", Options: [][2]string{{"node", "指定一个节点"}, {"filter", "按名称筛选一组，由内核自动选择"}}},
		{Key: "match", Label: "节点名称或筛选", Kind: "string", Hint: "指定节点时填完整名称；筛选时填正则，如 ^HK"},
	}
	if update {
		ps = append(ps, Param{Key: "enabled", Label: "启用", Kind: "bool"})
	}
	return ps
}

func outboundNeeds(args map[string]any) []string {
	if args["protocol"] == "link" {
		return []string{"link"}
	}
	return nil
}

func outboundPath(ctx context.Context, s *Server, args map[string]any) (gin.Params, error) {
	o, err := s.Store.Ent.Outbound.Get(ctx, numberOf(args["outbound_id"]))
	if err != nil {
		return nil, errors.New("出口不存在")
	}
	return pathOf("id", itoa(o.GateID), "oid", itoa(o.ID)), nil
}

// loadOutbound 读出口的现值；密码与分享链接只在中心内部用于整体覆盖，给人看时只标已填 / 未填。
func loadOutbound(ctx context.Context, s *Server, args map[string]any) (map[string]any, error) {
	o, err := s.Store.Ent.Outbound.Get(ctx, numberOf(args["outbound_id"]))
	if err != nil {
		return nil, errors.New("出口不存在")
	}
	v := map[string]any{"tag": o.Tag, "name": o.Name, "protocol": o.Protocol, "address": o.Address, "port": float64(o.Port), "username": o.Username,
		"enabled": o.Enabled, "password": "", "link": ""}
	if pw, err := s.reveal(o.Password); err == nil && pw != "" {
		if o.Protocol == "link" {
			v["link"] = pw
		} else {
			v["password"] = pw
		}
	}
	for _, k := range []string{"source_id", "mode", "match"} {
		if x, ok := o.Settings[k]; ok {
			v[k] = x
		}
	}
	return v, nil
}

func routeParams(update bool) []Param {
	first := Param{Key: "gate_id", Label: "VPS", Kind: "ref", Ref: "gate", Required: true}
	if update {
		first = Param{Key: "route_id", Label: "分流规则", Kind: "ref", Ref: "route", Required: true}
	}
	ps := []Param{first,
		{Key: "outbound_tag", Label: "出口", Kind: "string", Required: !update, Hint: "出口标签，见 list_outbounds；direct 为直连"},
		{Key: "domain", Label: "域名", Kind: "string", Hint: "每行或逗号一条：domain:netflix.com、geosite:disney、full:api.example.com"},
		{Key: "ip", Label: "IP", Kind: "string", Hint: "每行或逗号一条：1.1.1.1/32、geoip:jp"},
		{Key: "node_id", Label: "只对这个节点", Kind: "ref", Ref: "node"},
		{Key: "user_id", Label: "只对这个用户", Kind: "ref", Ref: "user"},
		{Key: "priority", Label: "优先级", Kind: "int", Min: num(0), Max: num(100000), Hint: "数字越小越先匹配"},
	}
	if update {
		ps = append(ps, Param{Key: "enabled", Label: "启用", Kind: "bool"})
	}
	return ps
}

func routePath(ctx context.Context, s *Server, args map[string]any) (gin.Params, error) {
	r, err := s.Store.Ent.Route.Get(ctx, numberOf(args["route_id"]))
	if err != nil {
		return nil, errors.New("分流规则不存在")
	}
	return pathOf("id", itoa(r.GateID), "rid", itoa(r.ID)), nil
}

func loadRoute(ctx context.Context, s *Server, args map[string]any) (map[string]any, error) {
	r, err := s.Store.Ent.Route.Get(ctx, numberOf(args["route_id"]))
	if err != nil {
		return nil, errors.New("分流规则不存在")
	}
	return map[string]any{"outbound_tag": r.OutboundTag, "domain": r.Domain, "ip": r.IP, "node_id": refOrNil(r.NodeID), "user_id": refOrNil(r.UserID),
		"priority": float64(r.Priority), "enabled": r.Enabled}, nil
}

// —— 订阅管理 ——

func sourceParams(update bool) []Param {
	ps := []Param{}
	if update {
		ps = append(ps, Param{Key: "source_id", Label: "订阅源", Kind: "ref", Ref: "source", Required: true})
	}
	return append(ps,
		Param{Key: "name", Label: "名称", Kind: "string"},
		Param{Key: "url", Label: "订阅地址", Kind: "string", Secret: true, Required: !update, Hint: "订阅地址带令牌，由管理员在确认窗里粘贴，草稿里留空"},
		Param{Key: "enabled", Label: "启用", Kind: "bool"},
		Param{Key: "user_agent", Label: "User-Agent", Kind: "string", Hint: "拉取订阅时用的 UA；不填用默认"},
		Param{Key: "filter_include", Label: "只保留", Kind: "string", Hint: "每行一条，支持正则"},
		Param{Key: "filter_exclude", Label: "排除", Kind: "string", Hint: "每行一条，支持正则"},
		Param{Key: "plus_as_space", Label: "名称中的 + 视为空格", Kind: "bool"},
		Param{Key: "node_cache", Label: "拉取失败时用上次的节点", Kind: "bool"},
		Param{Key: "exclude_traffic", Label: "不计入上游流量合计", Kind: "bool"},
		Param{Key: "website", Label: "官网", Kind: "string"},
		Param{Key: "remark", Label: "备注", Kind: "string"},
	)
}

func loadSource(ctx context.Context, s *Server, args map[string]any) (map[string]any, error) {
	v, err := s.Store.Ent.SubSource.Get(ctx, numberOf(args["source_id"]))
	if err != nil {
		return nil, errors.New("订阅源不存在")
	}
	return map[string]any{"name": v.Name, "url": v.URL, "enabled": v.Enabled, "user_agent": v.UserAgent, "filter_include": v.FilterInclude, "filter_exclude": v.FilterExclude,
		"plus_as_space": v.PlusAsSpace, "node_cache": v.NodeCache, "exclude_traffic": v.ExcludeTraffic, "website": v.Website, "remark": v.Remark}, nil
}

func profileParams(update bool) []Param {
	ps := []Param{}
	if update {
		ps = append(ps, Param{Key: "profile_id", Label: "订阅组", Kind: "ref", Ref: "profile", Required: true})
	}
	return append(ps,
		Param{Key: "name", Label: "名称", Kind: "string", Required: !update},
		Param{Key: "enabled", Label: "启用", Kind: "bool"},
		Param{Key: "all_sources", Label: "包含全部已启用的订阅源与手动节点", Kind: "bool"},
		Param{Key: "source_ids", Label: "订阅源", Kind: "refs", Ref: "source", Hint: "按这个顺序输出"},
		Param{Key: "node_ids", Label: "手动节点", Kind: "refs", Ref: "manual"},
		Param{Key: "settings", Label: "输出设置", Kind: "object", Hint: "可选键：template（builtin:编号 / custom:编号，见 list_templates）、rule_level、flag_emoji、prepend_manual、manual_prefix、prepend_sources、prepend_group_name、filter_include、filter_exclude、traffic_nodes；不给的沿用默认设置"},
		Param{Key: "expires_days", Label: "有效天数", Kind: "days", Unit: "天", Min: num(0), Max: num(3650), Hint: "从确认时起算；0 为不到期"},
	)
}

func profileBody(args map[string]any) map[string]any {
	body := pick(args, "name", "enabled", "all_sources", "source_ids", "node_ids", "settings")
	if v, ok := args["expires_days"].(float64); ok {
		if v > 0 {
			body["expires_at"] = time.Now().Add(time.Duration(v*24) * time.Hour).UTC()
		} else {
			body["expires_at"] = nil
		}
	}
	return body
}

func loadProfile(ctx context.Context, s *Server, args map[string]any) (map[string]any, error) {
	v, err := s.Store.Ent.SubProfile.Get(ctx, numberOf(args["profile_id"]))
	if err != nil {
		return nil, errors.New("订阅组不存在")
	}
	days := float64(0)
	if v.ExpiresAt != nil {
		days = float64(int(time.Until(*v.ExpiresAt).Hours()/24 + 0.5))
	}
	settings := map[string]any{}
	for k, x := range v.Settings {
		settings[k] = x
	}
	return map[string]any{"name": v.Name, "enabled": v.Enabled, "all_sources": v.AllSources, "source_ids": idList(v.SourceIds), "node_ids": idList(v.NodeIds),
		"settings": settings, "expires_days": days}, nil
}

func templateParams(update bool) []Param {
	ps := []Param{}
	if update {
		ps = append(ps, Param{Key: "template_id", Label: "规则模板", Kind: "ref", Ref: "template", Required: true})
	}
	return append(ps,
		Param{Key: "name", Label: "名称", Kind: "string", Required: !update},
		Param{Key: "format", Label: "模板格式", Kind: "enum", Required: !update, Options: [][2]string{{"ini", "INI（ACL4SSR 风格）"}, {"yaml", "YAML（Clash 规则片段）"}}},
		Param{Key: "content", Label: "内容", Kind: "text", Required: !update},
	)
}

func loadTemplate(ctx context.Context, s *Server, args map[string]any) (map[string]any, error) {
	v, err := s.Store.Ent.RuleTemplate.Get(ctx, numberOf(args["template_id"]))
	if err != nil {
		return nil, errors.New("规则模板不存在")
	}
	return map[string]any{"name": v.Name, "format": string(v.Format), "content": v.Content}, nil
}

func manualParams(update bool) []Param {
	ps := []Param{}
	if update {
		ps = append(ps, Param{Key: "manual_id", Label: "手动节点", Kind: "ref", Ref: "manual", Required: true})
	}
	return append(ps,
		Param{Key: "name", Label: "显示名称", Kind: "string"},
		Param{Key: "url", Label: "节点链接", Kind: "string", Secret: true, Required: !update, Hint: "分享链接里带密码，由管理员在确认窗里粘贴，草稿里留空"},
		Param{Key: "group", Label: "分组", Kind: "string"},
		Param{Key: "enabled", Label: "启用", Kind: "bool"},
	)
}

func loadManual(ctx context.Context, s *Server, args map[string]any) (map[string]any, error) {
	v, err := s.Store.Ent.SubNode.Get(ctx, numberOf(args["manual_id"]))
	if err != nil {
		return nil, errors.New("手动节点不存在")
	}
	return map[string]any{"name": v.Name, "url": v.URL, "group": v.Group, "enabled": v.Enabled}, nil
}

func linkPath(ctx context.Context, s *Server, args map[string]any) (gin.Params, error) {
	l, err := s.Store.Ent.Link.Get(ctx, numberOf(args["link_id"]))
	if err != nil {
		return nil, errors.New("链接不存在")
	}
	return pathOf("uid", itoa(l.UserID), "lid", itoa(l.ID)), nil
}

func loadLink(ctx context.Context, s *Server, args map[string]any) (map[string]any, error) {
	l, err := s.Store.Ent.Link.Get(ctx, numberOf(args["link_id"]))
	if err != nil {
		return nil, errors.New("链接不存在")
	}
	return map[string]any{"name": l.Name, "all_nodes": l.AllNodes, "node_ids": idList(l.NodeIds), "enabled": l.Enabled}, nil
}

func h(f func(s *Server) gin.HandlerFunc) func(*Server) gin.HandlerFunc { return f }

func forceQuery(args map[string]any) url.Values {
	if args["force"] == true {
		return url.Values{"force": {"1"}}
	}
	return nil
}

var opDraftCaps = []Capability{
	{Name: "node.create", Title: "新建节点", Purpose: "在一台 VPS 上按协议方案新建节点，与手动「新建节点」同一流程：分配端口、准备证书、REALITY 由 VPS 实测伪装目标，保存前在这台 VPS 上用真实内核实测连接；试运行通过才弹窗，确认后按试运行定下的端口与伪装目标保存。",
		Kind: "draft", Risk: "normal", Object: "node", Creates: true, Params: nodeCreateParams,
		op: &opSpec{kind: "create", method: "POST", handler: h(func(s *Server) gin.HandlerFunc { return s.createNode }), path: noPath, body: nodeCreateBody,
			probe: true, undo: "node.delete", idKey: "node_id", title: nodeCreateTitle}},
	{Name: "node.delete", Title: "删除节点", Purpose: "删除节点及其用户凭据与分流规则；已导入客户端的这个节点随即失效。被其他节点当作出口时拒绝；被用户自建链式代理当作出口时要 force 才一并删除。不能撤回，只能是最后一步。",
		Kind: "draft", Risk: "danger", Object: "node", Last: true,
		Params: []Param{{Key: "node_id", Label: "节点", Kind: "ref", Ref: "node", Required: true}, {Key: "force", Label: "一并删除依赖它的用户链式代理", Kind: "bool"}},
		op:     &opSpec{kind: "delete", method: "DELETE", handler: h(func(s *Server) gin.HandlerFunc { return s.deleteNode }), path: keys("node_id", "id"), pathKeys: []string{"node_id"}, query: forceQuery, self: "node_id"}},
	{Name: "gate.update", Title: "修改 VPS", Purpose: "修改 VPS 的名称、公网地址（客户端连接节点用；保存前检查格式与解析）或内核版本锁定。",
		Kind: "draft", Risk: "normal", Object: "gate",
		Params: []Param{{Key: "gate_id", Label: "VPS", Kind: "ref", Ref: "gate", Required: true}, {Key: "name", Label: "名称", Kind: "string"},
			{Key: "public_host", Label: "公网地址", Kind: "string", Hint: "域名或公网 IP，不带协议与端口"},
			{Key: "kernel_pins", Label: "内核版本锁定", Kind: "object", Hint: "如 {\"xray\": \"26.3.27\"}；不写某个内核表示跟随默认版本"}},
		op: &opSpec{kind: "update", method: "PATCH", handler: h(func(s *Server) gin.HandlerFunc { return s.updateGate }), path: keys("gate_id", "id"), pathKeys: []string{"gate_id"}, self: "gate_id", load: loadGate}},

	{Name: "outbound.create", Title: "新建出口", Purpose: "在一台 VPS 上新建出口（SOCKS5 / HTTP / HTTPS 代理、分享链接、订阅源里的节点、直连、丢弃）；出口要由分流规则引用才有流量。保存前确认这台 VPS 上用到的每个内核都能连接它。",
		Kind: "draft", Risk: "normal", Object: "outbound", Creates: true, Params: outboundParams(false),
		op: &opSpec{kind: "create", method: "POST", handler: h(func(s *Server) gin.HandlerFunc { return s.saveOutbound }), path: keys("gate_id", "id"), pathKeys: []string{"gate_id"},
			needs: outboundNeeds, undo: "outbound.delete", idKey: "outbound_id"}},
	{Name: "outbound.update", Title: "修改出口", Purpose: "修改出口；没给的字段保持原样（密码、分享链接不给就不改）。仍被启用的分流规则引用时不能改标签或停用。",
		Kind: "draft", Risk: "normal", Object: "outbound", Params: outboundParams(true),
		op: &opSpec{kind: "update", method: "PATCH", handler: h(func(s *Server) gin.HandlerFunc { return s.saveOutbound }), path: outboundPath, pathKeys: []string{"outbound_id"},
			self: "outbound_id", load: loadOutbound, full: true, needs: outboundNeeds}},
	{Name: "outbound.delete", Title: "删除出口", Purpose: "删除出口；还有分流规则引用它时拒绝（先改规则）。不能撤回，只能是最后一步。",
		Kind: "draft", Risk: "danger", Object: "outbound", Last: true, Params: []Param{{Key: "outbound_id", Label: "出口", Kind: "ref", Ref: "outbound", Required: true}},
		op: &opSpec{kind: "delete", method: "DELETE", handler: h(func(s *Server) gin.HandlerFunc { return s.deleteOutbound }), path: outboundPath, pathKeys: []string{"outbound_id"}, self: "outbound_id"}},
	{Name: "route.create", Title: "新建分流规则", Purpose: "在一台 VPS 上新建分流规则：匹配域名或 IP 的流量走指定出口；可以只对某个节点或某个用户生效。",
		Kind: "draft", Risk: "normal", Object: "route", Creates: true, Params: routeParams(false),
		op: &opSpec{kind: "create", method: "POST", handler: h(func(s *Server) gin.HandlerFunc { return s.saveRoute }), path: keys("gate_id", "id"), pathKeys: []string{"gate_id"},
			undo: "route.delete", idKey: "route_id"}},
	{Name: "route.update", Title: "修改分流规则", Purpose: "修改分流规则；没给的字段保持原样。",
		Kind: "draft", Risk: "normal", Object: "route", Params: routeParams(true),
		op: &opSpec{kind: "update", method: "PATCH", handler: h(func(s *Server) gin.HandlerFunc { return s.saveRoute }), path: routePath, pathKeys: []string{"route_id"},
			self: "route_id", load: loadRoute, full: true}},
	{Name: "route.delete", Title: "删除分流规则", Purpose: "删除分流规则。不能撤回，只能是最后一步。",
		Kind: "draft", Risk: "danger", Object: "route", Last: true, Params: []Param{{Key: "route_id", Label: "分流规则", Kind: "ref", Ref: "route", Required: true}},
		op: &opSpec{kind: "delete", method: "DELETE", handler: h(func(s *Server) gin.HandlerFunc { return s.deleteRoute }), path: routePath, pathKeys: []string{"route_id"}, self: "route_id"}},

	{Name: "source.create", Title: "新建订阅源", Purpose: "新建订阅源（MiSub 的上游订阅）。订阅地址带令牌，草稿里留空，由管理员在确认窗里粘贴；保存后可以加一步 source.refresh 拉取节点。",
		Kind: "draft", Risk: "normal", Object: "source", Creates: true, Params: sourceParams(false),
		op: &opSpec{kind: "create", method: "POST", handler: h(func(s *Server) gin.HandlerFunc { return s.saveSource }), path: noPath, undo: "source.delete", idKey: "source_id"}},
	{Name: "source.update", Title: "修改订阅源", Purpose: "修改订阅源；没给的字段保持原样（订阅地址不给就不改）。",
		Kind: "draft", Risk: "normal", Object: "source", Params: sourceParams(true),
		op: &opSpec{kind: "update", method: "PATCH", handler: h(func(s *Server) gin.HandlerFunc { return s.saveSource }), path: keys("source_id", "id"), pathKeys: []string{"source_id"}, self: "source_id", load: loadSource}},
	{Name: "source.delete", Title: "删除订阅源", Purpose: "删除订阅源，并从所有订阅组里移除。不能撤回，只能是最后一步。",
		Kind: "draft", Risk: "danger", Object: "source", Last: true, Params: []Param{{Key: "source_id", Label: "订阅源", Kind: "ref", Ref: "source", Required: true}},
		op: &opSpec{kind: "delete", method: "DELETE", handler: h(func(s *Server) gin.HandlerFunc { return s.deleteSource }), path: keys("source_id", "id"), pathKeys: []string{"source_id"}, self: "source_id"}},
	{Name: "source.refresh", Title: "刷新订阅源", Purpose: "立即重新拉取订阅源，更新节点缓存与用量。",
		Kind: "draft", Risk: "normal", Object: "source", Params: []Param{{Key: "source_id", Label: "订阅源", Kind: "ref", Ref: "source", Required: true}},
		op: &opSpec{kind: "action", method: "POST", handler: h(func(s *Server) gin.HandlerFunc { return s.refreshSource }), path: keys("source_id", "id"), pathKeys: []string{"source_id"}, self: "source_id"}},
	{Name: "profile.create", Title: "新建订阅组", Purpose: "新建订阅组：选哪些订阅源与手动节点、按什么规则模板输出。分配给用户用 profile.grant。",
		Kind: "draft", Risk: "normal", Object: "profile", Creates: true, Params: profileParams(false),
		op: &opSpec{kind: "create", method: "POST", handler: h(func(s *Server) gin.HandlerFunc { return s.saveProfile }), path: noPath, body: profileBody, undo: "profile.delete", idKey: "profile_id"}},
	{Name: "profile.update", Title: "修改订阅组", Purpose: "修改订阅组；没给的字段保持原样。",
		Kind: "draft", Risk: "normal", Object: "profile", Params: profileParams(true),
		op: &opSpec{kind: "update", method: "PATCH", handler: h(func(s *Server) gin.HandlerFunc { return s.saveProfile }), path: keys("profile_id", "id"), pathKeys: []string{"profile_id"},
			self: "profile_id", load: loadProfile, body: profileBody}},
	{Name: "profile.grant", Title: "分配订阅组", Purpose: "设置订阅组分配给哪些用户（完整名单：不在名单里的用户取消分配，他们的这个订阅地址随即失效；新分配的用户各得到一个订阅地址）。重新分配会换新的订阅地址，所以这一步不能撤回。",
		Kind: "draft", Risk: "normal", Object: "profile",
		Params: []Param{{Key: "profile_id", Label: "订阅组", Kind: "ref", Ref: "profile", Required: true}, {Key: "user_ids", Label: "分配给", Kind: "refs", Ref: "user", Required: true, Hint: "完整的用户名单"}},
		op: &opSpec{kind: "update", method: "PUT", handler: h(func(s *Server) gin.HandlerFunc { return s.setGrants }), path: keys("profile_id", "id"), pathKeys: []string{"profile_id"},
			self: "profile_id", noUndo: true, load: func(ctx context.Context, s *Server, args map[string]any) (map[string]any, error) {
				gs, err := s.Store.Ent.SubGrant.Query().Where(subgrant.ProfileIDEQ(numberOf(args["profile_id"]))).Order(ent.Asc(subgrant.FieldUserID)).All(ctx)
				ids := make([]any, 0, len(gs))
				for _, g := range gs {
					ids = append(ids, float64(g.UserID))
				}
				return map[string]any{"user_ids": ids}, err
			}}},
	{Name: "profile.delete", Title: "删除订阅组", Purpose: "删除订阅组：分配给用户的这个订阅地址随即失效。不能撤回，只能是最后一步。",
		Kind: "draft", Risk: "danger", Object: "profile", Last: true, Params: []Param{{Key: "profile_id", Label: "订阅组", Kind: "ref", Ref: "profile", Required: true}},
		op: &opSpec{kind: "delete", method: "DELETE", handler: h(func(s *Server) gin.HandlerFunc { return s.deleteProfile }), path: keys("profile_id", "id"), pathKeys: []string{"profile_id"}, self: "profile_id"}},
	{Name: "template.create", Title: "新建规则模板", Purpose: "新建自定义规则模板（保存前检查格式）；订阅组的 settings.template 写 custom:编号 使用它。",
		Kind: "draft", Risk: "normal", Object: "template", Creates: true, Params: templateParams(false),
		op: &opSpec{kind: "create", method: "POST", handler: h(func(s *Server) gin.HandlerFunc { return s.saveTemplate }), path: noPath, undo: "template.delete", idKey: "template_id"}},
	{Name: "template.update", Title: "修改规则模板", Purpose: "修改自定义规则模板；确认窗显示与现在内容的逐行差异。",
		Kind: "draft", Risk: "normal", Object: "template", Params: templateParams(true),
		op: &opSpec{kind: "update", method: "PATCH", handler: h(func(s *Server) gin.HandlerFunc { return s.saveTemplate }), path: keys("template_id", "id"), pathKeys: []string{"template_id"},
			self: "template_id", load: loadTemplate, full: true}},
	{Name: "template.delete", Title: "删除规则模板", Purpose: "删除自定义规则模板；还有订阅组或默认设置在用时拒绝。不能撤回，只能是最后一步。",
		Kind: "draft", Risk: "danger", Object: "template", Last: true, Params: []Param{{Key: "template_id", Label: "规则模板", Kind: "ref", Ref: "template", Required: true}},
		op: &opSpec{kind: "delete", method: "DELETE", handler: h(func(s *Server) gin.HandlerFunc { return s.deleteTemplate }), path: keys("template_id", "id"), pathKeys: []string{"template_id"}, self: "template_id"}},
	{Name: "manual.create", Title: "新建手动节点", Purpose: "新建手动节点（一条节点分享链接，给订阅组用）。链接带密码，草稿里留空，由管理员在确认窗里粘贴。",
		Kind: "draft", Risk: "normal", Object: "manual", Creates: true, Params: manualParams(false),
		op: &opSpec{kind: "create", method: "POST", handler: h(func(s *Server) gin.HandlerFunc { return s.saveManualNode }), path: noPath, undo: "manual.delete", idKey: "manual_id"}},
	{Name: "manual.update", Title: "修改手动节点", Purpose: "修改手动节点；没给的字段保持原样（链接不给就不改）。",
		Kind: "draft", Risk: "normal", Object: "manual", Params: manualParams(true),
		op: &opSpec{kind: "update", method: "PATCH", handler: h(func(s *Server) gin.HandlerFunc { return s.saveManualNode }), path: keys("manual_id", "id"), pathKeys: []string{"manual_id"}, self: "manual_id", load: loadManual}},
	{Name: "manual.delete", Title: "删除手动节点", Purpose: "删除手动节点，并从所有订阅组里移除。不能撤回，只能是最后一步。",
		Kind: "draft", Risk: "danger", Object: "manual", Last: true, Params: []Param{{Key: "manual_id", Label: "手动节点", Kind: "ref", Ref: "manual", Required: true}},
		op: &opSpec{kind: "delete", method: "DELETE", handler: h(func(s *Server) gin.HandlerFunc { return s.deleteManualNode }), path: keys("manual_id", "id"), pathKeys: []string{"manual_id"}, self: "manual_id"}},

	{Name: "kernel.fetch", Title: "下载内核版本", Purpose: "开始下载一个 Xray / sing-box / mihomo 版本（按官方摘要校验，后台进行）。可选版本用 list_kernel_releases 查；这一步开始后不能撤回。",
		Kind: "draft", Risk: "normal",
		Params: []Param{{Key: "kernel", Label: "内核", Kind: "enum", Required: true, Options: [][2]string{{"xray", "Xray"}, {"sing-box", "sing-box"}, {"mihomo", "mihomo"}}},
			{Key: "version", Label: "版本", Kind: "string", Required: true, Hint: "如 26.3.27，不带 v"}},
		op: &opSpec{kind: "action", method: "POST", handler: h(func(s *Server) gin.HandlerFunc { return s.fetchKernel }), pathKeys: []string{"kernel"},
			path: func(_ context.Context, _ *Server, args map[string]any) (gin.Params, error) {
				k, _ := args["kernel"].(string)
				return pathOf("kernel", k), nil
			},
			title: func(_ context.Context, _ *Server, _ *planCtx, args map[string]any) string {
				return fmt.Sprintf("下载 %v %v", args["kernel"], args["version"])
			}}},
	{Name: "kernel.restart", Title: "重启内核", Purpose: "重启 VPS 上的一个内核：这个内核上的节点会断开几秒，客户端自动重连。用于内核卡住、诊断显示异常时；这一步不能撤回。",
		Kind: "draft", Risk: "normal", Object: "gate",
		Params: []Param{{Key: "gate_id", Label: "VPS", Kind: "ref", Ref: "gate", Required: true}, {Key: "kernel", Label: "内核", Kind: "enum", Required: true, Options: [][2]string{{"xray", "Xray"}, {"sing-box", "sing-box"}}}},
		op: &opSpec{kind: "action", method: "POST", handler: func(s *Server) gin.HandlerFunc { return s.gateKernelAction(contracts.ActionCoreRestart) }, pathKeys: []string{"gate_id", "kernel"},
			path: func(_ context.Context, _ *Server, args map[string]any) (gin.Params, error) {
				k, _ := args["kernel"].(string)
				return pathOf("id", itoa(numberOf(args["gate_id"])), "kernel", k), nil
			},
			title: func(ctx context.Context, s *Server, p *planCtx, args map[string]any) string {
				g, _ := s.refName(ctx, p, "gate", args["gate_id"])
				return fmt.Sprintf("重启 %s 上的 %v", g, args["kernel"])
			}}},
	{Name: "cert.renew", Title: "续签证书", Purpose: "立即续签中心签发的证书（IP 证书、节点证书域名）；1Panel 证书由 1Panel 续签。",
		Kind: "draft", Risk: "normal", Object: "cert", Params: []Param{{Key: "cert_id", Label: "证书", Kind: "ref", Ref: "cert", Required: true}},
		op: &opSpec{kind: "action", method: "POST", handler: h(func(s *Server) gin.HandlerFunc { return s.renewCertNow }), path: keys("cert_id", "id"), pathKeys: []string{"cert_id"}, self: "cert_id"}},
	{Name: "user.reset_usage", Title: "清零用量", Purpose: "把用户的已用流量清零、从现在重新计算。不能撤回，只能是最后一步。",
		Kind: "draft", Risk: "normal", Object: "user", Last: true, Params: []Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true}},
		op: &opSpec{kind: "action", method: "POST", handler: h(func(s *Server) gin.HandlerFunc { return s.resetUsageHandler }), path: keys("user_id", "uid"), pathKeys: []string{"user_id"}, self: "user_id"}},
	{Name: "link.update", Title: "修改订阅链接", Purpose: "修改订阅链接的名称、包含的节点或是否启用；订阅地址不变。",
		Kind: "draft", Risk: "normal", Object: "link",
		Params: []Param{{Key: "link_id", Label: "链接", Kind: "ref", Ref: "link", Required: true}, {Key: "name", Label: "名称", Kind: "string"},
			{Key: "all_nodes", Label: "包含全部节点", Kind: "bool"}, {Key: "node_ids", Label: "包含的节点", Kind: "refs", Ref: "node"}, {Key: "enabled", Label: "启用", Kind: "bool"}},
		op: &opSpec{kind: "update", method: "PATCH", handler: func(s *Server) gin.HandlerFunc { return func(c *gin.Context) { s.updateLink(c, true) } }, path: linkPath, pathKeys: []string{"link_id"},
			self: "link_id", load: loadLink}},
	{Name: "link.delete", Title: "删除订阅链接", Purpose: "删除订阅链接：这个订阅地址与它在各节点上的凭据随即失效。不能撤回，只能是最后一步。",
		Kind: "draft", Risk: "danger", Object: "link", Last: true, Params: []Param{{Key: "link_id", Label: "链接", Kind: "ref", Ref: "link", Required: true}},
		op: &opSpec{kind: "delete", method: "DELETE", handler: func(s *Server) gin.HandlerFunc { return func(c *gin.Context) { s.deleteLink(c, true) } }, path: linkPath, pathKeys: []string{"link_id"}, self: "link_id"}},
}

func misubBuiltinTemplates() any { return misub.BuiltinTemplates() }

func listOf[T any](items []T, view func(T) any) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, toModel(view(it)))
	}
	return out
}

var opReadCaps = []Capability{
	{Name: "list_presets", Title: "协议方案", Purpose: "列出建节点可选的协议方案（就是「新建节点」第 1 步的选项）：编号、名称、说明、内核、协议、传输、安全（reality / tls / none）、是否需要证书、能否作出口，以及「高级」里可以改的字段。推荐协议前先读，不要凭印象。", Kind: "read",
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			return toModel(s.presets(ctx)), nil
		}},
	{Name: "get_gate", Title: "VPS 详情", Purpose: "读取一台 VPS 的详情：在线、配置是否生效、程序版本、系统与架构、公网地址、运行中的内核、内核版本锁定、正在监听的端口（VPS 上报的）、防火墙、1Panel，以及这台 VPS 上的节点、出口、分流规则数量。", Kind: "read",
		Params: []Param{{Key: "gate_id", Label: "VPS", Kind: "ref", Ref: "gate", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			g, err := s.Store.Ent.Gate.Get(ctx, intArg(args, "gate_id"))
			if err != nil {
				return nil, errors.New("VPS 不存在")
			}
			v, _ := toModel(gateView(g)).(map[string]any)
			v["nodes"], _ = s.Store.Ent.Node.Query().Where(entnode.GateIDEQ(g.ID)).Count(ctx)
			v["outbounds"], _ = s.Store.Ent.Outbound.Query().Where(outbound.GateIDEQ(g.ID)).Count(ctx)
			v["routes"], _ = s.Store.Ent.Route.Query().Where(route.GateIDEQ(g.ID)).Count(ctx)
			return v, nil
		}},
	{Name: "check_port", Title: "端口检查", Purpose: "检查一台 VPS 上某个端口能不能给新节点用（本项目的节点、统计端口与 VPS 上报的其他程序都算占用）；不给端口时返回系统会自动分配的端口。VPS 还没上报监听端口时其他程序的占用是「未知」。", Kind: "read",
		Params: []Param{{Key: "gate_id", Label: "VPS", Kind: "ref", Ref: "gate", Required: true}, {Key: "port", Label: "端口", Kind: "int", Min: num(1), Max: num(65535)},
			{Key: "reality", Label: "REALITY 节点", Kind: "bool", Hint: "REALITY 自动分配时优先 443"}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			g, err := s.Store.Ent.Gate.Get(ctx, intArg(args, "gate_id"))
			if err != nil {
				return nil, errors.New("VPS 不存在")
			}
			reported := g.ListeningPorts != nil
			out := map[string]any{"ports_reported": reported}
			if !reported {
				out["note"] = "这台 VPS 还没上报正在监听的端口：只能确认本项目自己没占用，其他程序是否占用未知"
			}
			if port := intArg(args, "port"); port > 0 {
				out["port"] = port
				switch err := s.checkPort(ctx, g, port, 0); {
				case err == nil && reported:
					out["free"] = true
				case err == nil:
					out["free"] = "未知"
				case err.Error() == "invalid_port":
					out["free"], out["reason"] = false, "手动指定的端口要在 1024–65535（443 只能由系统为 REALITY 自动分配）"
				default:
					out["free"], out["reason"] = false, "已被本项目的其他节点、统计端口或 VPS 上的其他程序占用"
				}
			}
			if p, err := s.allocateNodePort(ctx, g, args["reality"] == true); err == nil {
				out["suggested"] = p
			}
			return out, nil
		}},
	{Name: "probe_reality", Title: "伪装目标实测", Purpose: "让 VPS 实测内置的 REALITY 伪装候选站点，返回每个站点能不能用作伪装目标与延迟（新建 REALITY 节点不填伪装目标时就按这个结果选）。会让 VPS 实际发起连接，记审计。", Kind: "read", Probe: true,
		Params: []Param{{Key: "gate_id", Label: "VPS", Kind: "ref", Ref: "gate", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			return s.readVia(ctx, s.probeReality, "POST", pathOf("id", itoa(intArg(args, "gate_id"))), nil, nil)
		}},
	{Name: "test_node", Title: "节点实测", Purpose: "让节点所在 VPS 用节点现在的参数实测一次（内核自检 + 本机回环真连），返回到达的阶段（build / check / connect）、延迟与失败原因。不改节点；会让 VPS 实际发起连接，记审计。", Kind: "read", Probe: true,
		Params: []Param{{Key: "node_id", Label: "节点", Kind: "ref", Ref: "node", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			return s.readVia(ctx, s.testNodeHandler, "POST", pathOf("id", itoa(intArg(args, "node_id"))), nil, nil)
		}},
	{Name: "probe_outbound", Title: "出口实测", Purpose: "让出口所在 VPS 经这个出口真连一次，返回实际出口 IP 与延迟（代理与分享链接类出口）。会让 VPS 实际发起连接，记审计。", Kind: "read", Probe: true,
		Params: []Param{{Key: "outbound_id", Label: "出口", Kind: "ref", Ref: "outbound", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			p, err := outboundPath(ctx, s, args)
			if err != nil {
				return nil, err
			}
			return s.readVia(ctx, s.probeOutbound, "POST", p, nil, nil)
		}},
	{Name: "get_kernel_diagnostics", Title: "内核诊断", Purpose: "读取 VPS 上一个内核的诊断：错误分类计数与最近的日志（来源 IP 已遮盖）。排查连不上、断流时先读。会让 VPS 执行一次读取，记审计。", Kind: "read", Probe: true,
		Params: []Param{{Key: "gate_id", Label: "VPS", Kind: "ref", Ref: "gate", Required: true}, {Key: "kernel", Label: "内核", Kind: "enum", Required: true, Options: [][2]string{{"xray", "Xray"}, {"sing-box", "sing-box"}}}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			k, _ := args["kernel"].(string)
			return s.readVia(ctx, s.gateKernelAction(contracts.ActionDiagnostics), "POST", pathOf("id", itoa(intArg(args, "gate_id")), "kernel", k), nil, nil)
		}},
	{Name: "list_kernels", Title: "内核版本", Purpose: "列出已下载的 Xray / sing-box / mihomo 版本：各架构是否齐全、是否默认版本、对应驱动、被哪些 VPS 锁定或正在运行。", Kind: "read",
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			return s.readVia(ctx, s.listKernels, "GET", nil, nil, nil)
		}},
	{Name: "list_kernel_releases", Title: "内核发布版本", Purpose: "列出某个内核在 GitHub 上最近的发布版本（最多 20 个），下载前先查。", Kind: "read",
		Params: []Param{{Key: "kernel", Label: "内核", Kind: "enum", Required: true, Options: [][2]string{{"xray", "Xray"}, {"sing-box", "sing-box"}, {"mihomo", "mihomo"}}}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			k, _ := args["kernel"].(string)
			return s.readVia(ctx, s.kernelReleases, "GET", pathOf("kernel", k), nil, nil)
		}},
	{Name: "list_outbounds", Title: "出口", Purpose: "列出一台 VPS 上的出口：编号、标签、名称、类型、地址与端口、用户名、是否有密码、是否启用、订阅源出口当前用的节点、最近一次实测（出口 IP、延迟）。", Kind: "read",
		Params: []Param{{Key: "gate_id", Label: "VPS", Kind: "ref", Ref: "gate", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			items, err := s.Store.Ent.Outbound.Query().Where(outbound.GateIDEQ(intArg(args, "gate_id"))).Order(ent.Asc(outbound.FieldPosition), ent.Asc(outbound.FieldID)).All(ctx)
			return listOf(items, func(o *ent.Outbound) any {
				v := s.outboundView(o)
				v["has_password"] = v["password"] != nil && v["password"] != ""
				delete(v, "password")
				delete(v, "link")
				return v
			}), err
		}},
	{Name: "list_routes", Title: "分流规则", Purpose: "列出一台 VPS 上的分流规则：编号、出口标签、域名、IP、只对哪个节点 / 用户、优先级、是否启用。", Kind: "read",
		Params: []Param{{Key: "gate_id", Label: "VPS", Kind: "ref", Ref: "gate", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			items, err := s.Store.Ent.Route.Query().Where(route.GateIDEQ(intArg(args, "gate_id"))).Order(ent.Asc(route.FieldPriority), ent.Asc(route.FieldID)).All(ctx)
			return listOf(items, func(r *ent.Route) any { return routeView(r) }), err
		}},
	{Name: "list_sources", Title: "订阅源", Purpose: "列出订阅源：编号、名称、启用、订阅地址（只留主机）、筛选、节点数、上游用量与到期、最近一次拉取的时间与错误。", Kind: "read",
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			items, err := s.Store.Ent.SubSource.Query().Order(ent.Asc(subsource.FieldPosition), ent.Asc(subsource.FieldID)).All(ctx)
			return listOf(items, func(v *ent.SubSource) any { return sourceView(v) }), err
		}},
	{Name: "list_manual_nodes", Title: "手动节点", Purpose: "列出手动节点：编号、名称、协议与主机（链接里的密码不给）、分组、是否启用。", Kind: "read",
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			items, err := s.Store.Ent.SubNode.Query().Order(ent.Asc(subnode.FieldPosition), ent.Asc(subnode.FieldID)).All(ctx)
			return listOf(items, func(v *ent.SubNode) any { return manualView(v) }), err
		}},
	{Name: "list_profiles", Title: "订阅组", Purpose: "列出订阅组：编号、名称、启用、包含的订阅源与手动节点、输出设置、到期、分配给哪些用户、下载次数。", Kind: "read",
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			items, err := s.Store.Ent.SubProfile.Query().Order(ent.Asc(subprofile.FieldPosition), ent.Asc(subprofile.FieldID)).All(ctx)
			return listOf(items, func(v *ent.SubProfile) any { return s.profileView(ctx, v) }), err
		}},
	{Name: "list_templates", Title: "规则模板", Purpose: "列出规则模板：内置模板（引用写 builtin:编号）与自定义模板（custom:编号，含内容）。", Kind: "read",
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			items, err := s.Store.Ent.RuleTemplate.Query().Order(ent.Asc(ruletemplate.FieldID)).All(ctx)
			custom := listOf(items, func(v *ent.RuleTemplate) any {
				return map[string]any{"id": v.ID, "ref": "custom:" + itoa(v.ID), "name": v.Name, "format": v.Format, "content": v.Content, "updated_at": v.UpdatedAt}
			})
			return map[string]any{"builtin": toModel(misubBuiltinTemplates()), "custom": custom}, err
		}},
	{Name: "list_certs", Title: "证书", Purpose: "列出证书：编号、名称、域名、来源（上传 / 自签 / 中心签发 / 1Panel）、所属 VPS、到期时间与状态、续签错误。不含私钥。", Kind: "read",
		read: func(ctx context.Context, s *Server, _ map[string]any) (any, error) {
			items, err := s.Store.Ent.Certificate.Query().Order(ent.Asc(certificate.FieldID)).All(ctx)
			return listOf(items, func(v *ent.Certificate) any { return certView(v) }), err
		}},
	{Name: "list_audit", Title: "操作审计", Purpose: "查最近的操作审计（新到旧，最多 50 条）：操作、目标、结果、时间；q 匹配操作名或目标。不含来源地址。", Kind: "read",
		Params: []Param{{Key: "q", Label: "关键词", Kind: "string"}, {Key: "result", Label: "结果", Kind: "string", Hint: "如 ok、failed"}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			q := s.Store.Ent.Audit.Query()
			if v, _ := args["q"].(string); strings.TrimSpace(v) != "" {
				q = q.Where(audit.Or(audit.ActionContains(v), audit.TargetContains(v)))
			}
			if v, _ := args["result"].(string); v != "" {
				q = q.Where(audit.ResultEQ(v))
			}
			items, err := q.Order(ent.Desc(audit.FieldID)).Limit(50).All(ctx)
			return listOf(items, func(a *ent.Audit) any {
				return map[string]any{"id": a.ID, "actor": a.Actor, "action": a.Action, "target": a.Target, "result": a.Result, "created_at": a.CreatedAt}
			}), err
		}},
}
