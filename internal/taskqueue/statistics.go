package taskqueue

import (
	"context"
	"errors"
	"sort"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/predicate"
	"github.com/shusfun/weir/internal/store/ent/workerattempt"
	"github.com/shusfun/weir/internal/store/ent/workertask"
)

type AttemptStatistics struct {
	GateID       int      `json:"gate_id"`
	Kind         string   `json:"kind"`
	Attempts     int64    `json:"attempts"`
	Active       int64    `json:"active"`
	Succeeded    int64    `json:"succeeded"`
	Failed       int64    `json:"failed"`
	Cancelled    int64    `json:"cancelled"`
	Unknown      int64    `json:"unknown"`
	Finished     int64    `json:"finished"`
	DurationMS   int64    `json:"duration_ms"`
	Measured     int64    `json:"measured_results"`
	ResultBytes  *int64   `json:"result_bytes"`
	MeanDuration *float64 `json:"mean_duration_ms"`
	FailureRate  *float64 `json:"failure_rate"`
}

type Statistics struct {
	From   time.Time           `json:"from"`
	Until  time.Time           `json:"until"`
	Items  []AttemptStatistics `json:"items"`
	States map[string]int64    `json:"task_states"`
}

// attemptGroup 是按 Gate、任务种类（以及可选的结果）分组的尝试聚合。
type attemptGroup struct {
	GateID   int              `json:"gate_id"`
	Kind     string           `json:"kind"`
	Outcome  string           `json:"outcome"`
	N        int64            `json:"n"`
	Measured int64            `json:"measured"`
	Bytes    entsql.NullInt64 `json:"bytes"`
	Duration entsql.NullInt64 `json:"duration"`
}

type statKey struct {
	gate int
	kind string
}

// groupAttempts 统计窗口内（按尝试开始时间）的尝试，并连接任务表取种类。
func (q *Queue) groupAttempts(ctx context.Context, from, until time.Time, byOutcome bool, where []predicate.WorkerAttempt, aggs func(*entsql.Selector) []string) (out []attemptGroup, err error) {
	where = append(where, workerattempt.StartedAtGTE(from.UnixMilli()), workerattempt.StartedAtLT(until.UnixMilli()))
	err = q.Ent.WorkerAttempt.Query().Where(where...).Modify(func(s *entsql.Selector) {
		t := entsql.Dialect(s.Dialect()).Table(workertask.Table)
		s.Join(t).On(s.C(workerattempt.FieldID), t.C(workertask.FieldID))
		group := []string{s.C(workerattempt.FieldGateID), t.C(workertask.FieldKind)}
		if byOutcome {
			group = append(group, s.C(workerattempt.FieldOutcome))
		}
		s.Select(append(append([]string{}, group...), aggs(s)...)...).GroupBy(group...)
	}).Scan(ctx, &out)
	return out, err
}

func countAll(*entsql.Selector) []string { return []string{entsql.As(entsql.Count("*"), "n")} }

// Statistics aggregates persisted attempts, including retries, instead of a
// truncated recent-task list. The window is defined by attempt start time.
func (q *Queue) Statistics(ctx context.Context, from, until time.Time) (out Statistics, err error) {
	if !until.After(from) || until.Sub(from) > 14*24*time.Hour {
		return out, errors.New("invalid statistics window")
	}
	out = Statistics{From: from.UTC(), Until: until.UTC(), Items: []AttemptStatistics{}, States: map[string]int64{}}
	items := map[statKey]*AttemptStatistics{}
	bytes := map[statKey]int64{}
	item := func(g attemptGroup) *AttemptStatistics {
		k := statKey{g.GateID, g.Kind}
		if items[k] == nil {
			items[k] = &AttemptStatistics{GateID: g.GateID, Kind: g.Kind}
		}
		return items[k]
	}
	all, err := q.groupAttempts(ctx, from, until, true, nil, func(s *entsql.Selector) []string {
		return []string{
			entsql.As(entsql.Count("*"), "n"),
			entsql.As(entsql.Count(s.C(workerattempt.FieldResultBytes)), "measured"),
			entsql.As(entsql.Sum(s.C(workerattempt.FieldResultBytes)), "bytes"),
		}
	})
	if err != nil {
		return out, err
	}
	for _, g := range all {
		it := item(g)
		it.Attempts += g.N
		it.Measured += g.Measured
		bytes[statKey{g.GateID, g.Kind}] += g.Bytes.Int64
		switch g.Outcome {
		case "succeeded":
			it.Succeeded += g.N
		case "failed":
			it.Failed += g.N
		case "cancelled":
			it.Cancelled += g.N
		}
	}
	active, err := q.groupAttempts(ctx, from, until, false, []predicate.WorkerAttempt{workerattempt.FinishedAt(0)}, countAll)
	if err != nil {
		return out, err
	}
	for _, g := range active {
		item(g).Active += g.N
	}
	finished, err := q.groupAttempts(ctx, from, until, true, []predicate.WorkerAttempt{workerattempt.FinishedAtGT(0)}, countAll)
	if err != nil {
		return out, err
	}
	for _, g := range finished {
		it := item(g)
		it.Finished += g.N
		if g.Outcome != "succeeded" && g.Outcome != "failed" && g.Outcome != "cancelled" {
			it.Unknown += g.N
		}
	}
	// 时钟回拨导致结束早于开始的尝试不计入耗时。
	ordered := func(s *entsql.Selector) {
		s.Where(entsql.ColumnsGTE(s.C(workerattempt.FieldFinishedAt), s.C(workerattempt.FieldStartedAt)))
	}
	durations, err := q.groupAttempts(ctx, from, until, false, []predicate.WorkerAttempt{ordered}, func(s *entsql.Selector) []string {
		return []string{entsql.As(entsql.Sum(s.C(workerattempt.FieldFinishedAt)+" - "+s.C(workerattempt.FieldStartedAt)), "duration")}
	})
	if err != nil {
		return out, err
	}
	for _, g := range durations {
		item(g).DurationMS += g.Duration.Int64
	}
	for k, it := range items {
		if it.Measured > 0 {
			b := bytes[k]
			it.ResultBytes = &b
		}
		if it.Finished > 0 {
			mean := float64(it.DurationMS) / float64(it.Finished)
			it.MeanDuration = &mean
		}
		if known := it.Succeeded + it.Failed; known > 0 {
			rate := float64(it.Failed) / float64(known)
			it.FailureRate = &rate
		}
		out.Items = append(out.Items, *it)
	}
	sort.Slice(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		return a.GateID < b.GateID || a.GateID == b.GateID && a.Kind < b.Kind
	})
	var states []struct {
		Status string `json:"status"`
		Count  int64  `json:"count"`
	}
	if err = q.Ent.WorkerTask.Query().GroupBy(workertask.FieldStatus).Aggregate(ent.Count()).Scan(ctx, &states); err != nil {
		return out, err
	}
	for _, s := range states {
		out.States[s.Status] = s.Count
	}
	return out, nil
}
