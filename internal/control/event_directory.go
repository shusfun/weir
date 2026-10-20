package control

import (
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
)

func (s *Server) modelEvents(c *gin.Context) {
	query := s.Store.Resource.Query().Where(resource.KindIn("model-event", "security-event"))
	if kind := c.Query("kind"); kind != "" {
		if kind != "model-event" && kind != "security-event" {
			failure(c, 400, "invalid_event_kind")
			return
		}
		query.Where(resource.KindEQ(kind))
	}
	for _, key := range []string{"status", "severity", "device_id"} {
		value := c.Query(key)
		if value == "" {
			continue
		}
		if len(value) > 80 {
			failure(c, 400, "invalid_event_filter")
			return
		}
		if key == "device_id" {
			id, err := strconv.Atoi(value)
			if err != nil || id <= 0 {
				failure(c, 400, "invalid_device")
				return
			}
		}
		// Both key and expression structure are fixed; the filter value is bound.
		query.Where(func(selector *sql.Selector) {
			selector.Where(sql.P(func(b *sql.Builder) { b.WriteString(`COALESCE("data"->>'` + key + `','') = `).Arg(value) }))
		})
	}
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		if len(q) > 200 {
			failure(c, 400, "invalid_query")
			return
		}
		q = "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
		query.Where(func(selector *sql.Selector) {
			selector.Where(sql.P(func(b *sql.Builder) {
				b.WriteString(`LOWER(COALESCE("data"->>'category','') || ' ' || COALESCE("data"->>'reason','') || ' ' || COALESCE("data"->>'action','')) LIKE `).Arg(q)
			}))
		})
	}
	page, size := 1, 200
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
		failure(c, 503, "observation_unavailable")
		return
	}
	items, err := query.Order(ent.Desc("id")).Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		failure(c, 503, "observation_unavailable")
		return
	}
	views := []gin.H{}
	now := time.Now()
	for _, item := range items {
		data := map[string]any{}
		for _, key := range []string{"audit_id", "device_id", "certificate_id", "not_before", "not_after", "remaining_seconds", "last_seen", "observed_at", "category", "status", "severity", "reason", "count", "window_seconds", "action", "model", "error", "decision"} {
			if v, ok := item.Data[key]; ok {
				data[key] = v
			}
		}
		source, validity := "center.rules", "unknown"
		observed, parseErr := time.Parse(time.RFC3339, text(data["observed_at"]))
		if parseErr == nil && !observed.After(now.Add(5*time.Second)) {
			validity = "fresh"
			if now.Sub(observed) > 60*time.Second {
				validity = "stale"
			}
		}
		if item.Kind == "model-event" {
			source = "remote.laya"
			if validity != "unknown" {
				validity = "snapshot"
			}
		}
		views = append(views, gin.H{"id": item.ID, "kind": item.Kind, "key": item.Key, "created_at": item.CreatedAt, "data": data, "source": source, "validity": validity, "observed_at": data["observed_at"]})
	}
	c.JSON(200, gin.H{"items": views, "total": total, "page": page, "page_size": size, "queried_at": now.UTC().Format(time.RFC3339)})
}
