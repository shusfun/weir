package gate

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

type taskStageKey struct{}

// Lease credentials live only in the execution context, never in stage records.
type taskLeaseKey struct{}
type taskLease struct {
	id      string
	attempt int
	token   string
}

type taskStageLog struct {
	mu      sync.Mutex
	task    contracts.Task
	records []contracts.TaskStageRecord
}

func (l *taskStageLog) add(stage, origin, outcome, code string, started, finished time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.records) >= 63 {
		return
	}
	l.records = append(l.records, contracts.TaskStageRecord{TaskID: l.task.ID, TraceID: l.task.TraceID, Kind: l.task.Kind, Attempt: l.task.Attempt, Stage: stage, Origin: origin, Outcome: outcome, ErrorCode: code, Started: started, Finished: finished})
}

func beginTaskStage(ctx context.Context, stage, origin string) func(error) {
	started := time.Now().UTC()
	return func(err error) {
		log, _ := ctx.Value(taskStageKey{}).(*taskStageLog)
		if log == nil {
			return
		}
		outcome, code := "succeeded", ""
		if err != nil {
			outcome, code = "failed", "operation_failed"
			var detail *contracts.TaskError
			if errors.As(err, &detail) {
				code = detail.Code
			}
		}
		if errors.Is(err, context.Canceled) {
			outcome, code = "cancelled", "context_cancelled"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			outcome, code = "timeout", "deadline_exceeded"
		}
		log.add(stage, origin, outcome, code, started, time.Now().UTC())
	}
}

func layaTaskObserver(ctx context.Context) laya.Observer {
	return func(_ context.Context, e laya.Event) {
		log, _ := ctx.Value(taskStageKey{}).(*taskStageLog)
		if log == nil {
			return
		}
		origin := "laya.Client.Classify"
		switch e.Operation {
		case "laya.runtime.version":
			origin = "laya.Client.RuntimeVersion"
		case "laya.model.show":
			origin = "laya.Client.ShowModel"
		case "laya.model.pull":
			origin = "laya.Client.PullModel"
		}
		log.add(e.Operation, origin, e.Status, e.Code, e.Started, e.Finished)
	}
}

func readTaskStages(ctx context.Context, dir string, req contracts.TraceReadTask) (contracts.TraceReadResult, error) {
	out := contracts.TraceReadResult{Records: []contracts.TaskStageRecord{}, CollectedAt: time.Now().UTC(), Complete: false}
	if id, err := hex.DecodeString(req.TraceID); err != nil || len(id) != 16 || req.TraceID == strings.Repeat("0", 32) || req.Limit < 1 || req.Limit > 200 || req.After < 0 {
		return out, errors.New("trace_request_invalid")
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		out.MissingReason = "no_local_records"
		return out, nil
	}
	if err != nil {
		return out, err
	}
	corrupt := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var journal taskJournal
		if err := readJournal(filepath.Join(dir, entry.Name()), &journal); err != nil {
			corrupt = true
			continue
		}
		for _, record := range journal.Stages {
			if record.TraceID == req.TraceID && record.Started.UnixMilli() > req.After {
				out.Records = append(out.Records, record)
			}
		}
	}
	sort.Slice(out.Records, func(i, j int) bool {
		if out.Records[i].Started.Equal(out.Records[j].Started) {
			return out.Records[i].TaskID < out.Records[j].TaskID
		}
		return out.Records[i].Started.Before(out.Records[j].Started)
	})
	switch {
	case len(out.Records) > req.Limit:
		out.Records = out.Records[:req.Limit]
		out.MissingReason = "record_limit"
	case corrupt:
		out.MissingReason = "local_record_unreadable"
	case len(out.Records) == 0:
		out.MissingReason = "no_local_records"
	default:
		// Local retention cannot prove that an entire distributed operation
		// remains available; only the collected records are authoritative.
		out.MissingReason = "bounded_local_history"
	}
	return out, nil
}
