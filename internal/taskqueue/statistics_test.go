package taskqueue_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func TestStatisticsIncludesRetriesAndUnknownMeasurements(t *testing.T) {
	q := queue(t)
	ctx := t.Context()
	now := time.Now().Truncate(time.Millisecond)
	req := request(contracts.ActionSourceProbe, "statistics-retry", now)
	req.Candidates = []int{1, 2, 3}
	r, err := q.Enqueue(ctx, req, "sealed", now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := q.Claim(ctx, 1, capabilities(r.Task.Kind), contracts.WorkerResources{Concurrency: 1}, now)
	if err != nil || len(first) != 1 {
		t.Fatalf("claim: %v %v", first, err)
	}
	u := update(first[0])
	u.Result = nil
	u.Error = &contracts.TaskError{Code: "upstream_unavailable", Retryable: true}
	if err = q.FinishMeasured(ctx, r.Task.ID, 1, u, now.Add(time.Second), 0); err != nil {
		t.Fatal(err)
	}
	// A pending task can advance again without changing a finished attempt.
	if err = q.Sweep(ctx, now.Add(62*time.Second)); err != nil {
		t.Fatal(err)
	}
	second, err := q.Claim(ctx, 3, capabilities(r.Task.Kind), contracts.WorkerResources{Concurrency: 1}, now.Add(63*time.Second))
	if err != nil || len(second) != 1 {
		t.Fatalf("retry: %v %v", second, err)
	}
	if err = q.Finish(ctx, r.Task.ID, 3, update(second[0]), now.Add(65*time.Second)); err != nil {
		t.Fatal(err)
	}
	stats, err := q.Statistics(ctx, now.Add(-time.Second), now.Add(time.Minute*2))
	if err != nil || len(stats.Items) != 2 {
		t.Fatalf("statistics: %+v %v", stats, err)
	}
	a, b := stats.Items[0], stats.Items[1]
	if a.Failed != 1 || a.Succeeded != 0 || a.Finished != 1 || a.DurationMS != 1000 || a.Measured != 1 || a.ResultBytes == nil || *a.ResultBytes != 0 {
		t.Fatalf("failed attempt was overwritten: %+v", a)
	}
	if b.Succeeded != 1 || b.DurationMS != 2000 || b.ResultBytes != nil || b.Measured != 0 || stats.States["succeeded"] != 1 {
		t.Fatalf("legacy measurement must be unknown: %+v %+v", b, stats.States)
	}
}

func TestStatisticsIsNotTruncatedToRecentList(t *testing.T) {
	q := queue(t)
	ctx := t.Context()
	now := time.Now().Truncate(time.Millisecond)
	r, err := q.Enqueue(ctx, request(contracts.ActionSourceProbe, "many-attempts", now), "sealed", now)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 251; attempt++ {
		_, err = q.DB.ExecContext(ctx, `INSERT INTO worker_attempts(task_id,attempt,gate_id,started_at,finished_at,outcome,result_bytes) VALUES($1,$2,1,$3,$4,'succeeded',10)`, r.Task.ID, attempt, now.UnixMilli(), now.Add(time.Second).UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
	}
	stats, err := q.Statistics(ctx, now.Add(-time.Second), now.Add(time.Minute))
	if err != nil || len(stats.Items) != 1 || stats.Items[0].Attempts != 251 || stats.Items[0].ResultBytes == nil || *stats.Items[0].ResultBytes != 2510 {
		body, _ := json.Marshal(stats)
		t.Fatalf("truncated aggregate: %s %v", body, err)
	}
	if _, err = q.Statistics(ctx, now, now.Add(15*24*time.Hour)); err == nil {
		t.Fatal("unbounded statistics window accepted")
	}
}
