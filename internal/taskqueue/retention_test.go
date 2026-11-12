package taskqueue_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

func TestObservationRetentionKeepsActiveWorkAndBoundsHistory(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)
	old := now.Add(-15 * 24 * time.Hour)
	rq := request(contracts.ActionSourceProbe, "old-active", old)
	rq.Deadline = now.Add(time.Hour)
	active, err := q.Enqueue(ctx, rq, "encrypted", old)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := q.Enqueue(ctx, request(contracts.ActionSourceProbe, "old-terminal", old), "encrypted", old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = q.DB.ExecContext(ctx, `UPDATE worker_tasks SET status='succeeded' WHERE id=$1`, terminal.Task.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 105; i++ {
		if err = q.AddCommunication(ctx, []taskqueue.Communication{{Bucket: now, GateID: 1, Route: fmt.Sprintf("/route/%d", i), Requests: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = q.AddCommunication(ctx, []taskqueue.Communication{{Bucket: old, GateID: 2, Route: "/old", Requests: 1}}); err != nil {
		t.Fatal(err)
	}
	if err = q.PruneObservations(ctx, now, 14*24*time.Hour, 100); err != nil {
		t.Fatal(err)
	}
	if _, err = q.Get(ctx, active.Task.ID); err != nil {
		t.Fatal("active task removed", err)
	}
	if _, err = q.Get(ctx, terminal.Task.ID); err == nil {
		t.Fatal("expired terminal task retained")
	}
	rows, err := q.Communication(ctx, old)
	if err != nil || len(rows) != 100 {
		t.Fatalf("history cap: %d %v", len(rows), err)
	}
	for _, row := range rows {
		if row.GateID == 2 {
			t.Fatal("old counters retained")
		}
	}
	if err = q.PruneObservations(ctx, now, 0, 100); err == nil {
		t.Fatal("unbounded retention accepted")
	}
}
