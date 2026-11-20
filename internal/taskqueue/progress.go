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
	n, err := q.Ent.WorkerTask.Update().Where(currentLease(id, gate, u, now)...).SetProgress(string(b)).Save(ctx)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLease
	}
	return nil
}
