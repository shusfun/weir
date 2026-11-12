package gate

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

func TestJournalRejectsCorruptionAndPrunesOnlyExpiredTerminalMetadata(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for _, name := range []string{"running", "finished"} {
		status := "running"
		if name == "finished" {
			status = "succeeded"
		}
		path := filepath.Join(dir, name+".json")
		if err := writeJournal(path, taskJournal{Status: status, Deadline: now.Add(-16 * 24 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-16*24*time.Hour), now.Add(-16*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := prepareJournals(dir, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "running.json")); err != nil {
		t.Fatalf("lost uncertain side effect: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "finished.json")); !os.IsNotExist(err) {
		t.Fatal("old terminal entry retained", err)
	}
	for _, raw := range []string{`{"status":"running"} {}`, `{"status":"running","credentials":"secret"}`, "{"} {
		path := filepath.Join(dir, "broken.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if err := readJournal(path, new(taskJournal)); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestLayaTaskStagesPreserveRootAndContainOnlyMetadata(t *testing.T) {
	log := &taskStageLog{task: contracts.Task{ID: "task", TraceID: "root", Kind: contracts.TaskLayaClassify, Attempt: 2}}
	ctx := context.WithValue(t.Context(), taskStageKey{}, log)
	finish := beginTaskStage(ctx, "laya.container.status", "gate.layaOperation")
	finish(layaTaskFailure("laya_container_missing", "gate.layaOperation"))
	observer := layaTaskObserver(ctx)
	for range 100 {
		observer(ctx, laya.Event{Operation: "laya.model.show", TraceID: "must-not-replace-root", Started: time.Now(), Finished: time.Now(), Status: "failed", Code: "model_not_found"})
	}
	if len(log.records) != 63 || log.records[0].ErrorCode != "laya_container_missing" {
		t.Fatalf("%+v", log)
	}
	for _, stage := range log.records {
		if stage.TraceID != "root" || stage.TaskID != "task" || stage.Attempt != 2 || stage.Origin == "" {
			t.Fatal(stage)
		}
	}
}

func TestWorkerEnforcesLocalConcurrencyAndWaitsForCancellation(t *testing.T) {
	w := &workerRunner{running: map[string]context.CancelFunc{}}
	started := make(chan struct{})
	var stopped atomic.Bool
	if !w.start(context.Background(), contracts.Task{ID: "first", Deadline: time.Now().Add(time.Minute)}, func(ctx context.Context, _ contracts.Task) { close(started); <-ctx.Done(); stopped.Store(true) }) {
		t.Fatal("first task rejected")
	}
	<-started
	if w.start(context.Background(), contracts.Task{ID: "second", Deadline: time.Now().Add(time.Minute)}, func(context.Context, contracts.Task) { t.Error("exceeded concurrency") }) {
		t.Fatal("second task accepted")
	}
	w.stop()
	if !stopped.Load() {
		t.Fatal("stop returned before task exited")
	}
}

func TestTaskStageReadKeepsTraceBoundariesAndReportsMissingHistory(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	trace := "012345678901234567890123456789ab"
	for i, id := range []string{trace, "112345678901234567890123456789ab"} {
		if err := writeJournal(filepath.Join(dir, string(rune('a'+i))+".json"), taskJournal{Stages: []contracts.TaskStageRecord{{TaskID: "task", TraceID: id, Started: now, Finished: now, Stage: "execute", Origin: "gate.executeTask", Outcome: "succeeded"}}}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := readTaskStages(t.Context(), dir, contracts.TraceReadTask{TraceID: trace, Limit: 20})
	if err != nil || len(out.Records) != 1 || out.Records[0].TraceID != trace || out.Complete || out.MissingReason == "" {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err = readTaskStages(t.Context(), dir, contracts.TraceReadTask{TraceID: "../../secret", Limit: 20}); err == nil {
		t.Fatal("invalid trace accepted")
	}
}
