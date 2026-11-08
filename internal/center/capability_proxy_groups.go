package center

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"strings"
)

func proxyGroupParams(existing bool) []Param {
	ps := []Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true}}
	if existing {
		ps = append(ps, Param{Key: "group_id", Label: "分组编号", Kind: "int", Required: true, Min: num(1), Hint: "该用户内的稳定编号，见 get_proxy.groups"})
	}
	return append(ps, Param{Key: "name", Label: "名称", Kind: "string", Required: !existing},
		Param{Key: "revision", Label: "并发版本", Kind: "int", Required: true},
		Param{Key: "kind", Label: "方式", Kind: "enum", Options: [][2]string{{"fallback", "掉线自动切换"}, {"url-test", "选延迟最低的"}, {"select", "自己选择"}}},
		Param{Key: "members", Label: "有序线路", Kind: "string", Required: !existing, Hint: "用英文逗号分隔 node:编号、chain:编号，只能引用已授权且启用的具体线路；不能引用其他组；1 至 50 个，主用在前"},
		Param{Key: "tolerance_ms", Label: "延迟容差", Kind: "int", Min: num(0), Max: num(10000), Unit: "毫秒", Hint: "默认50，仅延迟最低使用"},
		Param{Key: "interval_seconds", Label: "探测间隔", Kind: "int", Min: num(30), Max: num(86400), Unit: "秒", Hint: "默认 60 秒"})
}

func proxyGroupPath(_ context.Context, _ *Server, args map[string]any) (gin.Params, error) {
	return pathOf("uid", itoa(numberOf(args["user_id"])), "group", itoa(numberOf(args["group_id"]))), nil
}
func proxyGroupBody(args map[string]any) map[string]any {
	body := map[string]any{}
	for _, k := range []string{"name", "revision", "kind", "tolerance_ms", "interval_seconds"} {
		if v, ok := args[k]; ok {
			body[k] = v
		}
	}
	if raw, ok := args["members"].(string); ok {
		members := []string{}
		for _, m := range strings.Split(raw, ",") {
			if m = strings.TrimSpace(m); m != "" {
				members = append(members, m)
			}
		}
		body["members"] = members
	}
	return body
}
func loadProxyGroup(ctx context.Context, s *Server, args map[string]any) (map[string]any, error) {
	u, err := s.Store.Ent.User.Get(ctx, numberOf(args["user_id"]))
	if err != nil {
		return nil, err
	}
	for _, g := range loadPolicy(u).Groups {
		if g.ID == numberOf(args["group_id"]) {
			return map[string]any{"name": g.Name, "revision": loadPolicy(u).Revision, "kind": g.Kind, "members": strings.Join(g.Members, ","), "tolerance_ms": g.ToleranceMS, "interval_seconds": g.IntervalSeconds}, nil
		}
	}
	return nil, errors.New("分组不存在")
}

var _ = func() bool {
	opDraftCaps = append(opDraftCaps,
		Capability{Name: "proxy.group_create", Title: "新建我的分组", Purpose: "为用户新建分组，最多 20 个。成员只能是已授权且启用的 node / chain 具体线路，不能引用其他组；默认延迟最低，主备切换时按成员顺序使用首个可用线路。先读 get_proxy。", Kind: "draft", Risk: "normal", Object: "user", Params: proxyGroupParams(false), op: &opSpec{kind: "create", method: "POST", handler: func(s *Server) gin.HandlerFunc { return func(c *gin.Context) { s.writeProxyGroup(c, true) } }, path: userPath, pathKeys: []string{"user_id"}, body: proxyGroupBody, undo: "proxy.group_delete", idKey: "group_id"}},
		Capability{Name: "proxy.group_update", Title: "修改我的分组", Purpose: "修改用户自己的分组及主备顺序；成员只能是具体 node / chain 线路，不能引用组，未给的字段保留。", Kind: "draft", Risk: "normal", Object: "user", Params: proxyGroupParams(true), op: &opSpec{kind: "update", method: "PUT", handler: func(s *Server) gin.HandlerFunc { return func(c *gin.Context) { s.writeProxyGroup(c, true) } }, path: proxyGroupPath, pathKeys: []string{"user_id", "group_id"}, self: "user_id", load: loadProxyGroup, full: true, body: proxyGroupBody}},
		Capability{Name: "proxy.group_delete", Title: "删除我的分组", Purpose: "删除用户的分组；成员恢复原有单独显示偏好。只能作为最后一步。", Kind: "draft", Risk: "normal", Object: "user", Last: true, Params: []Param{{Key: "revision", Label: "并发版本", Kind: "int", Required: true}, {Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true}, {Key: "group_id", Label: "分组编号", Kind: "int", Required: true, Min: num(1)}}, op: &opSpec{kind: "delete", method: "DELETE", handler: func(s *Server) gin.HandlerFunc { return func(c *gin.Context) { s.writeProxyGroup(c, true) } }, path: proxyGroupPath, pathKeys: []string{"user_id", "group_id"}, self: "user_id", body: proxyGroupBody}})
	return true
}()
