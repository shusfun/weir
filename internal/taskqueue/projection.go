package taskqueue

import (
	"context"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

const projectionFilter = `status IN ('succeeded','failed','expired','unknown','cancelled') AND projection_after>=0 AND (legacy_id<>0 OR kind IN ('` + contracts.TaskLayaPrepare + `','` + contracts.TaskLayaInstall + `','` + contracts.TaskLayaRemove + `'))`

func (q *Queue) initProjections(ctx context.Context) error {
	rows, err := q.DB.QueryContext(ctx, `SELECT projection_after FROM worker_tasks LIMIT 0`)
	if err == nil {
		err = rows.Close()
	} else {
		_, err = q.DB.ExecContext(ctx, `ALTER TABLE worker_tasks ADD COLUMN projection_after BIGINT NOT NULL DEFAULT 0`)
	}
	if err != nil {
		return err
	}
	_, err = q.DB.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS worker_tasks_projection ON worker_tasks(projection_after,id) WHERE `+projectionFilter)
	return err
}

func (q *Queue) PendingProjections(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := q.DB.QueryContext(ctx, `SELECT id FROM worker_tasks WHERE `+projectionFilter+` AND projection_after<=$1 ORDER BY projection_after,id LIMIT 100`, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (q *Queue) NeedsProjection(ctx context.Context, id string) (bool, error) {
	var pending bool
	err := q.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM worker_tasks WHERE id=$1 AND `+projectionFilter+`)`, id).Scan(&pending)
	return pending, err
}

func (q *Queue) ResolveProjection(ctx context.Context, id string, retryAt time.Time) error {
	after := int64(-1)
	if !retryAt.IsZero() {
		after = retryAt.UnixMilli()
	}
	_, err := q.DB.ExecContext(ctx, `UPDATE worker_tasks SET projection_after=$1 WHERE id=$2 AND projection_after>=0`, after, id)
	return err
}
