package taskqueue_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

func queue(t *testing.T) *taskqueue.Queue {
	t.Helper()
	s, e := store.Open(context.Background(), store.Config{Driver: store.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "queue.db")})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s.Tasks
}
func request(kind, key string, now time.Time) taskqueue.Request {
	r := taskqueue.Request{Kind: kind, Key: key, TraceID: "012345678901234567890123456789ab", Candidates: []int{1, 2}, Deadline: now.Add(time.Hour), Payload: json.RawMessage(`{"url":"https://github.com/example/repo","bytes":1024}`)}
	if kind == contracts.ActionCoreRestart {
		r.Candidates = []int{1}
		r.Requirements.TargetGateID = 1
		r.Payload = json.RawMessage(`{"kernel":"xray"}`)
	}
	return r
}
func capabilities(kind string) contracts.CapabilityDocument {
	return contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "1.0.0", Arch: "amd64", Tasks: map[string]int{kind: 1}}
}
func update(r taskqueue.Record) contracts.TaskUpdate {
	return contracts.TaskUpdate{Attempt: r.Task.Attempt, LeaseToken: r.Task.LeaseToken, Result: json.RawMessage(`{"ok":true}`)}
}

func TestLeaseFailoverFencesOldWorker(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)
	r, e := q.Enqueue(ctx, request(contracts.ActionSourceProbe, "failover", now), "encrypted", now)
	if e != nil {
		t.Fatal(e)
	}
	first, e := q.Claim(ctx, 1, capabilities(r.Task.Kind), contracts.WorkerResources{Concurrency: 1}, now)
	if e != nil || len(first) != 1 {
		t.Fatalf("claim: %v %v", first, e)
	}
	if e = q.Sweep(ctx, now.Add(61*time.Second)); e != nil {
		t.Fatal(e)
	}
	second, e := q.Claim(ctx, 2, capabilities(r.Task.Kind), contracts.WorkerResources{Concurrency: 1}, now.Add(62*time.Second))
	if e != nil || len(second) != 1 {
		t.Fatalf("failover: %v %v", second, e)
	}
	if e = q.Finish(ctx, r.Task.ID, 1, update(first[0]), now.Add(63*time.Second)); !errors.Is(e, taskqueue.ErrLease) {
		t.Fatalf("accepted stale result: %v", e)
	}
	if e = q.Finish(ctx, r.Task.ID, 2, update(second[0]), now.Add(63*time.Second)); e != nil {
		t.Fatal(e)
	}
	got, e := q.Get(ctx, r.Task.ID)
	if e != nil || got.Status != "succeeded" || got.Task.Attempt != 2 {
		t.Fatalf("result: %+v %v", got, e)
	}
}

func TestUnclaimedReadTaskMovesToNextCandidateButBoundTaskStays(t *testing.T) {
	q := queue(t)
	ctx := t.Context()
	now := time.Now().Truncate(time.Millisecond)
	read, err := q.Enqueue(ctx, request(contracts.ActionSourceProbe, "pickup", now), "sealed", now)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := q.Enqueue(ctx, request(contracts.ActionCoreRestart, "bound", now), "sealed", now)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := q.SweepChanged(ctx, now.Add(61*time.Second))
	if err != nil || changed != 1 {
		t.Fatalf("sweep=%d %v", changed, err)
	}
	got, _ := q.Get(ctx, read.Task.ID)
	if len(got.Candidates) != 1 || got.Candidates[0] != 2 {
		t.Fatalf("no pickup failover: %+v", got)
	}
	still, _ := q.Get(ctx, bound.Task.ID)
	if still.Candidates[0] != 1 || still.Status != "pending" {
		t.Fatalf("bound task moved: %+v", still)
	}
	events, err := q.Activity(ctx, read.Task.ID)
	if err != nil || len(events) != 2 || events[1].Reason != "pickup_timeout_reassigned" || events[1].GateID != 2 {
		t.Fatalf("activity=%+v %v", events, err)
	}
}

