package center

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/audit"
	"github.com/imzyb/MiGate/packages/contracts"
)

func (s *Server) opsRoutes(admin *gin.RouterGroup) {
	admin.GET("/audit", s.listAudit)
	admin.POST("/gates/:id/kernels/:kernel/diagnostics", s.gateKernelAction(contracts.ActionDiagnostics))
	admin.POST("/gates/:id/kernels/:kernel/restart", s.gateKernelAction(contracts.ActionCoreRestart))
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

// gateKernelAction 让 VPS 执行白名单动作：读取内核诊断（分类计数与遮盖 IP 后的最近日志）或重启内核。
func (s *Server) gateKernelAction(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		g, ok := s.gateParam(c)
		if !ok {
			return
		}
		kernel := c.Param("kernel")
		if kernel != "xray" && kernel != "sing-box" {
			failure(c, 404, "kernel_not_found")
			return
		}
		res, err := s.gateProbe(c.Request.Context(), g, kind, map[string]any{"kernel": kernel}, 30*time.Second)
		if err != nil {
			failErr(c, 422, err)
			return
		}
		if kind == contracts.ActionCoreRestart {
			s.audit(c, "gate.kernel.restart", "gate:"+itoa(g.ID)+":"+kernel, "ok")
		}
		c.JSON(200, gin.H{"result": res})
	}
}
