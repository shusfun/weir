package center

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/audit"
)

func (s *Server) opsRoutes(admin *gin.RouterGroup) {
	admin.GET("/audit", s.listAudit)
}

// listAudit 分页查询操作审计；q 匹配操作名或目标，result 按结果筛选。审计只记录动作与目标，不含任何凭据。
func (s *Server) listAudit(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	q := s.Store.Ent.Audit.Query()
	if v := strings.TrimSpace(c.Query("q")); v != "" {
		if len(v) > 100 {
			failure(c, 400, "invalid_query")
			return
		}
		q = q.Where(audit.Or(audit.ActionContains(v), audit.TargetContains(v)))
	}
	if v := c.Query("result"); v != "" {
		q = q.Where(audit.ResultEQ(v))
	}
	ctx := c.Request.Context()
	total, err := q.Clone().Count(ctx)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	items, err := q.Order(ent.Desc(audit.FieldID)).Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, a := range items {
		out = append(out, gin.H{"id": a.ID, "actor": a.Actor, "action": a.Action, "target": a.Target, "result": a.Result, "ip": a.IP, "created_at": a.CreatedAt})
	}
	c.JSON(200, gin.H{"items": out, "total": total, "page": page, "page_size": size})
}
