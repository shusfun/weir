package center

import (
	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/predicate"
	"strconv"
	"strings"
	"time"
)

func hasRestart(action, result string) bool {
	return strings.HasSuffix(action, ".restart") || strings.Contains(result, "restart") || strings.Contains(result, "重启")
}
func hasAIPrefix(action string) bool { return strings.HasPrefix(action, "ai.") }
func auditFailure(result string) bool {
	code, _, _ := strings.Cut(result, " ")
	return code == "failed" || code == "denied"
}

func auditRestartPredicate() predicate.Audit {
	return audit.Or(audit.ActionHasSuffix(".restart"), audit.ResultContains("restart"), audit.ResultContains("重启"))
}

// Bounded time windows and fixed buckets keep memory independent of record count.
func (s *Server) auditSummary(c *gin.Context) {
	days, err := strconv.Atoi(c.DefaultQuery("days", "7"))
	today, e := time.Parse(time.RFC3339, c.Query("today_from"))
	now := time.Now()
	if err != nil || days != 1 && days != 7 && days != 30 || e != nil || today.After(now) || now.Sub(today) > 25*time.Hour {
		failure(c, 400, "invalid_query")
		return
	}
	start := now.Add(-time.Duration(days) * 24 * time.Hour)
	cells := days * 24
	if days == 30 {
		cells = 180
	}
	span := now.Sub(start) / time.Duration(cells)
	type bucket struct {
		At       time.Time `json:"at"`
		Total    int       `json:"total"`
		Restarts int       `json:"restarts"`
	}
	buckets := make([]bucket, cells)
	for i := range buckets {
		buckets[i].At = start.Add(time.Duration(i) * span)
	}
	totals := gin.H{"human": 0, "automatic": 0, "failed": 0, "restarts": 0}
	cursor := 0
	for {
		rows, err := s.Store.Ent.Audit.Query().Where(audit.IDGT(cursor), audit.CreatedAtGTE(start), audit.CreatedAtLTE(now)).Order(ent.Asc(audit.FieldID)).Limit(1000).All(c.Request.Context())
		if err != nil {
			failInternal(c, 503, "query_failed", err)
			return
		}
		for _, a := range rows {
			cursor = a.ID
			restart := a.Action == "gate.kernel.restart" || hasRestart(a.Action, a.Result)
			i := int(a.CreatedAt.Sub(start) / span)
			if i >= cells {
				i = cells - 1
			}
			if i >= 0 {
				buckets[i].Total++
				if restart {
					buckets[i].Restarts++
				}
			}
			if !a.CreatedAt.Before(today) {
				k := "automatic"
				if a.Actor > 0 || hasAIPrefix(a.Action) {
					k = "human"
				}
				totals[k] = totals[k].(int) + 1
				if auditFailure(a.Result) {
					totals["failed"] = totals["failed"].(int) + 1
				}
				if restart {
					totals["restarts"] = totals["restarts"].(int) + 1
				}
			}
		}
		if len(rows) < 1000 {
			break
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"from": start, "to": now, "buckets": buckets, "today": totals})
}
