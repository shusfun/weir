package gate

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/packages/contracts"
)

type workerRunner struct {
	mu        sync.Mutex
	running   map[string]context.CancelFunc
	wg        sync.WaitGroup
	budget    contracts.WorkerBudget
	downloads map[string]bool
}

func (w *workerRunner) stop() {
	w.mu.Lock()
	for _, cancel := range w.running {
		cancel()
	}
	w.mu.Unlock()
	w.wg.Wait()
}

func (w *workerRunner) start(ctx context.Context, task contracts.Task, run func(context.Context, contracts.Task)) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Enforce the local budget even if a center sends too many tasks.
	budget := w.budget
	if budget.Validate() != nil {
		budget = contracts.DefaultWorkerBudget()
	}
	if _, exists := w.running[task.ID]; exists {
		return false
	}
	if len(w.running) >= budget.Concurrency || contracts.LargeDownload(task.Kind) && len(w.downloads) > 0 {
		return false
	}
	jobCtx, cancel := context.WithDeadline(ctx, task.Deadline)
	w.running[task.ID] = cancel
	if contracts.LargeDownload(task.Kind) {
		if w.downloads == nil {
			w.downloads = map[string]bool{}
		}
		w.downloads[task.ID] = true
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer cancel()
		defer func() { w.mu.Lock(); delete(w.running, task.ID); delete(w.downloads, task.ID); w.mu.Unlock() }()
		run(jobCtx, task)
	}()
	return true
}

func workerCapabilities() contracts.CapabilityDocument {
	tasks := map[string]int{}
	for _, kind := range []string{contracts.TaskSubscriptionFetch, contracts.TaskReleaseQuery, contracts.TaskExternalConvert, contracts.TaskTraceRead, contracts.TaskCoreInstall, contracts.TaskNodeProbe, contracts.TaskSubscriptionProbe} {
		tasks[kind] = 1
	}
	for _, kind := range []string{contracts.ActionAssetFetch, contracts.ActionSourceProbe, contracts.ActionNodeTest, contracts.ActionOutboundProbe, contracts.ActionChainProbe, contracts.ActionRealityProbe, contracts.ActionPanelConfigure, contracts.ActionPanelCert, contracts.ActionPanelDiscover, contracts.ActionPanelRemove, contracts.ActionACMEHTTP01, contracts.ActionDiagnostics, contracts.ActionCoreRestart, contracts.ActionHostRepair, contracts.ActionLiveStart, contracts.ActionReleaseReplace, contracts.ActionUninstall} {
		tasks[kind] = contracts.TaskVersion
	}
	return contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: buildinfo.Version, Arch: runtime.GOARCH, OS: runtime.GOOS, Tasks: tasks}
}

