package taskqueue

import (
	"context"
	"time"

	"github.com/shusfun/weir/internal/store/ent/workeractivity"
)

type Activity struct {
	TaskID  string    `json:"task_id"`
	GateID  int       `json:"gate_id"`
	Attempt int       `json:"attempt"`
	At      time.Time `json:"at"`
	Reason  string    `json:"reason"`
}

func (q *Queue) Activity(ctx context.Context, taskID string) ([]Activity, error) {
	rows, err := q.Ent.WorkerActivity.Query().Where(workeractivity.TaskID(taskID)).
		Order(workeractivity.ByAt(), workeractivity.ByID()).Limit(200).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Activity, 0, len(rows))
	for _, a := range rows {
		out = append(out, Activity{TaskID: a.TaskID, GateID: a.GateID, Attempt: int(a.Attempt), At: time.UnixMilli(a.At).UTC(), Reason: a.Reason})
	}
	return out, nil
}
