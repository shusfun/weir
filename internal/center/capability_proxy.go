package center

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/packages/contracts"
)

// 代理页与订阅管理的能力：读代理设置与速率排序、读拉取统计（不含来源地址）；草稿改类型开关 / 分流 / 自动选择、
// 给类型加出口、去掉类型出口。处理函数与手动操作相同（proxy_routes.go、sub_pulls.go），试运行在第一次写入前停下。
// SSH 安装 VPS 不做成能力：登录密码与私钥不能经过模型，主机指纹要管理员当场核对。

// 在包变量初始化时并进声明式能力表：capability_ops.go 的 init 给每项接上通用的试运行 / 执行，init 在全部包变量之后运行。
var _ = func() bool {
	opReadCaps = append(opReadCaps, proxyReadCaps...)
	opDraftCaps = append(opDraftCaps, proxyDraftCaps...)
	return true
}()

const (
	targetHint = "auto（自动选择）、select（节点选择，手动）、direct（直连）、reject（拒绝，「其余流量」不能用）或一个类型的 key（见 get_proxy 的 types）"
	typesHint  = "类型的 key，多个用英文逗号分隔（见 get_proxy 的 types）；空字符串表示全部放进订阅"
)

func userPath(_ context.Context, _ *Server, args map[string]any) (gin.Params, error) {
	return pathOf("uid", itoa(numberOf(args["user_id"]))), nil
}

func typeExitPath(_ context.Context, _ *Server, args map[string]any) (gin.Params, error) {
	t, _ := args["type_key"].(string)
	if t == "" || len(t) > 64 {
		return nil, errors.New("类型 key 无效")
	}
	return pathOf("uid", itoa(numberOf(args["user_id"])), "type", t), nil
}

func proxyParams() []Param {
	ps := []Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true},
		{Key: "disabled", Label: "不放进订阅的类型", Kind: "string", Hint: typesHint}}
	for _, rc := range ruleCategories {
		ps = append(ps, Param{Key: "rule_" + rc.Key, Label: "分流：" + rc.Label, Kind: "string", Hint: targetHint + "；默认 " + rc.Default})
	}
	return append(ps,
		Param{Key: "auto_mode", Label: "自动选择", Kind: "enum", Options: [][2]string{{contracts.AutoLatency, "客户端测延迟"}, {contracts.AutoRanked, "按速率排序"}}},
		Param{Key: "auto_top", Label: "放进自动选择的节点数", Kind: "int", Min: num(1), Max: num(20), Hint: "按速率排序时用；大于 1 时其中 1 个是轮换位"},
		Param{Key: "refresh_minutes", Label: "节点列表刷新间隔", Kind: "int", Unit: "分钟", Min: num(contracts.MinRefreshMinutes), Max: num(24 * 60)})
}

// loadProxy 读出现在的设置（与参数同名同单位），用于显示改前值、补齐没给的字段、撤回。
func loadProxy(ctx context.Context, s *Server, args map[string]any) (map[string]any, error) {
	u, err := s.Store.Ent.User.Get(ctx, numberOf(args["user_id"]))
	if err != nil {
		return nil, errors.New("用户不存在")
	}
	pol := loadPolicy(u)
	v := map[string]any{"disabled": strings.Join(pol.Disabled, ","), "auto_mode": pol.Auto.Mode, "auto_top": float64(pol.Auto.Top), "refresh_minutes": float64(pol.Auto.RefreshMinutes)}
	for _, rc := range ruleCategories {
		v["rule_"+rc.Key] = firstNonEmpty(pol.Rules[rc.Key], rc.Default)
	}
	return v, nil
}

// proxyBody 把展开的参数换回 PUT /proxy 的请求体（处理函数整体覆盖，没给的字段已由 load 补齐）。
func proxyBody(args map[string]any) map[string]any {
	disabled := []string{}
	if raw, _ := args["disabled"].(string); raw != "" {
		for _, t := range strings.Split(raw, ",") {
			if t = strings.TrimSpace(t); t != "" {
				disabled = append(disabled, t)
			}
		}
	}
	rules := map[string]any{}
	for _, rc := range ruleCategories {
		if v, ok := args["rule_"+rc.Key].(string); ok && v != "" {
			rules[rc.Key] = v
		}
	}
	return map[string]any{"disabled": disabled, "rules": rules,
		"auto": map[string]any{"mode": args["auto_mode"], "top": numberOf(args["auto_top"]), "refresh_minutes": numberOf(args["refresh_minutes"])}}
}

func typeExitParams() []Param {
	return []Param{
		{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true},
		{Key: "type_key", Label: "类型", Kind: "string", Required: true, Hint: "类型的 key（见 get_proxy 的 types）"},
		{Key: "kind", Label: "出口类型", Kind: "enum", Required: true, Options: [][2]string{{"socks5", "SOCKS5 代理"}, {"http", "HTTP 代理"}, {"https", "HTTPS 代理"},
			{"link", "分享链接"}, {"node", "另一节点"}, {"source", "订阅组里的节点"}}},
		{Key: "address", Label: "地址", Kind: "string", Hint: "代理类填写；只能是公网地址"},
		{Key: "port", Label: "端口", Kind: "int", Min: num(1), Max: num(65535)},
		{Key: "username", Label: "用户名", Kind: "string"},
		{Key: "password", Label: "代理密码", Kind: "string", Secret: true, Hint: "由管理员在确认窗里填，草稿里留空"},
		{Key: "link", Label: "分享链接", Kind: "string", Secret: true, Hint: "一个节点的链接，由管理员在确认窗里粘贴，草稿里留空"},
		{Key: "node_id", Label: "出口节点", Kind: "ref", Ref: "node", Hint: "kind 为 node 时填；这个用户可用的另一个节点"},
		{Key: "profile_id", Label: "订阅组", Kind: "ref", Ref: "profile", Hint: "kind 为 source 时填；分给这个用户的订阅组"},
		{Key: "mode", Label: "选择方式", Kind: "enum", Options: [][2]string{{"node", "指定一个节点"}, {"filter", "按名称筛选一组，由内核自动选择"}}},
		{Key: "match", Label: "节点名称或筛选", Kind: "string", Hint: "指定节点时填完整名称；筛选时填正则"},
	}
}

