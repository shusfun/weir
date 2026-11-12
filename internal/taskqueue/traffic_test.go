package taskqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/taskqueue"
)

func TestCommunicationAtomicAggregationAndIsolation(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)
	entry := taskqueue.Communication{Bucket: now, GateID: 1, Route: "/agent/poll", Requests: 1, Received: 31, Sent: 72, Errors: 1}
	if err := q.AddCommunication(ctx, []taskqueue.Communication{entry, entry, {Bucket: now, GateID: 2, Route: entry.Route, Requests: 1, Sent: 100}}); err != nil {
		t.Fatal(err)
	}
	got, err := q.Communication(ctx, now)
	if err != nil || len(got) != 2 || got[0].Requests != 2 || got[0].Received != 62 || got[0].Sent != 144 || got[0].Errors != 2 || got[1].Sent != 100 {
		t.Fatalf("aggregation: %+v %v", got, err)
	}
	bad := entry
	bad.GateID = -1
	if err = q.AddCommunication(ctx, []taskqueue.Communication{entry, bad}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	got, err = q.Communication(ctx, now)
	if err != nil || got[0].Requests != 2 {
		t.Fatalf("failed batch was partially saved: %+v %v", got, err)
	}
	got, err = q.Communication(ctx, now.Add(time.Minute))
	if err != nil || len(got) != 0 {
		t.Fatalf("range not applied: %+v %v", got, err)
	}
}