func TestConcurrentClaimAndCapacity(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	now := time.Now()
	for _, key := range []string{"a", "b"} {
		if _, e := q.Enqueue(ctx, request(contracts.ActionSourceProbe, key, now), "encrypted", now); e != nil {
			t.Fatal(e)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	claimed := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rs, e := q.Claim(ctx, 1, capabilities(contracts.ActionSourceProbe), contracts.WorkerResources{Concurrency: 1}, now)
			if e != nil {
				t.Error(e)
			}
			mu.Lock()
			claimed += len(rs)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if claimed != 1 {
		t.Fatalf("capacity exceeded: %d", claimed)
	}
}

func TestBoundTaskCannotMoveAndCancellationStopsRenewal(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)
	r, e := q.Enqueue(ctx, request(contracts.ActionCoreRestart, "restart", now), "encrypted", now)
	if e != nil {
		t.Fatal(e)
	}
	rs, e := q.Claim(ctx, 2, capabilities(r.Task.Kind), contracts.WorkerResources{Concurrency: 1}, now)
	if e != nil || len(rs) != 0 {
		t.Fatal("bound task moved")
	}
	rs, e = q.Claim(ctx, 1, capabilities(r.Task.Kind), contracts.WorkerResources{Concurrency: 1}, now)
	if e != nil || len(rs) != 1 {
		t.Fatal(e)
	}
	if e = q.Cancel(ctx, r.Task.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Renew(ctx, r.Task.ID, 1, update(rs[0]), now.Add(time.Second)); !errors.Is(e, taskqueue.ErrLease) {
		t.Fatalf("cancelled task renewed: %v", e)
	}
	if e = q.Sweep(ctx, now.Add(61*time.Second)); e != nil {
		t.Fatal(e)
	}
	got, _ := q.Get(ctx, r.Task.ID)
	if got.Status != "cancelled" {
		t.Fatal(got.Status)
	}
	r, e = q.Enqueue(ctx, request(contracts.ActionCoreRestart, "unknown", now), "encrypted", now)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Claim(ctx, 1, capabilities(r.Task.Kind), contracts.WorkerResources{Concurrency: 1}, now); e != nil {
		t.Fatal(e)
	}
	if e = q.Sweep(ctx, now.Add(61*time.Second)); e != nil {
		t.Fatal(e)
	}
	got, _ = q.Get(ctx, r.Task.ID)
	if got.Status != "unknown" {
		t.Fatal(got.Status)
	}
}

func TestIdempotencyAndCapabilityFiltering(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	now := time.Now()
	req := request(contracts.ActionSourceProbe, "same", now)
	a, e := q.Enqueue(ctx, req, "encrypted", now)
	if e != nil {
		t.Fatal(e)
	}
	b, e := q.Enqueue(ctx, req, "other-ciphertext", now)
	if e != nil || a.Task.ID != b.Task.ID {
		t.Fatalf("duplicate: %v", e)
	}
	req.Payload = json.RawMessage(`{"url":"https://github.com/other/repo","bytes":1024}`)
	if _, e = q.Enqueue(ctx, req, "encrypted", now); !errors.Is(e, taskqueue.ErrConflict) {
		t.Fatal(e)
	}
	rs, e := q.Claim(ctx, 1, capabilities(contracts.ActionCoreRestart), contracts.WorkerResources{Concurrency: 1}, now)
	if e != nil || len(rs) != 0 {
		t.Fatal("unsupported work dispatched")
	}
	bad := request(contracts.ActionCoreRestart, "unbound", now)
	bad.Requirements.TargetGateID = 0
	if _, e = q.Enqueue(ctx, bad, "encrypted", now); e == nil {
		t.Fatal("unbound mutation accepted")
	}
}

func TestCancellationAcknowledgementIsFencedAndIdempotent(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)
	r, err := q.Enqueue(ctx, request(contracts.ActionCoreRestart, "cancel-ack", now), "encrypted", now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := q.Claim(ctx, 1, capabilities(r.Task.Kind), contracts.WorkerResources{Concurrency: 1}, now)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v", err)
	}
	u := update(claimed[0])
	u.Result = nil
	if err = q.AcknowledgeCancel(ctx, r.Task.ID, 1, u, now); !errors.Is(err, taskqueue.ErrLease) {
		t.Fatalf("unsolicited ack: %v", err)
	}
	if err = q.Cancel(ctx, r.Task.ID); err != nil {
		t.Fatal(err)
	}
	if err = q.AcknowledgeCancel(ctx, r.Task.ID, 2, u, now); !errors.Is(err, taskqueue.ErrLease) {
		t.Fatalf("wrong worker: %v", err)
	}
	bad := u
	bad.Attempt++
	if err = q.AcknowledgeCancel(ctx, r.Task.ID, 1, bad, now); !errors.Is(err, taskqueue.ErrLease) {
		t.Fatalf("wrong attempt: %v", err)
	}
	for range 2 {
		if err = q.AcknowledgeCancel(ctx, r.Task.ID, 1, u, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := q.Cancellations(ctx, 1)
	if err != nil || len(ids) != 0 {
		t.Fatalf("cancel still delivered: %v %v", ids, err)
	}
	if err = q.Finish(ctx, r.Task.ID, 1, update(claimed[0]), now.Add(time.Second)); !errors.Is(err, taskqueue.ErrLease) {
		t.Fatalf("cancelled attempt committed: %v", err)
	}
}

func TestTaskCannotReturnBothResultAndError(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	now := time.Now()
	r, err := q.Enqueue(ctx, request(contracts.ActionSourceProbe, "ambiguous-result", now), "encrypted", now)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := q.Claim(ctx, 1, capabilities(r.Task.Kind), contracts.WorkerResources{Concurrency: 1}, now)
	if err != nil || len(rs) != 1 {
		t.Fatal(err)
	}
	u := update(rs[0])
	u.Error = &contracts.TaskError{Code: "failed"}
	if err = q.Finish(ctx, r.Task.ID, 1, u, now); err == nil {
		t.Fatal("ambiguous result accepted")
	}
	got, _ := q.Get(ctx, r.Task.ID)
	if got.Status != "leased" {
		t.Fatal(got.Status)
	}
}