func typeExitBody(args map[string]any) map[string]any {
	exit := map[string]any{}
	for _, k := range []string{"kind", "address", "port", "username", "password", "link", "node_id", "profile_id", "mode", "match"} {
		if v, ok := args[k]; ok {
			exit[k] = v
		}
	}
	return map[string]any{"exit": exit}
}

var proxyDraftCaps = []Capability{
	{Name: "proxy.update", Title: "修改代理设置", Purpose: "修改一个用户代理页的设置：哪些类型不放进订阅、按网站类别分流到哪一组、自动选择（客户端测延迟 / 按速率排序）、放几个、节点列表刷新间隔。没给的字段保持原样；客户端下次更新订阅时生效。改前先读 get_proxy。",
		Kind: "draft", Risk: "normal", Object: "user", Params: proxyParams(),
		op: &opSpec{kind: "update", method: "PUT", handler: func(s *Server) gin.HandlerFunc { return func(c *gin.Context) { s.putProxy(c, true) } }, path: userPath, pathKeys: []string{"user_id"},
			self: "user_id", load: loadProxy, full: true, body: proxyBody}},
	{Name: "proxy.type_exit", Title: "给类型加出口", Purpose: "给一个用户某个类型的全部节点统一加同一个出口：每个节点生成一条链式代理，由入口 VPS 逐个实测，有一个不通就整体不保存。已有出口时换成新的。需要这个用户开放了链式代理。不能撤回（要去掉用 proxy.type_exit_delete），只能是最后一步。",
		Kind: "draft", Risk: "normal", Object: "user", Last: true, Params: typeExitParams(),
		op: &opSpec{kind: "action", method: "PUT", handler: func(s *Server) gin.HandlerFunc { return func(c *gin.Context) { s.putTypeExit(c, true) } }, path: typeExitPath, pathKeys: []string{"user_id", "type_key"},
			self: "user_id", body: typeExitBody, probe: true, needs: func(args map[string]any) []string {
				if args["kind"] == "link" {
					return []string{"link"}
				}
				return nil
			}}},
	{Name: "proxy.type_exit_delete", Title: "去掉类型出口", Purpose: "去掉一个用户某个类型的出口：这类节点恢复直接从 VPS 出去，经出口的那条从订阅里拿掉。不能撤回，只能是最后一步。",
		Kind: "draft", Risk: "normal", Object: "user", Last: true,
		Params: []Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true}, {Key: "type_key", Label: "类型", Kind: "string", Required: true, Hint: "类型的 key（见 get_proxy 的 types）"}},
		op:     &opSpec{kind: "delete", method: "DELETE", handler: func(s *Server) gin.HandlerFunc { return func(c *gin.Context) { s.deleteTypeExit(c, true) } }, path: typeExitPath, pathKeys: []string{"user_id", "type_key"}, self: "user_id"}},
}

var proxyReadCaps = []Capability{
	{Name: "get_proxy", Title: "代理设置与速率", Purpose: "读取一个用户代理页的内容：节点按类型（协议方案）归类，每类是否放进订阅、可用节点数、统一出口（密码与分享链接不给）、还没加出口的节点；分流规则（每个类别现在走哪一组）；自动选择设置；此刻的排序——每个节点的状况、下载速率（rate.p90_bps 是近 24 小时有流量的分钟的第 90 百分位，字节/秒；minutes 为 0 是没有数据，不是 0；from 是 self 自己的或 all 全部用户的）、是否在自动选择里、是不是轮换位。",
		Kind: "read", Params: []Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			u, err := s.Store.Ent.User.Get(ctx, intArg(args, "user_id"))
			if err != nil {
				return nil, errors.New("用户不存在")
			}
			v, err := s.proxyView(ctx, u)
			if err != nil {
				return nil, err
			}
			return toModel(v), nil
		}},
	{Name: "get_link_stats", Title: "订阅拉取统计", Purpose: "读取订阅链接最近 30 天的拉取统计：每条链接的状态（active 生效中 / not_started 未开始 / expired 已结束）、生效时间、已用流量、近 24 小时与 30 天的拉取次数、不在生效时间内被拒的次数、来源个数、按客户端分的次数、最近一次拉取的时间 / 客户端 / 格式 / 结果。不含来源 IP、位置与 User-Agent。不给用户时是全部用户。",
		Kind: "read", Params: []Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user"}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			uid := 0
			if args["user_id"] != nil {
				uid = intArg(args, "user_id")
				if _, err := s.Store.Ent.User.Get(ctx, uid); err != nil {
					return nil, errors.New("用户不存在")
				}
			}
			items, err := s.collectLinkStats(ctx, uid, false)
			if err != nil {
				return nil, err
			}
			return toModel(map[string]any{"items": items, "keep_days": int(pullKeep.Hours() / 24)}), nil
		}},
}
