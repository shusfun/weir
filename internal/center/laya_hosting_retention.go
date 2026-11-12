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
		if _, err = s.Store.Ent.Resource.Delete().Where(resource.KindEQ(layaOperationKind), resource.CreatedAtLT(now.Add(-14*24*time.Hour))).Exec(ctx); err != nil {
			return err
		}
		last, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(layaOperationKind)).Order(ent.Desc(resource.FieldID)).Offset(1_000_000).First(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = s.Store.Ent.Resource.Delete().Where(resource.KindEQ(layaOperationKind), resource.IDLTE(last.ID), resource.CreatedAtLT(now.Add(-time.Hour))).Exec(ctx)
		return err
	})
}
