package gate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/packages/contracts"
)

func configurationContext(ctx context.Context, cfg contracts.GateConfig) context.Context {
	return releasefiles.WithTrace(ctx, "00-"+cfg.TraceID+"-1111111111111111-01")
}

// Configuration execution has the same bounded metadata history as tasks. It
// never stores the configuration, whose credentials belong only in State.
func (a *Agent) applySyncedConfig(ctx context.Context, cfg contracts.GateConfig) (state contracts.GateState, err error) {
	if releasefiles.Trace(ctx) == "" {
		ctx = configurationContext(ctx, cfg)
	}
	trace := releasefiles.Trace(ctx)
	id := sha256.Sum256([]byte(fmt.Sprintf("config:%d:%s", cfg.Revision, trace)))
	taskID := hex.EncodeToString(id[:24])
	stages := &taskStageLog{task: contracts.Task{ID: taskID, Kind: "config.apply", TraceID: trace, Attempt: 1}}
	ctx = context.WithValue(ctx, taskStageKey{}, stages)
	finish := beginTaskStage(ctx, "config.apply", "gate.applySyncedConfig")
	defer func() {
		failure := err
		if failure == nil && state.FailedRevision != 0 {
			failure = &contracts.TaskError{Code: "configuration_rejected", Origin: "gate.applyLocked"}
		}
		finish(failure)
		status := "succeeded"
		if failure != nil {
			status = "failed"
		}
		dir := filepath.Join(a.P.Data, "tasks")
		if recordErr := prepareJournals(dir, time.Now()); recordErr == nil {
			stages.mu.Lock()
			records := append([]contracts.TaskStageRecord(nil), stages.records...)
			stages.mu.Unlock()
			recordErr = writeJournal(filepath.Join(dir, taskID+".json"), taskJournal{Kind: "config.apply", Key: fmt.Sprintf("config:%d", cfg.Revision), Status: status, Deadline: time.Now().UTC(), Stages: records})
			if recordErr != nil {
				log.Printf("configuration history unavailable revision=%d", cfg.Revision)
			}
		} else {
			log.Printf("configuration history capacity unavailable revision=%d", cfg.Revision)
		}
	}()
	if cfg.ExecutionOnly {
		return a.applyExecution(ctx, cfg), nil
	}
	state, err = a.applyLocked(ctx, cfg)
	state.ExecutionSeq = cfg.ExecutionSeq
	return state, err
}
