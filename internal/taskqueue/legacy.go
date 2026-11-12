package taskqueue

import "context"

// Legacy locates the one authoritative task behind a rollout adapter row.
func (q *Queue) Legacy(ctx context.Context, id int) (Record, error) {
	return scan(q.DB.QueryRowContext(ctx, `SELECT `+columns+` FROM worker_tasks WHERE legacy_id=$1`, id))
}
