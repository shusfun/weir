package taskqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func (q *Queue) Renew(ctx context.Context, id string, gate int, u contracts.TaskUpdate, now time.Time) (time.Time, error) {
	until := now.Add(contracts.TaskLease)
	r, err := q.Get(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	if until.After(r.Task.Deadline) {
		until = r.Task.Deadline
	}
	v, err := q.DB.ExecContext(ctx, `UPDATE worker_tasks SET lease_until=$1 WHERE id=$2 AND gate_id=$3 AND attempt=$4 AND lease_token=$5 AND status='leased' AND lease_until>$6 AND deadline>$6`, until.UnixMilli(), id, gate, u.Attempt, u.LeaseToken, now.UnixMilli())
	if err != nil {
		return time.Time{}, err
	}
	n, err := v.RowsAffected()
	if err != nil {
		return time.Time{}, err
	}
	if n != 1 {
		return time.Time{}, ErrLease
	}
	return until, nil
}

func (q *Queue) Finish(ctx context.Context, id string, gate int, u contracts.TaskUpdate, now time.Time) error {
	return q.finish(ctx, id, gate, u, now, nil)
}

// FinishMeasured stores the accepted, decoded result size before host encryption.
// Older or unmeasured attempts retain NULL rather than reporting invented zeros.
func (q *Queue) FinishMeasured(ctx context.Context, id string, gate int, u contracts.TaskUpdate, now time.Time, resultBytes int64) error {
	if resultBytes < 0 || resultBytes > contracts.MaxTaskResult || u.Error != nil && resultBytes != 0 {
		return errors.New("invalid task result measurement")
	}
	return q.finish(ctx, id, gate, u, now, &resultBytes)
}

func (q *Queue) finish(ctx context.Context, id string, gate int, u contracts.TaskUpdate, now time.Time, resultBytes *int64) error {
	// The host seals accepted wire results before persistence. Ciphertext has
	// base64 and envelope overhead beyond the public wire limit.
	if len(u.Result) > 2*contracts.MaxTaskResult || (u.Error == nil && !json.Valid(u.Result)) || (u.Error != nil && len(u.Result) > 0) {
		return errors.New("invalid task result")
	}
	if u.Error != nil && u.Error.Validate() != nil {
		return errors.New("invalid task error")
	}
	if len(u.Result) > 0 && (bytes.Equal(bytes.TrimSpace(u.Result), []byte("null")) || !json.Valid(u.Result)) {
		return errors.New("invalid task result")
	}
	tx, err := q.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM worker_tasks WHERE id=$1`, id))
	if err != nil {
		return err
	}
	if r.GateID != gate || r.Task.Attempt != u.Attempt || r.Task.LeaseToken != u.LeaseToken || !r.Task.LeaseUntil.After(now) || !r.Task.Deadline.After(now) {
		return ErrLease
	}
	if r.Status == "succeeded" || r.Status == "failed" {
		return nil
	}
	if r.Status != "leased" {
		return ErrLease
	}
	status := "succeeded"
	failure := ""
	result := string(u.Result)
	if u.Error != nil {
		status = "failed"
		b, _ := json.Marshal(u.Error)
		failure = string(b)
	}
	remaining := r.Candidates
	attemptOutcome := status
	if u.Error != nil && u.Error.Retryable && contracts.Relocatable(r.Task.Kind) && len(remaining) > 1 {
		status = "pending"
		remaining = remaining[1:]
		result = ""
	}
	candidates, _ := json.Marshal(remaining)
	v, err := tx.ExecContext(ctx, `UPDATE worker_tasks SET status=$1,result=$2,error=$3,candidates=$4,candidate_at=$8 WHERE id=$5 AND status='leased' AND attempt=$6 AND lease_token=$7 AND lease_until>$8`, status, result, failure, string(candidates), id, u.Attempt, u.LeaseToken, now.UnixMilli())
	if err != nil {
		return err
	}
	n, _ := v.RowsAffected()
	if n != 1 {
		return ErrLease
	}
	_, err = tx.ExecContext(ctx, `UPDATE worker_attempts SET finished_at=$1,outcome=$2,result_bytes=$5 WHERE task_id=$3 AND attempt=$4`, now.UnixMilli(), attemptOutcome, id, u.Attempt, resultBytes)
	if err != nil {
		return err
	}
	reason := status
	if status == "pending" {
		reason = "retryable_failure_reassigned"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO worker_activity(id,task_id,gate_id,attempt,at,reason) VALUES($1,$2,$3,$4,$5,$6)`, token(), id, remaining[0], u.Attempt, now.UnixMilli(), reason); err != nil {
		return err
	}
	return tx.Commit()
}

