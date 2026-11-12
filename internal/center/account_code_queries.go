package center

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/accountplan"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/accesscode"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

// The response is an explicit allowlist: stored code material and issuance
// traces must never become visible through a list or personal history.
type accountCodeView struct {
	ID              int               `json:"id"`
	Kind            accesscode.Kind   `json:"kind"`
	PlanID          *int              `json:"plan_id"`
	PlanRevision    int               `json:"plan_revision"`
	Grant           accountplan.Grant `json:"grant"`
	Status          string            `json:"status"`
	ExpiresAt       *time.Time        `json:"expires_at"`
	UsedAt          *time.Time        `json:"used_at"`
	UsedBy          *int              `json:"used_by,omitempty"`
	Revoked         bool              `json:"revoked"`
	CreatedAt       time.Time         `json:"created_at"`
	Synchronization string            `json:"synchronization,omitempty"`
}

func accountCodeStatus(r *ent.AccessCode, now time.Time) string {
	switch {
	case r.UsedAt != nil:
		return "used"
	case r.Revoked:
		return "revoked"
	case r.ExpiresAt != nil && !r.ExpiresAt.After(now):
		return "expired"
	default:
		return "unused"
	}
}

func accountCodeDTO(r *ent.AccessCode, now time.Time) accountCodeView {
	return accountCodeView{ID: r.ID, Kind: r.Kind, PlanID: r.PlanID, PlanRevision: r.PlanRevision, Grant: r.Grant, Status: accountCodeStatus(r, now), ExpiresAt: r.ExpiresAt, UsedAt: r.UsedAt, UsedBy: r.UsedBy, Revoked: r.Revoked, CreatedAt: r.CreatedAt}
}

func accountCodePage(c *gin.Context) (int, bool) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 1000000 {
		failure(c, 400, "invalid_page")
		return 0, false
	}
	return page, true
}

func (s *Server) listAccessCodes(c *gin.Context) {
	page, ok := accountCodePage(c)
	if !ok {
		return
	}
	q := s.Store.Ent.AccessCode.Query()
	for _, filter := range []struct {
		key   string
		apply func(int)
	}{
		{"id", func(id int) { q.Where(accesscode.IDEQ(id)) }},
		{"plan_id", func(id int) { q.Where(accesscode.PlanIDEQ(id)) }},
	} {
		if raw := c.Query(filter.key); raw != "" {
			id, err := strconv.Atoi(raw)
			if err != nil || id < 1 {
				failure(c, 400, "invalid_id")
				return
			}
			filter.apply(id)
		}
	}
	if kind := c.Query("kind"); kind != "" {
		if !catalogChoice(kind, "invite", "redeem") {
			failure(c, 400, "invalid_kind")
			return
		}
		q.Where(accesscode.KindEQ(accesscode.Kind(kind)))
	}
	now := time.Now()
	switch c.Query("status") {
	case "":
	case "used":
		q.Where(accesscode.UsedAtNotNil())
	case "revoked":
		q.Where(accesscode.UsedAtIsNil(), accesscode.RevokedEQ(true))
	case "expired":
		q.Where(accesscode.UsedAtIsNil(), accesscode.RevokedEQ(false), accesscode.ExpiresAtLTE(now))
	case "unused":
		q.Where(accesscode.UsedAtIsNil(), accesscode.RevokedEQ(false), accesscode.Or(accesscode.ExpiresAtIsNil(), accesscode.ExpiresAtGT(now)))
	default:
		failure(c, 400, "invalid_status")
		return
	}
	s.respondAccountCodes(c, q, page, false, now)
}

func (s *Server) listRedemptions(c *gin.Context) {
	page, ok := accountCodePage(c)
	if !ok {
		return
	}
	q := s.Store.Ent.AccessCode.Query().Where(accesscode.KindEQ(accesscode.KindRedeem), accesscode.UsedByEQ(c.GetInt("user_id")), accesscode.UsedAtNotNil())
	s.respondAccountCodes(c, q, page, true, time.Now())
}

func (s *Server) respondAccountCodes(c *gin.Context, q *ent.AccessCodeQuery, page int, personal bool, now time.Time) {
	ctx := c.Request.Context()
	started := time.Now()
	total, err := q.Clone().Count(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	if personal {
		q.Order(ent.Desc(accesscode.FieldUsedAt), ent.Desc(accesscode.FieldID))
	} else {
		q.Order(ent.Desc(accesscode.FieldID))
	}
	rows, err := q.Offset((page - 1) * 50).Limit(50).All(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	sync, err := s.accountCodeSynchronization(ctx, rows)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	items := make([]accountCodeView, 0, len(rows))
	for _, row := range rows {
		v := accountCodeDTO(row, now)
		v.Synchronization = sync[row.ID]
		if personal {
			v.UsedBy = nil
		}
		items = append(items, v)
	}
	slog.InfoContext(ctx, "account codes queried", "trace", taskTrace(ctx), "user", c.GetInt("user_id"), "personal", personal, "count", len(items), "ms", time.Since(started).Milliseconds())
	c.JSON(200, gin.H{"items": items, "total": total, "page": page})
}

func (s *Server) accountCodeSynchronization(ctx context.Context, rows []*ent.AccessCode) (map[int]string, error) {
	states := map[int]string{}
	keys := []string{}
	values := []any{}
	for _, r := range rows {
		if r.UsedAt != nil {
			states[r.ID] = "unknown"
			keys = append(keys, itoa(r.ID))
			values = append(values, itoa(r.ID))
		}
	}
	if len(keys) == 0 {
		return states, nil
	}
	// Successful reconciliation removes its marker. Absence alone is not proof
	// of completion for older data; a successful audit supplies that evidence.
	events, err := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("account.grant.sync"), audit.ResultEQ("ok"), func(q *sql.Selector) { q.Where(sqljson.ValueIn(audit.FieldDetail, values, sqljson.Path("code_id"))) }).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range events {
		if key, ok := a.Detail["code_id"].(string); ok {
			id, _ := strconv.Atoi(key)
			states[id] = "ok"
		}
	}
	markers, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(accountGrantSyncKind), resource.KeyIn(keys...)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range markers {
		id, _ := strconv.Atoi(r.Key)
		states[id] = "pending"
	}
	return states, nil
}
