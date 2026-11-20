package taskqueue

import (
	"context"
	"errors"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/predicate"
	"github.com/shusfun/weir/internal/store/ent/workeractivity"
	"github.com/shusfun/weir/internal/store/ent/workerattempt"
	"github.com/shusfun/weir/internal/store/ent/workercommunication"
	"github.com/shusfun/weir/internal/store/ent/workertask"
)

// PruneObservations bounds worker history. Active work is never removed.
// Each pass removes bounded batches so cleanup cannot monopolize the database.
func (q *Queue) PruneObservations(ctx context.Context, now time.Time, retention time.Duration, maxRecords int) error {
	if retention < time.Hour || retention > 90*24*time.Hour || maxRecords < 100 || maxRecords > 1_000_000 {
		return errors.New("invalid worker retention policy")
	}
	batch := q.PruneBatch
	if batch == 0 {
		batch = 2000
	}
	if batch < 1 || batch > 10000 {
		return errors.New("invalid worker maintenance batch")
	}
	cutoff := now.Add(-retention).UnixMilli()
	return q.Lock.Do(ctx, "worker-retention", func(ctx context.Context) error {
		return q.withTx(ctx, func(tx *ent.Tx) error {
			// 先按时间删除过期记录，再按条数上限删除最旧的记录，每步最多 batch 行。
			for _, w := range []window{{cutoff: cutoff}, {offset: maxRecords}} {
				if _, err := tx.WorkerActivity.Delete().Where(pruneActivity(w, batch)).Exec(ctx); err != nil {
					return err
				}
				if _, err := tx.WorkerCommunication.Delete().Where(pruneCommunication(w, batch)).Exec(ctx); err != nil {
					return err
				}
				if _, err := tx.WorkerTask.Delete().Where(pruneTasks(w, batch)).Exec(ctx); err != nil {
					return err
				}
			}
			_, err := tx.WorkerAttempt.Delete().Where(pruneOrphanAttempts(batch)).Exec(ctx)
			return err
		})
	})
}

// window 选出一批待删除的行：cutoff 非 0 时按时间升序删早于它的记录；
// 否则按时间降序保留最新的 offset 条，删其后的记录。
type window struct {
	cutoff int64
	offset int
}

// limited 给子查询加时间条件、排序与偏移。byTime 是时间列，keys 是用于稳定排序的主键列（已带方向）。
func (w window) limited(sub *entsql.Selector, byTime string, keys ...string) *entsql.Selector {
	if w.cutoff != 0 {
		return sub.Where(entsql.LT(byTime, w.cutoff)).OrderBy(append([]string{byTime}, keys...)...)
	}
	return sub.OrderBy(append([]string{entsql.Desc(byTime)}, keys...)...).Offset(w.offset)
}

func pruneActivity(w window, batch int) predicate.WorkerActivity {
	return func(s *entsql.Selector) {
		d := entsql.Dialect(s.Dialect())
		r := d.Table(workeractivity.Table).As("r")
		id := r.C(workeractivity.FieldID)
		order := id
		if w.cutoff == 0 {
			order = entsql.Desc(id)
		}
		sub := w.limited(d.Select(id).From(r), r.C(workeractivity.FieldAt), order).Limit(batch)
		s.Where(entsql.In(s.C(workeractivity.FieldID), sub))
	}
}

func pruneCommunication(w window, batch int) predicate.WorkerCommunication {
	return func(s *entsql.Selector) {
		d := entsql.Dialect(s.Dialect())
		r := d.Table(workercommunication.Table).As("r")
		keys := []string{r.C(workercommunication.FieldID), r.C(workercommunication.FieldGateID), r.C(workercommunication.FieldRoute)}
		sub := w.limited(d.Select(keys...).From(r), keys[0], keys[1:]...).Limit(batch)
		s.Where(tupleIn([]string{s.C(workercommunication.FieldID), s.C(workercommunication.FieldGateID), s.C(workercommunication.FieldRoute)}, sub))
	}
}

// pruneTasks 只删除已结束、且不再等待结果投影的任务；按时间清理看截止时间，按条数清理看创建时间。
func pruneTasks(w window, batch int) predicate.WorkerTask {
	return func(s *entsql.Selector) {
		d := entsql.Dialect(s.Dialect())
		r := d.Table(workertask.Table).As("r")
		sub := d.Select(r.C(workertask.FieldID)).From(r)
		workertask.StatusIn(terminalStatuses...)(sub)
		workertask.Not(ProjectionPending())(sub)
		byTime, order := r.C(workertask.FieldDeadline), r.C(workertask.FieldID)
		if w.cutoff == 0 {
			byTime, order = r.C(workertask.FieldCreatedAt), entsql.Desc(order)
		}
		s.Where(entsql.In(s.C(workertask.FieldID), w.limited(sub, byTime, order).Limit(batch)))
	}
}

// pruneOrphanAttempts 删除所属任务已被清理的尝试记录。
func pruneOrphanAttempts(batch int) predicate.WorkerAttempt {
	return func(s *entsql.Selector) {
		d := entsql.Dialect(s.Dialect())
		a, t := d.Table(workerattempt.Table).As("a"), d.Table(workertask.Table).As("t")
		sub := d.Select(a.C(workerattempt.FieldID), a.C(workerattempt.FieldAttempt)).From(a).
			Where(entsql.NotIn(a.C(workerattempt.FieldID), d.Select(t.C(workertask.FieldID)).From(t))).
			OrderBy(a.C(workerattempt.FieldID), a.C(workerattempt.FieldAttempt)).Limit(batch)
		s.Where(tupleIn([]string{s.C(workerattempt.FieldID), s.C(workerattempt.FieldAttempt)}, sub))
	}
}

// tupleIn 生成多列 IN 子查询，用于复合主键的表。
func tupleIn(columns []string, sub *entsql.Selector) *entsql.Predicate {
	return entsql.P(func(b *entsql.Builder) {
		b.Wrap(func(b *entsql.Builder) { b.IdentComma(columns...) })
		b.WriteOp(entsql.OpIn)
		b.Wrap(func(b *entsql.Builder) { b.Join(sub) })
	})
}
