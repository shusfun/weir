package taskqueue

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

var (
	ErrLease    = errors.New("task lease is no longer current")
	ErrConflict = errors.New("idempotency key already has different work")
)

type Locker interface {
	Do(context.Context, string, func(context.Context) error) error
}
type Queue struct {
	DB   *sql.DB
	Lock Locker
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

// Bodies are encrypted by the host. Dispatch metadata contains no task inputs.
func (q *Queue) Migrate(ctx context.Context) error {
	return q.Lock.Do(ctx, "worker-schema", func(ctx context.Context) error {
		for _, s := range []string{
			`CREATE TABLE IF NOT EXISTS worker_capabilities (gate_id BIGINT PRIMARY KEY,hash TEXT NOT NULL,document TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS worker_tasks (id TEXT PRIMARY KEY, kind TEXT NOT NULL, version INTEGER NOT NULL, trace_id TEXT NOT NULL, idem TEXT NOT NULL UNIQUE, payload_hash TEXT NOT NULL, body TEXT NOT NULL, requirements TEXT NOT NULL, candidates TEXT NOT NULL, deadline BIGINT NOT NULL, created_at BIGINT NOT NULL, status TEXT NOT NULL, gate_id BIGINT NOT NULL DEFAULT 0, attempt INTEGER NOT NULL DEFAULT 0, lease_token TEXT NOT NULL DEFAULT '', lease_until BIGINT NOT NULL DEFAULT 0, result TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', legacy_id BIGINT NOT NULL DEFAULT 0)`,
			`CREATE INDEX IF NOT EXISTS worker_tasks_dispatch ON worker_tasks(status, created_at)`,
			`CREATE INDEX IF NOT EXISTS worker_tasks_gate ON worker_tasks(gate_id, status)`,
			`CREATE INDEX IF NOT EXISTS worker_tasks_legacy ON worker_tasks(legacy_id)`,
			`CREATE TABLE IF NOT EXISTS worker_attempts (task_id TEXT NOT NULL, attempt INTEGER NOT NULL, gate_id BIGINT NOT NULL, started_at BIGINT NOT NULL, finished_at BIGINT NOT NULL DEFAULT 0, outcome TEXT NOT NULL, PRIMARY KEY(task_id, attempt))`,
			`CREATE INDEX IF NOT EXISTS worker_attempts_window ON worker_attempts(started_at,gate_id)`,
			`CREATE TABLE IF NOT EXISTS worker_activity (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, gate_id BIGINT NOT NULL, attempt INTEGER NOT NULL, at BIGINT NOT NULL, reason TEXT NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS worker_activity_task ON worker_activity(task_id, at)`,
			`CREATE TABLE IF NOT EXISTS worker_communication (bucket BIGINT NOT NULL,gate_id BIGINT NOT NULL,route TEXT NOT NULL,requests BIGINT NOT NULL,received BIGINT NOT NULL,sent BIGINT NOT NULL,errors BIGINT NOT NULL,PRIMARY KEY(bucket,gate_id,route))`,
		} {
			if _, err := q.DB.ExecContext(ctx, s); err != nil {
				return err
			}
		}
		rows, err := q.DB.QueryContext(ctx, `SELECT candidate_at FROM worker_tasks LIMIT 0`)
		if err == nil {
			if err = rows.Close(); err != nil {
				return err
			}
		} else {
			if _, err = q.DB.ExecContext(ctx, `ALTER TABLE worker_tasks ADD COLUMN candidate_at BIGINT NOT NULL DEFAULT 0`); err != nil {
				return err
			}
		}
		rows, err = q.DB.QueryContext(ctx, `SELECT progress FROM worker_tasks LIMIT 0`)
		if err == nil {
			err = rows.Close()
		} else {
			_, err = q.DB.ExecContext(ctx, `ALTER TABLE worker_tasks ADD COLUMN progress TEXT NOT NULL DEFAULT ''`)
		}
		if err != nil {
			return err
		}
		rows, err = q.DB.QueryContext(ctx, `SELECT result_bytes FROM worker_attempts LIMIT 0`)
		if err == nil {
			err = rows.Close()
		} else {
			_, err = q.DB.ExecContext(ctx, `ALTER TABLE worker_attempts ADD COLUMN result_bytes BIGINT`)
		}
		if err != nil {
			return err
		}
		if _, err = q.DB.ExecContext(ctx, `UPDATE worker_attempts SET outcome='failed' WHERE outcome='pending' AND finished_at>0`); err != nil {
			return err
		}
		_, err = q.DB.ExecContext(ctx, `UPDATE worker_tasks SET candidate_at=created_at WHERE candidate_at=0`)
		return err
	})
}

func token() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (q *Queue) Enqueue(ctx context.Context, req Request, encrypted string, now time.Time) (r Record, err error) {
	if err = req.Requirements.Validate(); err != nil {
		return r, err
	}
	if req.Key == "" || len(req.Key) > 256 || len(req.TraceID) > 64 || !req.Deadline.After(now) || !contracts.KnownTask(req.Kind) || len(req.Candidates) == 0 || len(req.Candidates) > 1024 || encrypted == "" {
		return r, errors.New("invalid task request")
	}
	if err = contracts.ValidateTaskPayload(req.Kind, contracts.TaskVersion, req.Payload); err != nil {
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
		var id, kind, requirements, hash string
		sum := sha256.Sum256(req.Payload)
		wantHash := hex.EncodeToString(sum[:])
		reqJSON, _ := json.Marshal(req.Requirements)
		e := q.DB.QueryRowContext(ctx, `SELECT id, kind, requirements, payload_hash FROM worker_tasks WHERE idem=$1`, req.Key).Scan(&id, &kind, &requirements, &hash)
		if e == nil {
			// Compare plaintext hashes, since encryption may be randomized.
			if kind != req.Kind || requirements != string(reqJSON) || hash != wantHash {
				return ErrConflict
			}
			r, e = q.Get(ctx, id)
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		id = token()
		candidates, _ := json.Marshal(req.Candidates)
		tx, e := q.DB.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		_, e = tx.ExecContext(ctx, `INSERT INTO worker_tasks(id,kind,version,trace_id,idem,payload_hash,body,requirements,candidates,deadline,created_at,status,legacy_id,candidate_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'pending',$12,$11)`, id, req.Kind, contracts.TaskVersion, req.TraceID, req.Key, wantHash, encrypted, string(reqJSON), string(candidates), req.Deadline.UnixMilli(), now.UnixMilli(), req.LegacyID)
		if e != nil {
			return e
		}
		reason := "capability_candidate_order"
		if req.Requirements.TargetGateID != 0 {
			reason = "resource_bound"
		} else if req.Requirements.PreferredGateID == req.Candidates[0] {
			reason = "preferred_gate"
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO worker_activity(id,task_id,gate_id,attempt,at,reason) VALUES($1,$2,$3,0,$4,$5)`, token(), id, req.Candidates[0], now.UnixMilli(), reason); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		r, e = q.Get(ctx, id)
		return e
	})
	return r, err
}

const columns = `id,kind,version,trace_id,idem,body,requirements,candidates,deadline,created_at,status,gate_id,attempt,lease_token,lease_until,result,error,legacy_id,candidate_at,progress`

func scan(row interface{ Scan(...any) error }) (r Record, err error) {
	var body, req, candidates, result, failure, progress string
	var deadline, created, lease, candidate int64
	err = row.Scan(&r.Task.ID, &r.Task.Kind, &r.Task.Version, &r.Task.TraceID, &r.Task.IdempotencyKey, &body, &req, &candidates, &deadline, &created, &r.Status, &r.GateID, &r.Task.Attempt, &r.Task.LeaseToken, &lease, &result, &failure, &r.LegacyID, &candidate, &progress)
	if err != nil {
		return r, err
	}
	// Payload is ciphertext until the host has authenticated the worker.
	r.Ciphertext = body
	if err = json.Unmarshal([]byte(req), &r.Task.Requirements); err != nil {
		return r, err
	}
	if err = json.Unmarshal([]byte(candidates), &r.Candidates); err != nil {
		return r, err
	}
	r.Task.Deadline = time.UnixMilli(deadline).UTC()
	r.Task.LeaseUntil = time.UnixMilli(lease).UTC()
	r.CreatedAt = time.UnixMilli(created).UTC()
	if candidate == 0 {
		candidate = created
	}
	r.CandidateAt = time.UnixMilli(candidate).UTC()
	if result != "" {
		r.Result = json.RawMessage(result)
	}
	if failure != "" {
		err = json.Unmarshal([]byte(failure), &r.Error)
	}
	if err == nil && progress != "" {
		err = json.Unmarshal([]byte(progress), &r.Progress)
	}
	return r, err
}

func (q *Queue) Get(ctx context.Context, id string) (Record, error) {
	return scan(q.DB.QueryRowContext(ctx, `SELECT `+columns+` FROM worker_tasks WHERE id=$1`, id))
}

// Claim serializes capacity checks across center instances, not just requests
// handled by the same process. The SQL predicate also fences stale readers.
func (q *Queue) Claim(ctx context.Context, gate int, caps contracts.CapabilityDocument, res contracts.WorkerResources, now time.Time) (out []Record, err error) {
	if err = caps.Validate(); err != nil {
		return nil, err
	}
	if res.Validate() != nil {
		return nil, errors.New("invalid worker concurrency")
	}
	err = q.Lock.Do(ctx, "worker-dispatch", func(ctx context.Context) error {
		var active int
		if e := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM worker_tasks WHERE gate_id=$1 AND status IN ('leased','cancelling')`, gate).Scan(&active); e != nil {
			return e
		}
		if active >= res.Concurrency {
			return nil
		}
		var downloads int
		if err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM worker_tasks WHERE gate_id=$1 AND status IN ('leased','cancelling') AND kind IN ($2,$3,$4,$5)`, gate, contracts.ActionAssetFetch, contracts.ActionReleaseReplace, contracts.TaskLayaInstall, contracts.TaskCoreInstall).Scan(&downloads); err != nil {
			return err
		}
		rows, e := q.DB.QueryContext(ctx, `SELECT `+columns+` FROM worker_tasks WHERE status='pending' AND deadline>$1 ORDER BY created_at,id LIMIT 100`, now.UnixMilli())
		if e != nil {
			return e
		}
		var pending []Record
		for rows.Next() {
			r, e := scan(rows)
			if e != nil {
				rows.Close()
				return e
			}
			pending = append(pending, r)
		}
		e = rows.Err()
		rows.Close()
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
			lease := now.Add(contracts.TaskLease)
			if lease.After(r.Task.Deadline) {
				lease = r.Task.Deadline
			}
			lt := token()
			tx, e := q.DB.BeginTx(ctx, nil)
			if e != nil {
				return e
			}
			v, e := tx.ExecContext(ctx, `UPDATE worker_tasks SET status='leased',gate_id=$1,attempt=attempt+1,lease_token=$2,lease_until=$3,progress='' WHERE id=$4 AND status='pending'`, gate, lt, lease.UnixMilli(), r.Task.ID)
			if e != nil {
				tx.Rollback()
				return e
			}
			n, _ := v.RowsAffected()
			if n != 1 {
				tx.Rollback()
				continue
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO worker_attempts(task_id,attempt,gate_id,started_at,outcome) VALUES($1,$2,$3,$4,'leased')`, r.Task.ID, r.Task.Attempt+1, gate, now.UnixMilli())
			if e != nil {
				tx.Rollback()
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO worker_activity(id,task_id,gate_id,attempt,at,reason) VALUES($1,$2,$3,$4,$5,'lease_claimed')`, token(), r.Task.ID, gate, r.Task.Attempt+1, now.UnixMilli()); e != nil {
				tx.Rollback()
				return e
			}
			if e = tx.Commit(); e != nil {
				return e
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

func eligible(r Record, gate int, c contracts.CapabilityDocument, res contracts.WorkerResources) bool {
	if !r.Task.Requirements.Matches(gate, r.Task.Kind, r.Task.Version, c, res) {
		return false
	}
	// Candidate order is chosen by the center. Only the first candidate may
	// claim; expiration or a retryable failure advances that order explicitly.
	return len(r.Candidates) > 0 && r.Candidates[0] == gate
}
