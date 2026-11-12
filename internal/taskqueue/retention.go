package taskqueue

import (
	"context"
	"errors"
	"time"
)

// PruneObservations bounds worker history. Active work is never removed.
// Each pass removes bounded batches so cleanup cannot monopolize the database.
func (q *Queue) PruneObservations(ctx context.Context, now time.Time, retention time.Duration, maxRecords int) error {
	if retention < time.Hour || retention > 90*24*time.Hour || maxRecords < 100 || maxRecords > 1_000_000 {
		return errors.New("invalid worker retention policy")
	}
	return q.Lock.Do(ctx, "worker-retention", func(ctx context.Context) error {
		tx, err := q.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		cutoff := now.Add(-retention).UnixMilli()
		for _, query := range []string{
			`DELETE FROM worker_activity WHERE id IN (SELECT id FROM worker_activity WHERE at<$1 ORDER BY at,id LIMIT 2000)`,
			`DELETE FROM worker_communication WHERE (bucket,gate_id,route) IN (SELECT bucket,gate_id,route FROM worker_communication WHERE bucket<$1 ORDER BY bucket,gate_id,route LIMIT 2000)`,
			`DELETE FROM worker_tasks WHERE id IN (SELECT id FROM worker_tasks WHERE status IN ('succeeded','failed','cancelled','expired','unknown') AND deadline<$1 ORDER BY deadline,id LIMIT 2000)`,
		} {
			if _, err = tx.ExecContext(ctx, query, cutoff); err != nil {
				return err
			}
		}
		for _, query := range []string{
			`DELETE FROM worker_activity WHERE id IN (SELECT id FROM worker_activity ORDER BY at DESC,id DESC LIMIT 2000 OFFSET $1)`,
			`DELETE FROM worker_communication WHERE (bucket,gate_id,route) IN (SELECT bucket,gate_id,route FROM worker_communication ORDER BY bucket DESC,gate_id,route LIMIT 2000 OFFSET $1)`,
			`DELETE FROM worker_tasks WHERE id IN (SELECT id FROM worker_tasks WHERE status IN ('succeeded','failed','cancelled','expired','unknown') ORDER BY created_at DESC,id DESC LIMIT 2000 OFFSET $1)`,
		} {
			if _, err = tx.ExecContext(ctx, query, maxRecords); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM worker_attempts WHERE task_id IN (SELECT a.task_id FROM worker_attempts a LEFT JOIN worker_tasks t ON a.task_id=t.id WHERE t.id IS NULL LIMIT 2000)`); err != nil {
			return err
		}
		return tx.Commit()
	})
}
