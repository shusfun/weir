package control

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/audit"
)

func (s *Server) listAudit(c *gin.Context) {
	query := s.Store.Audit.Query()
	q := strings.TrimSpace(c.Query("q"))
	if len(q) > 200 {
		failure(c, 400, "invalid_query")
		return
	}
	if q != "" {
		query.Where(audit.Or(audit.ActionContainsFold(q), audit.TargetContainsFold(q)))
	}
	if value := c.Query("actor"); value != "" {
		id, err := strconv.Atoi(value)
		if err != nil || id < 0 {
			failure(c, 400, "invalid_actor")
			return
		}
		query.Where(audit.ActorEQ(id))
	}
	if value := c.Query("result"); value != "" {
		if len(value) > 80 {
			failure(c, 400, "invalid_result")
			return
		}
		query.Where(audit.ResultEQ(value))
	}
	page, size := 1, 200 // Keep the legacy unpaginated contract.
	if c.Query("page") != "" || c.Query("page_size") != "" {
		var err error
		page, err = strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 1000000 {
			failure(c, 400, "invalid_page")
			return
		}
		size, err = strconv.Atoi(c.DefaultQuery("page_size", "20"))
		if err != nil || size < 1 || size > 200 {
			failure(c, 400, "invalid_page_size")
			return
		}
	}
	ctx := c.Request.Context()
	total, err := query.Clone().Count(ctx)
	if err != nil {
		failure(c, 503, "audit_unavailable")
		return
	}
	items, err := query.Order(ent.Desc("id")).Limit(size).Offset((page - 1) * size).All(ctx)
	if err != nil {
		failure(c, 503, "audit_unavailable")
		return
	}
	c.JSON(200, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}
