package store

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

func TestPostgresWorkerClaimAndLeaseFence(t *testing.T) {
	queue := openPostgresForTest(t).Tasks
	ctx := t.Context()
	now := time.Now().Truncate(time.Millisecond)
	kind := contracts.ActionSourceProbe
	task, err := queue.Enqueue(ctx, taskqueue.Request{
		Kind: kind, Key: "concurrent-pg", Candidates: []int{1, 2}, Deadline: now.Add(time.Hour),
		Payload: json.RawMessage(`{"url":"https://example.test"}`),
	}, "sealed", now)
	if err != nil {
		t.Fatal(err)
	}
	caps := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: map[string]int{kind: 1}}
	resources := contracts.WorkerResources{Concurrency: 1}
	var claimers sync.WaitGroup
	claims := make(chan taskqueue.Record, 8)
	for range cap(claims) {
		claimers.Go(func() {
			batch, err := queue.Claim(ctx, 1, caps, resources, now)
			if err != nil {
				t.Error(err)
				return
			}
			for _, claim := range batch {
				claims <- claim
			}
		})
	}
	claimers.Wait()
	if len(claims) != 1 {
		t.Fatalf("want one lease, got %d", len(claims))
	}
	first := <-claims
	if err = queue.Sweep(ctx, now.Add(61*time.Second)); err != nil {
		t.Fatal(err)
	}
	second, err := queue.Claim(ctx, 2, caps, resources, now.Add(62*time.Second))
	if err != nil || len(second) != 1 || second[0].Task.Attempt <= first.Task.Attempt {
		t.Fatalf("replacement lease: count=%d err=%v", len(second), err)
	}
	receipt := func(record taskqueue.Record) contracts.TaskUpdate {
		return contracts.TaskUpdate{Attempt: record.Task.Attempt, LeaseToken: record.Task.LeaseToken, Result: json.RawMessage(`{"ok":true}`)}
	}
	if err = queue.Finish(ctx, task.Task.ID, 1, receipt(first), now.Add(63*time.Second)); !errors.Is(err, taskqueue.ErrLease) {
		t.Fatalf("old worker receipt: %v", err)
	}
	if err = queue.Finish(ctx, task.Task.ID, 2, receipt(second[0]), now.Add(63*time.Second)); err != nil {
		t.Fatalf("current worker receipt: %v", err)
	}
}
