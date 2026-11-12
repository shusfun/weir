package taskqueue

import (
	"context"
	"errors"
	"time"
)

type AttemptStatistics struct {
	GateID       int      `json:"gate_id"`
	Kind         string   `json:"kind"`
	Attempts     int64    `json:"attempts"`
	Active       int64    `json:"active"`
	Succeeded    int64    `json:"succeeded"`
	Failed       int64    `json:"failed"`
	Cancelled    int64    `json:"cancelled"`
	Unknown      int64    `json:"unknown"`
	Finished     int64    `json:"finished"`
	DurationMS   int64    `json:"duration_ms"`
	Measured     int64    `json:"measured_results"`
	ResultBytes  *int64   `json:"result_bytes"`
	MeanDuration *float64 `json:"mean_duration_ms"`
	FailureRate  *float64 `json:"failure_rate"`
}

type Statistics struct {
	From   time.Time           `json:"from"`
	Until  time.Time           `json:"until"`
	Items  []AttemptStatistics `json:"items"`
	States map[string]int64    `json:"task_states"`
}

// Statistics aggregates persisted attempts, including retries, instead of a
// truncated recent-task list. The window is defined by attempt start time.
func (q *Queue) Statistics(ctx context.Context, from, until time.Time) (out Statistics, err error) {
	if !until.After(from) || until.Sub(from) > 14*24*time.Hour {
		return out, errors.New("invalid statistics window")
	}
	out = Statistics{From: from.UTC(), Until: until.UTC(), Items: []AttemptStatistics{}, States: map[string]int64{}}
	rows, err := q.DB.QueryContext(ctx, `SELECT a.gate_id,t.kind,count(*),
		SUM(CASE WHEN a.finished_at=0 THEN 1 ELSE 0 END),
		SUM(CASE WHEN a.outcome='succeeded' THEN 1 ELSE 0 END),
		SUM(CASE WHEN a.outcome='failed' THEN 1 ELSE 0 END),
		SUM(CASE WHEN a.outcome='cancelled' THEN 1 ELSE 0 END),
		SUM(CASE WHEN a.finished_at>0 AND a.outcome NOT IN ('succeeded','failed','cancelled') THEN 1 ELSE 0 END),
		SUM(CASE WHEN a.finished_at>0 THEN 1 ELSE 0 END),
		SUM(CASE WHEN a.finished_at>=a.started_at THEN a.finished_at-a.started_at ELSE 0 END),
		count(a.result_bytes),COALESCE(SUM(a.result_bytes),0)
		FROM worker_attempts a JOIN worker_tasks t ON t.id=a.task_id
		WHERE a.started_at>=$1 AND a.started_at<$2 GROUP BY a.gate_id,t.kind ORDER BY a.gate_id,t.kind`, from.UnixMilli(), until.UnixMilli())
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item AttemptStatistics
		var resultBytes int64
		if err = rows.Scan(&item.GateID, &item.Kind, &item.Attempts, &item.Active, &item.Succeeded, &item.Failed, &item.Cancelled, &item.Unknown, &item.Finished, &item.DurationMS, &item.Measured, &resultBytes); err != nil {
			rows.Close()
			return out, err
		}
		if item.Measured > 0 {
			item.ResultBytes = &resultBytes
		}
		if item.Finished > 0 {
			mean := float64(item.DurationMS) / float64(item.Finished)
			item.MeanDuration = &mean
		}
		if known := item.Succeeded + item.Failed; known > 0 {
			rate := float64(item.Failed) / float64(known)
			item.FailureRate = &rate
		}
		out.Items = append(out.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = q.DB.QueryContext(ctx, `SELECT status,count(*) FROM worker_tasks GROUP BY status`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n int64
		if err = rows.Scan(&state, &n); err != nil {
			return out, err
		}
		out.States[state] = n
	}
	return out, rows.Err()
}
