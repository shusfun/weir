package taskqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/predicate"
	"github.com/shusfun/weir/internal/store/ent/workerattempt"
	"github.com/shusfun/weir/internal/store/ent/workertask"
	"github.com/shusfun/weir/packages/contracts"
)

// Legacy locates the one authoritative task behind a rollout adapter row.
func (q *Queue) Legacy(ctx context.Context, id int) (Record, error) {
	t, err := q.Ent.WorkerTask.Query().Where(workertask.LegacyID(id)).Only(ctx)
	if err != nil {
		return Record{}, notFound(err)
	}
	return record(t)
}

// currentLease 是 Gate 仍持有该次领取的条件：同一 Gate、同一尝试、同一令牌，租约与截止时间都未过。
func currentLease(id string, gate int, u contracts.TaskUpdate, now time.Time) []predicate.WorkerTask {
	return []predicate.WorkerTask{
		workertask.ID(id), workertask.GateID(gate), workertask.Attempt(int32(u.Attempt)), workertask.LeaseToken(u.LeaseToken),
		workertask.Status("leased"), workertask.LeaseUntilGT(now.UnixMilli()), workertask.DeadlineGT(now.UnixMilli()),
	}
}

func (q *Queue) Renew(ctx context.Context, id string, gate int, u contracts.TaskUpdate, now time.Time) (time.Time, error) {
	until := now.Add(contracts.TaskLease)
	r, err := q.Get(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	if until.After(r.Task.Deadline) {
		until = r.Task.Deadline
	}
	n, err := q.Ent.WorkerTask.Update().Where(currentLease(id, gate, u, now)...).SetLeaseUntil(until.UnixMilli()).Save(ctx)
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
	return q.withTx(ctx, func(tx *ent.Tx) error {
		t, err := tx.WorkerTask.Get(ctx, id)
		if err != nil {
			return notFound(err)
		}
		r, err := record(t)
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
		n, err := tx.WorkerTask.Update().
			Where(workertask.ID(id), workertask.Status("leased"), workertask.Attempt(int32(u.Attempt)), workertask.LeaseToken(u.LeaseToken), workertask.LeaseUntilGT(now.UnixMilli())).
			SetStatus(status).SetResult(result).SetError(failure).SetCandidates(string(candidates)).SetCandidateAt(now.UnixMilli()).
			Save(ctx)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrLease
		}
		attempt := tx.WorkerAttempt.Update().Where(workerattempt.ID(id), workerattempt.Attempt(int32(u.Attempt))).
			SetFinishedAt(now.UnixMilli()).SetOutcome(attemptOutcome)
		if resultBytes == nil {
			attempt.ClearResultBytes()
		} else {
			attempt.SetResultBytes(*resultBytes)
		}
		if err = attempt.Exec(ctx); err != nil {
			return err
		}
		reason := status
		if status == "pending" {
			reason = "retryable_failure_reassigned"
		}
		return addActivity(ctx, tx, id, remaining[0], u.Attempt, now, reason)
	})
}

func (q *Queue) AcknowledgeCancel(ctx context.Context, id string, gate int, u contracts.TaskUpdate, now time.Time) error {
	return q.withTx(ctx, func(tx *ent.Tx) error {
		t, err := tx.WorkerTask.Get(ctx, id)
		if err != nil {
			return notFound(err)
		}
		r, err := record(t)
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
		n, err := tx.WorkerTask.Update().
			Where(workertask.ID(id), workertask.Status("cancelling"), workertask.Attempt(int32(u.Attempt)), workertask.LeaseToken(u.LeaseToken), workertask.LeaseUntilGT(now.UnixMilli())).
			SetStatus("cancelled").Save(ctx)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrLease
		}
		return tx.WorkerAttempt.Update().Where(workerattempt.ID(id), workerattempt.Attempt(int32(u.Attempt))).
			SetFinishedAt(now.UnixMilli()).SetOutcome("cancelled").Exec(ctx)
	})
}

