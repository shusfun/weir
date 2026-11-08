package center

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

func candidatePath(_ context.Context, _ *Server, args map[string]any) (gin.Params, error) {
	id, _ := args["candidate_id"].(string)
	if id == "" || len(id) > 80 || strings.ContainsAny(id, "/\\") {
		return nil, errors.New("候选编号无效")
	}
	return pathOf("candidate", id), nil
}

var _ = func() bool {
	params := []Param{{Key: "candidate_id", Label: "候选编号", Kind: "string", Required: true}, {Key: "revision", Label: "并发版本", Kind: "int", Required: true}}
	opDraftCaps = append(opDraftCaps,
		Capability{Name: "proxy.migration_ack", Title: "确认代理迁移摘要", Purpose: "确认已经阅读用户的代理设置迁移摘要，之后不再提示。先读 get_proxy，使用当前并发版本。", Kind: "draft", Risk: "normal", Object: "user", Last: true,
			Params: []Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true}, {Key: "revision", Label: "并发版本", Kind: "int", Required: true}},
			op:     &opSpec{kind: "action", method: "POST", path: userPath, pathKeys: []string{"user_id"}, handler: func(s *Server) gin.HandlerFunc { return func(c *gin.Context) { s.ackProxyMigration(c, true) } }}},
		Capability{Name: "proxy.exit_retry", Title: "重试候选出口", Purpose: "重新验证已保存的候选出口，通过后自动应用。使用 get_pending_exits 返回的编号和并发版本；已过期、已应用或已放弃的候选不能重试。", Kind: "draft", Risk: "normal", Last: true, Params: params,
			op: &opSpec{kind: "action", method: "POST", path: candidatePath, pathKeys: []string{"candidate_id"}, handler: func(s *Server) gin.HandlerFunc { return s.retryPendingExit }}},
		Capability{Name: "proxy.exit_discard", Title: "放弃候选出口", Purpose: "放弃尚未生效的候选并回收秘密，保留当前出口。使用 get_pending_exits 返回的编号和并发版本。", Kind: "draft", Risk: "normal", Last: true, Params: params,
			op: &opSpec{kind: "action", method: "DELETE", path: candidatePath, pathKeys: []string{"candidate_id"}, handler: func(s *Server) gin.HandlerFunc { return s.discardPendingExit }}},
	)
	opReadCaps = append(opReadCaps, Capability{Name: "get_pending_exits", Title: "查看出口验证任务", Purpose: "读取用户的出口验证任务、失败原因和并发版本。只返回脱敏状态，不返回候选请求或凭据。", Kind: "read", Params: []Param{{Key: "user_id", Label: "用户", Kind: "ref", Ref: "user", Required: true}},
		read: func(ctx context.Context, s *Server, args map[string]any) (any, error) {
			actor, err := s.systemActor(ctx)
			if err != nil {
				return nil, err
			}
			code, result := call(actor, func(c *gin.Context) {
				c.Request.URL.RawQuery = "user=" + itoa(numberOf(args["user_id"]))
				s.listPendingExits(c)
			}, "GET", nil, nil, nil, false)
			if code >= 300 {
				return nil, handlerError(code, result)
			}
			return modelSafe(result), nil
		}})
	return true
}()
