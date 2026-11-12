package taskqueue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

func TestProgressRequiresCurrentLeaseAndMetadataOnly(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)
	r, err := q.Enqueue(ctx, request(contracts.ActionSourceProbe, "progress", now), "encrypted", now)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := q.Claim(ctx, 1, capabilities(r.Task.Kind), contracts.WorkerResources{Concurrency: 1}, now)
	if err != nil || len(rs) != 1 {
		t.Fatalf("claim: %v %v", rs, err)
	}
	u := update(rs[0])
	u.Result = nil
	u.Progress = &contracts.TaskProgress{Stage: "executing", Done: 2}
	if err = q.Progress(ctx, r.Task.ID, 2, u, now); !errors.Is(err, taskqueue.ErrLease) {
		t.Fatalf("wrong gate: %v", err)
	}
	if err = q.Progress(ctx, r.Task.ID, 1, u, now); err != nil {
		t.Fatal(err)
	}
	got, _ := q.Get(ctx, r.Task.ID)
	if got.Progress == nil || got.Progress.Done != 2 {
		t.Fatalf("missing progress: %+v", got)
	}
	u.Progress.Stage = "https://secret.invalid/token"
	if err = q.Progress(ctx, r.Task.ID, 1, u, now); err == nil {
		t.Fatal("untrusted stage accepted")
	}
	u.Progress.Stage = "executing"
	if err = q.Progress(ctx, r.Task.ID, 1, u, now.Add(61*time.Second)); !errors.Is(err, taskqueue.ErrLease) {
		t.Fatalf("expired lease: %v", err)
	}
	if err = q.Sweep(ctx, now.Add(61*time.Second)); err != nil {
		t.Fatal(err)
	}
	rs, err = q.Claim(ctx, 2, capabilities(r.Task.Kind), contracts.WorkerResources{Concurrency: 1}, now.Add(62*time.Second))
	if err != nil || len(rs) != 1 || rs[0].Progress != nil {
		t.Fatalf("old attempt progress carried forward: %+v %v", rs, err)
	}
}
