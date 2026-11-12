package taskqueue

import (
	"context"
	"time"
)

type Activity struct {
	TaskID  string    `json:"task_id"`
	GateID  int       `json:"gate_id"`
	Attempt int       `json:"attempt"`
	At      time.Time `json:"at"`
	Reason  string    `json:"reason"`
}

func (q *Queue) Activity(ctx context.Context, taskID string) ([]Activity, error) {
	rows, err := q.DB.QueryContext(ctx, `SELECT task_id,gate_id,attempt,at,reason FROM worker_activity WHERE task_id=$1 ORDER BY at,id LIMIT 200`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		var a Activity
		var at int64
		if err = rows.Scan(&a.TaskID, &a.GateID, &a.Attempt, &at, &a.Reason); err != nil {
			return nil, err
		}
		a.At = time.UnixMilli(at).UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}