// Cancel 把已领取的任务标为 cancelling 等 Gate 确认，未领取的直接取消；两步在同一事务里，
// 状态只会按其中一条路径变化。
func (q *Queue) Cancel(ctx context.Context, id string) error {
	var n int
	err := q.withTx(ctx, func(tx *ent.Tx) error {
		leased, err := tx.WorkerTask.Update().Where(workertask.ID(id), workertask.Status("leased")).SetStatus("cancelling").Save(ctx)
		if err != nil {
			return err
		}
		pending, err := tx.WorkerTask.Update().Where(workertask.ID(id), workertask.Status("pending")).SetStatus("cancelled").Save(ctx)
		n = leased + pending
		return err
	})
	if err != nil {
		return err
	}
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

func (q *Queue) Cancellations(ctx context.Context, gate int) ([]string, error) {
	return q.Ent.WorkerTask.Query().Where(workertask.GateID(gate), workertask.Status("cancelling")).IDs(ctx)
}

// Sweep runs outside the poll path. Side-effecting attempts whose result is
// unknown require reconciliation; they are never blindly dispatched again.
func (q *Queue) Sweep(ctx context.Context, now time.Time) error {
	_, err := q.SweepChanged(ctx, now)
	return err
}

func (q *Queue) SweepChanged(ctx context.Context, now time.Time) (changed int, err error) {
	ids, err := q.SweepTransitions(ctx, now)
	return len(ids), err
}

func (q *Queue) SweepTransitions(ctx context.Context, now time.Time) (changed []string, err error) {
	at := now.UnixMilli()
	pickup := now.Add(-contracts.TaskLease).UnixMilli()
	err = q.Lock.Do(ctx, "worker-dispatch", func(ctx context.Context) error {
		rows, err := q.Ent.WorkerTask.Query().Select(summaryFields...).Where(workertask.Or(
			workertask.And(workertask.StatusIn(activeStatuses...), workertask.LeaseUntilLTE(at)),
			workertask.And(workertask.Status("pending"), workertask.Or(
				workertask.DeadlineLTE(at),
				// 未改派过的任务 candidate_at 为 0，按创建时间计算领取超时。
				workertask.And(workertask.CandidateAtNEQ(0), workertask.CandidateAtLTE(pickup)),
				workertask.And(workertask.CandidateAt(0), workertask.CreatedAtLTE(pickup)),
			)),
		)).All(ctx)
		if err != nil {
			return err
		}
		all, err := records(rows)
		if err != nil {
			return err
		}
		for _, r := range all {
			if r.Status == "pending" && r.Task.Deadline.After(now) && (!contracts.Relocatable(r.Task.Kind) || len(r.Candidates) < 2) {
				continue
			}
			moved, e := q.sweepOne(ctx, r, now)
			if e != nil {
				return e
			}
			if moved {
				changed = append(changed, r.Task.ID)
			}
		}
		return nil
	})
	return changed, err
}

func (q *Queue) sweepOne(ctx context.Context, r Record, now time.Time) (moved bool, err error) {
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
	err = q.withTx(ctx, func(tx *ent.Tx) error {
		n, e := tx.WorkerTask.Update().
			Where(workertask.ID(r.Task.ID), workertask.Status(r.Status), workertask.LeaseUntil(r.Task.LeaseUntil.UnixMilli()), workertask.CandidateAt(r.CandidateAt.UnixMilli())).
			SetStatus(status).SetCandidates(string(b)).SetCandidateAt(now.UnixMilli()).Save(ctx)
		if e != nil || n != 1 {
			return e
		}
		moved = true
		reason := status
		if status == "pending" {
			reason = "lease_expired_reassigned"
			if r.Status == "pending" {
				reason = "pickup_timeout_reassigned"
			}
		}
		if e = addActivity(ctx, tx, r.Task.ID, candidates[0], r.Task.Attempt, now, reason); e != nil {
			return e
		}
		if r.Task.Attempt == 0 {
			return nil
		}
		outcome := "lease_expired"
		if r.Status == "cancelling" {
			outcome = "cancelled"
		}
		return tx.WorkerAttempt.Update().
			Where(workerattempt.ID(r.Task.ID), workerattempt.Attempt(int32(r.Task.Attempt)), workerattempt.FinishedAt(0)).
			SetFinishedAt(now.UnixMilli()).SetOutcome(outcome).Exec(ctx)
	})
	return moved && err == nil, err
}

func (q *Queue) List(ctx context.Context, limit int) ([]Record, error) {
	if limit < 1 || limit > 500 {
		return nil, errors.New("invalid task list limit")
	}
	rows, err := q.Ent.WorkerTask.Query().Select(summaryFields...).
		Order(workertask.ByCreatedAt(entsql.OrderDesc()), workertask.ByID()).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	return records(rows)
}
