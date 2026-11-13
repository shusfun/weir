package center

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

func (s *Server) maintainLayaHosting(ctx context.Context, now time.Time) error {
	doc, err := s.retentionPolicy(ctx)
	if err != nil {
		return err
	}
	return s.Store.Lock.Do(ctx, "laya-host-retention", func(ctx context.Context) error {
		rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(layaOperationKind), resource.CreatedAtLT(now.Add(-31*time.Minute)), func(selector *entsql.Selector) {
			selector.Where(sqljson.ValueEQ(resource.FieldData, "running", sqljson.Path("status")))
		}).Order(ent.Asc(resource.FieldID)).Limit(100).All(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var op layaHostOperation
			if err := decodeStrict(row.Data, &op); err != nil {
				return err
			}
			if op.Deadline.After(now.Add(-time.Minute)) {
				continue
			}
			op.Status, op.Code = "unknown", "laya_operation_interrupted"
			tx, err := s.Store.Ent.Tx(ctx)
			if err != nil {
				return err
			}
			if _, err = tx.Resource.UpdateOneID(row.ID).SetData(toMap(op)).Save(ctx); err == nil {
				_, err = tx.Audit.Create().SetActor(op.Actor).SetAction("laya.host.interrupted").SetTarget(op.ID).SetResult("unknown").SetDetail(map[string]any{"trace_id": op.TraceID, "code": op.Code, "last_stage": op.Stage}).Save(ctx)
			}
			if err != nil {
				_ = tx.Rollback()
				return err
			}
			if err = tx.Commit(); err != nil {
				return err
			}
		}
		terminal := func(sel *entsql.Selector) {
			sel.Where(sqljson.ValueIn(resource.FieldData, []any{"succeeded", "failed", "unknown"}, sqljson.Path("status")))
		}
		if err = s.pruneResources(ctx, resource.KindEQ(layaOperationKind), terminal, resource.CreatedAtLT(now.Add(-time.Duration(doc.Policy.LayaDays)*24*time.Hour))); err != nil {
			return err
		}
		last, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(layaOperationKind), terminal).Order(ent.Desc(resource.FieldID)).Offset(doc.Policy.MaxRecords).First(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return s.pruneResources(ctx, resource.KindEQ(layaOperationKind), terminal, resource.IDLTE(last.ID), resource.CreatedAtLT(now.Add(-time.Hour)))
	})
}