func (q *Queue) AcknowledgeCancel(ctx context.Context, id string, gate int, u contracts.TaskUpdate, now time.Time) error {
	tx, err := q.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM worker_tasks WHERE id=$1`, id))
	if err != nil {
		return err
	}
	if r.GateID != gate || r.Task.Attempt != u.Attempt || r.Task.LeaseToken != u.LeaseToken {
		return ErrLease
	}
	if r.Status == "cancelled" {
		return nil
	}
	if r.Status != "cancelling" || !r.Task.LeaseUntil.After(now) {
		return ErrLease
	}
	v, err := tx.ExecContext(ctx, `UPDATE worker_tasks SET status='cancelled' WHERE id=$1 AND status='cancelling' AND attempt=$2 AND lease_token=$3 AND lease_until>$4`, id, u.Attempt, u.LeaseToken, now.UnixMilli())
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
	if _, err = tx.ExecContext(ctx, `UPDATE worker_attempts SET finished_at=$1,outcome='cancelled' WHERE task_id=$2 AND attempt=$3`, now.UnixMilli(), id, u.Attempt); err != nil {
		return err
	}
	return tx.Commit()
}

func (q *Queue) Cancel(ctx context.Context, id string) error {
	v, err := q.DB.ExecContext(ctx, `UPDATE worker_tasks SET status=CASE WHEN status='leased' THEN 'cancelling' ELSE 'cancelled' END WHERE id=$1 AND status IN ('pending','leased')`, id)
	if err != nil {
		return err
	}
	n, _ := v.RowsAffected()
	if n == 0 {
		r, e := q.Get(ctx, id)
		if e != nil {
			return e
		}
		if r.Status != "cancelled" && r.Status != "cancelling" {
			return ErrLease
		}
	}
	return nil
}

func (q *Queue) Cancellations(ctx context.Context, gate int) (out []string, err error) {
	rows, err := q.DB.QueryContext(ctx, `SELECT id FROM worker_tasks WHERE gate_id=$1 AND status='cancelling'`, gate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Sweep runs outside the poll path. Side-effecting attempts whose result is
// unknown require reconciliation; they are never blindly dispatched again.
func (q *Queue) Sweep(ctx context.Context, now time.Time) error {
	_, err := q.SweepChanged(ctx, now)
	return err
}

func (q *Queue) SweepChanged(ctx context.Context, now time.Time) (changed int, err error) {
	err = q.Lock.Do(ctx, "worker-dispatch", func(ctx context.Context) error {
		rows, err := q.DB.QueryContext(ctx, `SELECT `+columns+` FROM worker_tasks WHERE (status IN ('leased','cancelling') AND lease_until<=$1) OR (status='pending' AND (deadline<=$1 OR COALESCE(NULLIF(candidate_at,0),created_at)<=$2))`, now.UnixMilli(), now.Add(-contracts.TaskLease).UnixMilli())
		if err != nil {
			return err
		}
		var all []Record
		for rows.Next() {
			r, e := scan(rows)
			if e != nil {
				rows.Close()
				return e
			}
			all = append(all, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, r := range all {
			if r.Status == "pending" && r.Task.Deadline.After(now) && (!contracts.Relocatable(r.Task.Kind) || len(r.Candidates) < 2) {
				continue
			}
			status := "unknown"
			candidates := r.Candidates
			switch {
			case r.Status == "cancelling":
				status = "cancelled"
			case !r.Task.Deadline.After(now):
				status = "expired"
			case contracts.Relocatable(r.Task.Kind) && len(candidates) > 1:
				status = "pending"
				candidates = candidates[1:]
			}
			b, _ := json.Marshal(candidates)
			tx, e := q.DB.BeginTx(ctx, nil)
			if e != nil {
				return e
			}
			v, e := tx.ExecContext(ctx, `UPDATE worker_tasks SET status=$1,candidates=$2,candidate_at=$6 WHERE id=$3 AND status=$4 AND lease_until=$5 AND candidate_at=$7`, status, string(b), r.Task.ID, r.Status, r.Task.LeaseUntil.UnixMilli(), now.UnixMilli(), r.CandidateAt.UnixMilli())
			if e != nil {
				tx.Rollback()
				return e
			}
			n, _ := v.RowsAffected()
			if n == 1 {
				reason := status
				if status == "pending" {
					reason = "lease_expired_reassigned"
					if r.Status == "pending" {
						reason = "pickup_timeout_reassigned"
					}
				}
				_, e = tx.ExecContext(ctx, `INSERT INTO worker_activity(id,task_id,gate_id,attempt,at,reason) VALUES($1,$2,$3,$4,$5,$6)`, token(), r.Task.ID, candidates[0], r.Task.Attempt, now.UnixMilli(), reason)
				if e != nil {
					tx.Rollback()
					return e
				}
			}
			if n == 1 && r.Task.Attempt > 0 {
				outcome := "lease_expired"
				if r.Status == "cancelling" {
					outcome = "cancelled"
				}
				_, e = tx.ExecContext(ctx, `UPDATE worker_attempts SET finished_at=$1,outcome=$2 WHERE task_id=$3 AND attempt=$4 AND finished_at=0`, now.UnixMilli(), outcome, r.Task.ID, r.Task.Attempt)
			}
			if e != nil {
				tx.Rollback()
				return e
			}
			if e = tx.Commit(); e != nil {
				return e
			}
			changed += int(n)
		}
		return nil
	})
	return changed, err
}

func (q *Queue) List(ctx context.Context, limit int) (out []Record, err error) {
	if limit < 1 || limit > 500 {
		return nil, errors.New("invalid task list limit")
	}
	rows, err := q.DB.QueryContext(ctx, `SELECT `+columns+` FROM worker_tasks ORDER BY created_at DESC,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, e := scan(rows)
		if e != nil {
			return nil, e
		}
		r.Task.LeaseToken = ""
		r.Ciphertext = ""
		out = append(out, r)
	}
	return out, rows.Err()
}
