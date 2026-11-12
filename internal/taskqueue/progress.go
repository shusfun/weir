package taskqueue

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func (q *Queue) Progress(ctx context.Context, id string, gate int, u contracts.TaskUpdate, now time.Time) error {
	if u.Progress == nil || u.Progress.Validate() != nil || len(u.Result) > 0 || u.Error != nil {
		return errors.New("invalid task progress")
	}
	b, err := json.Marshal(u.Progress)
	if err != nil {
		return err
	}
	v, err := q.DB.ExecContext(ctx, `UPDATE worker_tasks SET progress=$1 WHERE id=$2 AND gate_id=$3 AND attempt=$4 AND lease_token=$5 AND status='leased' AND lease_until>$6 AND deadline>$6`, string(b), id, gate, u.Attempt, u.LeaseToken, now.UnixMilli())
	if err != nil {
		return err
	}
	n, err := v.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLease
	}
	return nil
}
