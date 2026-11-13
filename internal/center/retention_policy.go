package center

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/packages/contracts"
)

const retentionPolicyKey = "retention-policy"

func defaultRetentionPolicy() contracts.RetentionPolicy {
	return contracts.RetentionPolicy{ObservationDays: 14, TaskDays: 14, LayaDays: 14, SampleHours: 25, PullDays: 30, AuditDays: 0, MaxRecords: 1_000_000}
}

func validateRetentionPolicy(p contracts.RetentionPolicy) error {
	if p.ObservationDays < 1 || p.ObservationDays > 90 || p.TaskDays < 1 || p.TaskDays > 90 || p.LayaDays < 1 || p.LayaDays > 90 || p.SampleHours < 25 || p.SampleHours > 2160 || p.PullDays < 30 || p.PullDays > 90 || p.AuditDays != 0 && (p.AuditDays < 30 || p.AuditDays > 3650) || p.MaxRecords < 100 || p.MaxRecords > 1_000_000 {
		return errors.New("invalid retention policy limits")
	}
	return nil
}

func (s *Server) retentionPolicy(ctx context.Context) (contracts.RetentionDocument, error) {
	p := defaultRetentionPolicy()
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(retentionPolicyKey)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return contracts.RetentionDocument{}, err
	}
	if err == nil {
		if err = decodeStrict(row.Value, &p); err != nil {
			return contracts.RetentionDocument{}, err
		}
	}
	if err = validateRetentionPolicy(p); err != nil {
		return contracts.RetentionDocument{}, err
	}
	raw, _ := json.Marshal(p)
	return contracts.RetentionDocument{Policy: p, Revision: digest(string(raw))}, nil
}

var errRetentionConflict = errors.New("retention policy changed")

func (s *Server) retentionPolicyRoutes(admin *gin.RouterGroup) {
	admin.GET("/settings/retention", func(c *gin.Context) {
		doc, err := s.retentionPolicy(c.Request.Context())
		if err != nil {
			failInternal(c, 503, "retention_policy_unavailable", err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(200, doc)
	})
	admin.PUT("/settings/retention", func(c *gin.Context) {
		var req contracts.RetentionDocument
		if !bindWorker(c, &req, 4096) {
			return
		}
		if err := validateRetentionPolicy(req.Policy); err != nil {
			if req.Policy.AuditDays != 0 && (req.Policy.AuditDays < 30 || req.Policy.AuditDays > 3650) {
				failure(c, 422, "retention_audit_days_invalid")
				return
			}
			failure(c, 422, "retention_policy_invalid")
			return
		}
		err := s.Store.Lock.Do(c.Request.Context(), retentionPolicyKey, func(ctx context.Context) error {
			old, err := s.retentionPolicy(ctx)
			if err != nil {
				return err
			}
			if old.Revision != req.Revision {
				return errRetentionConflict
			}
			tx, err := s.Store.Ent.Tx(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err = tx.Setting.Create().SetKey(retentionPolicyKey).SetValue(toMap(req.Policy)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
				return err
			}
			if err = tx.Audit.Create().SetActor(c.GetInt("user_id")).SetAction("settings.retention").SetTarget(retentionPolicyKey).SetResult("ok").SetDetail(map[string]any{"trace_id": c.GetString("trace_id"), "before": old.Policy, "after": req.Policy}).Exec(ctx); err != nil {
				return err
			}
			return tx.Commit()
		})
		if errors.Is(err, errRetentionConflict) {
			failure(c, 409, "retention_policy_conflict")
			return
		}
		if err != nil {
			failInternal(c, 503, "save_failed", err)
			return
		}
		raw, _ := json.Marshal(req.Policy)
		req.Revision = digest(string(raw))
		c.JSON(200, req)
	})
}

func (s *Server) pruneAudit(ctx context.Context, now time.Time, p contracts.RetentionPolicy) error {
	if p.AuditDays == 0 {
		return nil
	}
	cutoff := now.Add(-time.Duration(p.AuditDays) * 24 * time.Hour)
	ids, err := s.Store.Ent.Audit.Query().Where(audit.CreatedAtLT(cutoff)).Order(ent.Asc(audit.FieldID)).Limit(s.Config.Store.Maintenance.Effective().RecordBatch).IDs(ctx)
	if err == nil && len(ids) > 0 {
		_, err = s.Store.Ent.Audit.Delete().Where(audit.IDIn(ids...), audit.CreatedAtLT(cutoff)).Exec(ctx)
	}
	return err
}