func (a *Agent) workerLoop(ctx context.Context) {
	ctx, cancelLoop := context.WithCancel(ctx)
	w := &workerRunner{running: map[string]context.CancelFunc{}}
	caps := a.workerCapabilities()
	capsJSON, _ := json.Marshal(caps)
	sendCaps := true
	var applyWG sync.WaitGroup
	var applied atomic.Int64
	a.mu.Lock()
	applied.Store(a.st.Applied)
	a.mu.Unlock()
	var applyMu sync.Mutex
	applying := false
	defer func() { cancelLoop(); w.stop(); applyWG.Wait() }()
	for ctx.Err() == nil {
		currentCaps := a.workerCapabilities()
		currentCapsJSON, _ := json.Marshal(currentCaps)
		if string(currentCapsJSON) != string(capsJSON) {
			caps, capsJSON, sendCaps = currentCaps, currentCapsJSON, true
		}
		revision := applied.Load()
		resources := sampleWorkerResources()
		resources.Components = a.workerComponentSnapshot()
		a.workerObserved.RLock()
		if !a.workerObserved.sourcesAt.IsZero() {
			at := a.workerObserved.sourcesAt
			resources.SourcesObservedAt = &at
		}
		a.workerObserved.RUnlock()
		resources.Concurrency = 32 // Local hard cap; center policy may only lower it.
		req := contracts.AgentPoll{Protocol: contracts.AgentProtocol, ConfigRevision: revision, Resources: resources}
		a.mu.Lock()
		if a.st.Config != nil {
			req.ExecutionSeq = a.st.Config.ExecutionSeq
		}
		a.mu.Unlock()
		if sendCaps {
			req.Capabilities = &caps
		}
		var reply contracts.AgentPollReply
		code, err := a.center.call(ctx, "POST", "/agent/poll", req, &reply)
		if code == 404 {
			a.heartbeatProtocol.Store(0)
			a.meterProtocol.Store(0)
			a.center.gzipPayload.Store(false)
			// Rollout only: old centers retain their two legacy poll endpoints.
			go a.actionLoop(ctx)
			a.configLoop(ctx)
			return
		}
		if err != nil {
			if code == 409 {
				sendCaps = true
			}
			sleep(ctx, 5*time.Second)
			continue
		}
		if reply.Protocol != contracts.AgentProtocol {
			log.Printf("worker: unsupported protocol")
			sleep(ctx, 5*time.Second)
			continue
		}
		if reply.WorkerBudget != nil {
			if a.setWorkerBudget(*reply.WorkerBudget) != nil {
				log.Printf("worker: invalid budget")
				sleep(ctx, 5*time.Second)
				continue
			}
		}
		w.mu.Lock()
		w.budget = a.currentWorkerBudget()
		w.mu.Unlock()
		a.meterProtocol.Store(int32(reply.MeterProtocol))
		if reply.HeartbeatProtocol == contracts.HeartbeatProtocol {
			a.heartbeatProtocol.Store(contracts.HeartbeatProtocol)
		}
		a.center.gzipPayload.Store(slices.Contains(reply.PayloadEncodings, "gzip"))
		sendCaps = false
		for _, id := range reply.Cancel {
			w.mu.Lock()
			cancel := w.running[id]
			w.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
		if reply.Config != nil && reply.Config.CertOnly {
			if err = a.ensureCerts(ctx, *reply.Config); err != nil {
				log.Printf("cert sync: %v", err)
			} else if _, err = a.center.call(ctx, "POST", "/agent/state", contracts.GateState{AppliedRevision: a.appliedRevision(), CertSynced: true}, nil); err != nil {
				log.Printf("report cert sync: %v", err)
			} else {
				log.Printf("cert sync: revision %d refs %d", reply.Config.Revision, len(reply.Config.Certs))
			}
			sleep(ctx, time.Second)
			continue
		}
		if reply.Config != nil {
			applyMu.Lock()
			if !applying {
				applying = true
				cfg := *reply.Config
				configCtx := configurationContext(ctx, cfg)
				applyWG.Add(1)
				go func() {
					defer applyWG.Done()
					defer func() { applyMu.Lock(); applying = false; applyMu.Unlock() }()
					a.mu.Lock()
					state, e := a.applySyncedConfig(configCtx, cfg)
					a.mu.Unlock()
					if e != nil {
						log.Printf("worker configuration failed revision=%d", cfg.Revision)
						return
					}
					applied.Store(state.AppliedRevision)
					if _, e = a.center.call(configCtx, "POST", "/agent/state", state, nil); e != nil {
						log.Printf("worker configuration report failed revision=%d", cfg.Revision)
					}
				}()
			}
			applyMu.Unlock()
		}
		for _, task := range reply.Tasks {
			w.start(ctx, task, a.executeTask)
		}
		// An unapplied revision is returned immediately. Pace retries while an
		// application is running rather than opening a busy poll loop.
		if reply.Config != nil {
			sleep(ctx, time.Second)
		}
	}
}

type taskJournal struct {
	Kind     string                      `json:"kind"`
	Key      string                      `json:"key"`
	Status   string                      `json:"status"`
	Error    *contracts.TaskError        `json:"error,omitempty"`
	Deadline time.Time                   `json:"deadline"`
	Stages   []contracts.TaskStageRecord `json:"stages,omitempty"`
}

func (a *Agent) executeTask(ctx context.Context, t contracts.Task) {
	if len(t.ID) != 48 || len(t.LeaseToken) != 48 || t.Attempt < 1 || t.IdempotencyKey == "" || !t.LeaseUntil.After(time.Now()) || contracts.ValidateTaskPayload(t.Kind, t.Version, t.Payload) != nil {
		return
	}
	for _, c := range t.ID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return
		}
	}
	ctx = releasefiles.WithTrace(ctx, "00-"+t.TraceID+"-1111111111111111-01")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	started := time.Now().UTC()
	u := contracts.TaskUpdate{Attempt: t.Attempt, LeaseToken: t.LeaseToken}
	// Validate the lease immediately before work, including an operation that
	// could stop this process (uninstall/update).
	if _, err := a.center.call(ctx, "POST", "/agent/tasks/"+t.ID+"/renew", u, nil); err != nil {
		return
	}
	journal := filepath.Join(a.P.Data, "tasks", t.ID+".json")
	var previous taskJournal
	journalErr := readJournal(journal, &previous)
	if journalErr != nil && !errors.Is(journalErr, os.ErrNotExist) {
		u.Error = &contracts.TaskError{Code: "task_journal_invalid", Origin: "gate.readJournal"}
		_, _ = a.center.call(ctx, "POST", "/agent/tasks/"+t.ID+"/result", u, nil)
		return
	}
	if journalErr == nil {
		// Results can contain credentials. Only metadata is persisted on Gate;
		// an uncertain side effect must be reconciled by the center.
		if contracts.Relocatable(t.Kind) && previous.Key == t.IdempotencyKey && previous.Kind == t.Kind {
			// Read-only work can safely run again when a response was lost.
		} else {
			u.Error = &contracts.TaskError{Code: "task_reconciliation_required", Origin: "gate.executeTask"}
			_, _ = a.center.call(ctx, "POST", "/agent/tasks/"+t.ID+"/result", u, nil)
			return
		}
	}
	if err := prepareJournals(filepath.Dir(journal), time.Now()); err != nil {
		u.Error = &contracts.TaskError{Code: "task_journal_capacity", Origin: "gate.prepareJournals"}
		_, _ = a.center.call(ctx, "POST", "/agent/tasks/"+t.ID+"/result", u, nil)
		return
	}
	if err := writeJournal(journal, taskJournal{Kind: t.Kind, Key: t.IdempotencyKey, Status: "running", Deadline: t.Deadline}); err != nil {
		return
	}
	reportProgress := func(stage string) {
		progressCtx, progressCancel := context.WithTimeout(ctx, 3*time.Second)
		defer progressCancel()
		progress := contracts.TaskUpdate{Attempt: t.Attempt, LeaseToken: t.LeaseToken, Progress: &contracts.TaskProgress{Stage: stage}}
		if code, _ := a.center.call(progressCtx, "POST", "/agent/tasks/"+t.ID+"/progress", progress, nil); code == 409 {
			cancel()
		}
	}
	reportProgress("executing")
	if ctx.Err() != nil {
		return
	}
	stages := &taskStageLog{task: t}
	stages.task.Payload = nil
	stages.task.LeaseToken = ""
	ctx = context.WithValue(ctx, taskLeaseKey{}, taskLease{id: t.ID, attempt: t.Attempt, token: t.LeaseToken})
	ctx = context.WithValue(ctx, taskStageKey{}, stages)
	renewDone := make(chan struct{})
	defer close(renewDone)
	renewal := u
	go func() {
		ticker := time.NewTicker(contracts.TaskRenewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-renewDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := a.center.call(ctx, "POST", "/agent/tasks/"+t.ID+"/renew", renewal, nil); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	var cpuRaw json.RawMessage
	var cpuErr error
	cpuRan := false
	if contracts.CPUHeavy(t.Kind) {
		reportProgress("cpu_isolated")
		cpuRaw, cpuErr = runCPUChild(ctx, a.P, a.currentWorkerBudget(), t)
		cpuRan = true
	}
	var payload map[string]any
	err := json.Unmarshal(t.Payload, &payload)
	var result contracts.ActionResult
	if cpuRan {
		err = cpuErr
		if err == nil {
			result.OK = true
			_ = json.Unmarshal(cpuRaw, &result.Result)
		} else {
			result.Error = cpuTaskError(cpuErr)
		}
	} else if err == nil && t.Kind == contracts.TaskTraceRead {
		var req contracts.TraceReadTask
		err = contracts.DecodeStrict(t.Payload, &req, contracts.MaxTaskPayload)
		if err == nil {
			var detail contracts.TraceReadResult
			detail, err = readTaskStages(ctx, filepath.Join(a.P.Data, "tasks"), req)
			if err == nil {
				raw, _ := json.Marshal(detail)
				_ = json.Unmarshal(raw, &result.Result)
				result.OK = true
			}
		}
	} else if err == nil && (t.Kind == contracts.TaskNodeProbe || t.Kind == contracts.TaskSubscriptionProbe) {
		raw, e := a.probeTask(ctx, t)
		err = e
		if e == nil {
			result.OK = true
			_ = json.Unmarshal(raw, &result.Result)
		}
	} else if err == nil && (t.Kind == contracts.TaskLayaInstall || t.Kind == contracts.TaskLayaRemove || t.Kind == contracts.TaskLayaClassify) {
		raw, e := a.layaTask(ctx, t)
		err = e
		if e == nil {
			_ = json.Unmarshal(raw, &result.Result)
			result.OK = true
		} else {
			result.Error = "laya_task_failed"
		}
	} else if err == nil && (t.Kind == contracts.TaskSubscriptionFetch || t.Kind == contracts.TaskReleaseQuery || t.Kind == contracts.TaskExternalConvert) {
		raw, e := a.networkTask(ctx, t)
		if e == nil {
			result.OK = true
			_ = json.Unmarshal(raw, &result.Result)
		} else {
			result.Error = networkErrorCode(e)
		}
	} else if err == nil {
		result = a.runAction(ctx, contracts.Action{Kind: t.Kind, Payload: payload, ExpiresAt: t.Deadline})
	} else {
		result.Error = "task_payload_invalid"
	}
	t.Payload = nil
	payload = nil
	if result.OK {
		u.Result, _ = json.Marshal(result.Result)
		if string(u.Result) == "null" {
			u.Result = json.RawMessage(`{}`)
		}
	} else {
		code := "task_execution_failed"
		origin := "gate.runAction"
		if result.Error != "" {
			log.Printf("task execution failed kind=%s err=%s", t.Kind, trimTaskErr(result.Error))
		}
		if t.Kind == contracts.TaskSubscriptionFetch || t.Kind == contracts.TaskReleaseQuery || t.Kind == contracts.TaskExternalConvert {
			code, origin = result.Error, "gate.executeNetworkTask"
		} else if result.Error != "" {
			if candidate := (&contracts.TaskError{Code: result.Error, Origin: origin}); candidate.Validate() == nil {
				code = result.Error
			}
		}
		var detail *contracts.TaskError
		if errors.As(err, &detail) {
			code, origin = detail.Code, detail.Origin
		}
		if t.Kind == contracts.ActionNodeTest {
			if stage, ok := result.Result["stage"].(string); ok {
				switch stage {
				case "build", "check", "client_build", "client_check", "target", "connect", "start":
					code, origin = "node_test_"+stage+"_failed", "gate.nodeTest"
				}
			}
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			code = "task_cancelled"
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = "task_timeout"
		}
		u.Error = &contracts.TaskError{Code: code, Origin: origin, Retryable: contracts.Relocatable(t.Kind) && code != "release_not_found"}
	}
	status := "succeeded"
	if u.Error != nil {
		status = "failed"
	}
	stage := contracts.TaskStageRecord{TaskID: t.ID, TraceID: t.TraceID, Kind: t.Kind, Attempt: t.Attempt, Stage: "execute", Started: started, Finished: time.Now().UTC(), Outcome: status, Origin: "gate.executeTask"}
	if u.Error != nil {
		stage.ErrorCode, stage.Origin = u.Error.Code, u.Error.Origin
	}
	stages.mu.Lock()
	records := append(append([]contracts.TaskStageRecord(nil), stages.records...), stage)
	stages.mu.Unlock()
	_ = writeJournal(journal, taskJournal{Kind: t.Kind, Key: t.IdempotencyKey, Status: status, Error: u.Error, Deadline: t.Deadline, Stages: records})
	if ctx.Err() == nil {
		reportProgress("finalizing")
	}
	if ctx.Err() != nil {
		// A cancellation acknowledgement is metadata only. It must survive the
		// cancelled execution context, but cannot complete a different lease.
		ackCtx, ackCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer ackCancel()
		_, _ = a.center.call(ackCtx, "POST", "/agent/tasks/"+t.ID+"/cancelled", renewal, nil)
		return
	}
	for attempt := 0; attempt < 3 && ctx.Err() == nil; attempt++ {
		code, e := a.center.call(ctx, "POST", "/agent/tasks/"+t.ID+"/result", u, nil)
		if e == nil || code == 409 {
			return
		}
		sleep(ctx, time.Second)
	}
}

func trimTaskErr(msg string) string {
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

func cpuTaskError(err error) string {
	var detail *contracts.TaskError
	if errors.As(err, &detail) && detail.Code != "" {
		return detail.Code
	}
	text := ""
	if err != nil {
		text = err.Error()
	}
	switch {
	case strings.Contains(text, "cpu_cgroup_permission"):
		return "cpu_cgroup_permission"
	case strings.Contains(text, "cpu_controller_missing"):
		return "cpu_controller_missing"
	case strings.Contains(text, "cpu_cgroup_not_delegated"):
		return "cpu_cgroup_not_delegated"
	case strings.Contains(text, "cpu_cgroup_unavailable"):
		return "cpu_cgroup_unavailable"
	case strings.Contains(text, "cpu_isolated_process_failed"):
		return "cpu_isolated_process_failed"
	default:
		return "task_execution_failed"
	}
}
