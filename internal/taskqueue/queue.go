package taskqueue

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/workertask"
	"github.com/shusfun/weir/packages/contracts"
)

var (
	ErrLease    = errors.New("task lease is no longer current")
	ErrConflict = errors.New("idempotency key already has different work")
	// ErrNotFound 表示任务或 Gate 能力文档不存在，与存储故障区分。
	ErrNotFound = errors.New("task queue record not found")
)

type Locker interface {
	Do(context.Context, string, func(context.Context) error) error
}

// Queue 只经 Ent 访问 worker_* 表。Dialect 用来决定是否附加行锁：SQLite 是单写者，
// 不支持 FOR UPDATE，靠单连接串行化。
type Queue struct {
	Ent        *ent.Client
	Dialect    string
	Lock       Locker
	PruneBatch int
}

type Request struct {
	Kind         string
	TraceID      string
	Key          string
	Payload      json.RawMessage
	Requirements contracts.TaskRequirements
	Candidates   []int
	Deadline     time.Time
	LegacyID     int
}

type Record struct {
	Progress    *contracts.TaskProgress `json:"progress,omitempty"`
	CandidateAt time.Time               `json:"candidate_at"`
	Task        contracts.Task          `json:"task"`
	Status      string                  `json:"status"`
	GateID      int                     `json:"gate_id"`
	LegacyID    int                     `json:"legacy_id,omitempty"`
	Candidates  []int                   `json:"candidates"`
	Result      json.RawMessage         `json:"result,omitempty"`
	Error       *contracts.TaskError    `json:"error,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
	Ciphertext  string                  `json:"-"`
}

func token() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// notFound 把 Ent 的不存在错误收敛为 ErrNotFound，其余错误原样返回。
func notFound(err error) error {
	if ent.IsNotFound(err) {
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return err
}

// withTx 在一个 Ent 事务里执行 fn，出错回滚。
func (q *Queue) withTx(ctx context.Context, fn func(*ent.Tx) error) error {
	tx, err := q.Ent.Tx(ctx)
	if err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// lockRow 在 PostgreSQL 上对选中的行加 FOR UPDATE SKIP LOCKED，让并发的中心实例跳过
// 已被领取的行；SQLite 只有一个写连接，不需要也不支持行锁。
func (q *Queue) lockRow(query *ent.WorkerTaskQuery) *ent.WorkerTaskQuery {
	if q.Dialect == dialect.Postgres {
		return query.ForUpdate(entsql.WithLockAction(entsql.SkipLocked))
	}
	return query
}

func (q *Queue) Enqueue(ctx context.Context, req Request, encrypted string, now time.Time) (r Record, err error) {
	if err = req.Requirements.Validate(); err != nil {
		return r, err
	}
	if req.Key == "" || len(req.Key) > 256 || len(req.TraceID) > 64 || !req.Deadline.After(now) || !contracts.KnownTask(req.Kind) || len(req.Candidates) == 0 || len(req.Candidates) > 1024 || encrypted == "" {
		return r, errors.New("invalid task request")
	}
	if err = contracts.ValidateTaskPayload(req.Kind, contracts.TaskSchemaVersion(req.Kind), req.Payload); err != nil {
		return r, err
	}
	seen := map[int]bool{}
	for _, id := range req.Candidates {
		if id < 1 || seen[id] {
			return r, errors.New("invalid task candidates")
		}
		seen[id] = true
	}
	if !contracts.Relocatable(req.Kind) && req.Requirements.TargetGateID == 0 || req.Requirements.TargetGateID != 0 && (len(req.Candidates) != 1 || req.Candidates[0] != req.Requirements.TargetGateID) {
		return r, errors.New("task requires its resource's Gate")
	}
	err = q.Lock.Do(ctx, "worker-enqueue", func(ctx context.Context) error {
		sum := sha256.Sum256(req.Payload)
		wantHash := hex.EncodeToString(sum[:])
		reqJSON, _ := json.Marshal(req.Requirements)
		existing, e := q.Ent.WorkerTask.Query().Where(workertask.Idem(req.Key)).
			Select(workertask.FieldID, workertask.FieldKind, workertask.FieldRequirements, workertask.FieldPayloadHash).Only(ctx)
		if e == nil {
			// Compare plaintext hashes, since encryption may be randomized.
			if existing.Kind != req.Kind || existing.Requirements != string(reqJSON) || existing.PayloadHash != wantHash {
				return ErrConflict
			}
			r, e = q.Get(ctx, existing.ID)
			return e
		}
		if !ent.IsNotFound(e) {
			return e
		}
		id := token()
		candidates, _ := json.Marshal(req.Candidates)
		reason := "capability_candidate_order"
		if req.Requirements.TargetGateID != 0 {
			reason = "resource_bound"
		} else if req.Requirements.PreferredGateID == req.Candidates[0] {
			reason = "preferred_gate"
		}
		e = q.withTx(ctx, func(tx *ent.Tx) error {
			if err := tx.WorkerTask.Create().SetID(id).SetKind(req.Kind).SetVersion(int32(contracts.TaskSchemaVersion(req.Kind))).
				SetTraceID(req.TraceID).SetIdem(req.Key).SetPayloadHash(wantHash).SetBody(encrypted).
				SetRequirements(string(reqJSON)).SetCandidates(string(candidates)).SetDeadline(req.Deadline.UnixMilli()).
				SetCreatedAt(now.UnixMilli()).SetStatus("pending").SetLegacyID(req.LegacyID).SetCandidateAt(now.UnixMilli()).
				Exec(ctx); err != nil {
				return err
			}
			return addActivity(ctx, tx, id, req.Candidates[0], 0, now, reason)
		})
		if e != nil {
			return e
		}
		r, e = q.Get(ctx, id)
		return e
	})
	return r, err
}

func addActivity(ctx context.Context, tx *ent.Tx, taskID string, gate, attempt int, at time.Time, reason string) error {
	return tx.WorkerActivity.Create().SetID(token()).SetTaskID(taskID).SetGateID(gate).SetAttempt(int32(attempt)).
		SetAt(at.UnixMilli()).SetReason(reason).Exec(ctx)
}

// summaryFields 用于列表和清扫：不读取密文、租约令牌与结果正文。
var summaryFields = []string{
	workertask.FieldID, workertask.FieldKind, workertask.FieldVersion, workertask.FieldTraceID, workertask.FieldIdem,
	workertask.FieldRequirements, workertask.FieldCandidates, workertask.FieldDeadline, workertask.FieldCreatedAt,
	workertask.FieldStatus, workertask.FieldGateID, workertask.FieldAttempt, workertask.FieldLeaseUntil,
	workertask.FieldError, workertask.FieldLegacyID, workertask.FieldCandidateAt, workertask.FieldProgress,
}

func record(t *ent.WorkerTask) (r Record, err error) {
	r.Task.ID, r.Task.Kind, r.Task.Version, r.Task.TraceID = t.ID, t.Kind, int(t.Version), t.TraceID
	r.Task.IdempotencyKey, r.Status, r.GateID, r.Task.Attempt = t.Idem, t.Status, t.GateID, int(t.Attempt)
	r.Task.LeaseToken, r.LegacyID = t.LeaseToken, t.LegacyID
	// Payload is ciphertext until the host has authenticated the worker.
	r.Ciphertext = t.Body
	if err = json.Unmarshal([]byte(t.Requirements), &r.Task.Requirements); err != nil {
		return r, err
	}
	if err = json.Unmarshal([]byte(t.Candidates), &r.Candidates); err != nil {
		return r, err
	}
	r.Task.Deadline = time.UnixMilli(t.Deadline).UTC()
	r.Task.LeaseUntil = time.UnixMilli(t.LeaseUntil).UTC()
	r.CreatedAt = time.UnixMilli(t.CreatedAt).UTC()
	candidate := t.CandidateAt
	if candidate == 0 {
		candidate = t.CreatedAt
	}
	r.CandidateAt = time.UnixMilli(candidate).UTC()
	if t.Result != "" {
		r.Result = json.RawMessage(t.Result)
	}
	if t.Error != "" {
		err = json.Unmarshal([]byte(t.Error), &r.Error)
	}
	if err == nil && t.Progress != "" {
		err = json.Unmarshal([]byte(t.Progress), &r.Progress)
	}
	return r, err
}

func records(tasks []*ent.WorkerTask) ([]Record, error) {
	out := make([]Record, 0, len(tasks))
	for _, t := range tasks {
		r, err := record(t)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (q *Queue) Get(ctx context.Context, id string) (Record, error) {
	t, err := q.Ent.WorkerTask.Get(ctx, id)
	if err != nil {
		return Record{}, notFound(err)
	}
	return record(t)
}

var activeStatuses = []string{"leased", "cancelling"}

// ActiveCount 是 Gate 上已领取、尚未结束的任务数，供派发前的容量判断。
func (q *Queue) ActiveCount(ctx context.Context, gate int) (int, error) {
	return q.Ent.WorkerTask.Query().Where(workertask.GateID(gate), workertask.StatusIn(activeStatuses...)).Count(ctx)
}

// LatestSucceeded 返回同一 trace 下某 Gate 最近一次成功的指定任务。
func (q *Queue) LatestSucceeded(ctx context.Context, kind, traceID string, gate int) (string, error) {
	id, err := q.Ent.WorkerTask.Query().
		Where(workertask.Kind(kind), workertask.TraceID(traceID), workertask.GateID(gate), workertask.Status("succeeded")).
		Order(workertask.ByCreatedAt(entsql.OrderDesc()), workertask.ByID(entsql.OrderDesc())).
		FirstID(ctx)
	return id, notFound(err)
}

// Claim serializes capacity checks across center instances, not just requests
// handled by the same process. The row lock and status predicate also fence stale readers.
func (q *Queue) Claim(ctx context.Context, gate int, caps contracts.CapabilityDocument, res contracts.WorkerResources, now time.Time) (out []Record, err error) {
	if err = caps.Validate(); err != nil {
		return nil, err
	}
	if res.Validate() != nil {
		return nil, errors.New("invalid worker concurrency")
	}
	err = q.Lock.Do(ctx, "worker-dispatch", func(ctx context.Context) error {
		active, e := q.ActiveCount(ctx, gate)
		if e != nil {
			return e
		}
		if active >= res.Concurrency {
			return nil
		}
		downloads, e := q.Ent.WorkerTask.Query().Where(workertask.GateID(gate), workertask.StatusIn(activeStatuses...),
			workertask.KindIn(contracts.ActionAssetFetch, contracts.ActionReleaseReplace, contracts.TaskLayaInstall, contracts.TaskCoreInstall, contracts.TaskLayaPrepare)).Count(ctx)
		if e != nil {
			return e
		}
		rows, e := q.Ent.WorkerTask.Query().Where(workertask.Status("pending"), workertask.DeadlineGT(now.UnixMilli())).
			Order(workertask.ByCreatedAt(), workertask.ByID()).Limit(100).All(ctx)
		if e != nil {
			return e
		}
		pending, e := records(rows)
		if e != nil {
			return e
		}
		for _, r := range pending {
			if !eligible(r, gate, caps, res) {
				continue
			}
			if contracts.LargeDownload(r.Task.Kind) && downloads > 0 {
				continue
			}
			claimed, e := q.claim(ctx, r, gate, now)
			if e != nil {
				return e
			}
			if !claimed {
				continue
			}
			r, e = q.Get(ctx, r.Task.ID)
			if e != nil {
				return e
			}
			out = append(out, r)
			if contracts.LargeDownload(r.Task.Kind) {
				downloads++
			}
			active++
			if active >= res.Concurrency {
				break
			}
		}
		return nil
	})
	return out, err
}

// claim 领取一行待派发任务；行已被别的实例锁住或状态已变时返回 false。
func (q *Queue) claim(ctx context.Context, r Record, gate int, now time.Time) (claimed bool, err error) {
	lease := now.Add(contracts.TaskLease)
	if lease.After(r.Task.Deadline) {
		lease = r.Task.Deadline
	}
	err = q.withTx(ctx, func(tx *ent.Tx) error {
		_, e := q.lockRow(tx.WorkerTask.Query().Where(workertask.ID(r.Task.ID), workertask.Status("pending"))).OnlyID(ctx)
		if ent.IsNotFound(e) {
			return nil
		}
		if e != nil {
			return e
		}
		n, e := tx.WorkerTask.Update().Where(workertask.ID(r.Task.ID), workertask.Status("pending")).
			SetStatus("leased").SetGateID(gate).AddAttempt(1).SetLeaseToken(token()).SetLeaseUntil(lease.UnixMilli()).SetProgress("").
			Save(ctx)
		if e != nil || n != 1 {
			return e
		}
		attempt := r.Task.Attempt + 1
		if e = tx.WorkerAttempt.Create().SetID(r.Task.ID).SetAttempt(int32(attempt)).SetGateID(gate).
			SetStartedAt(now.UnixMilli()).SetOutcome("leased").Exec(ctx); e != nil {
			return e
		}
		if e = addActivity(ctx, tx, r.Task.ID, gate, attempt, now, "lease_claimed"); e != nil {
			return e
		}
		claimed = true
		return nil
	})
	return claimed, err
}

func eligible(r Record, gate int, c contracts.CapabilityDocument, res contracts.WorkerResources) bool {
	if !r.Task.Requirements.Matches(gate, r.Task.Kind, r.Task.Version, c, res) {
		return false
	}
	// Candidate order is chosen by the center. Only the first candidate may
	// claim; expiration or a retryable failure advances that order explicitly.
	return len(r.Candidates) > 0 && r.Candidates[0] == gate
}
