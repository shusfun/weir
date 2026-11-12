package center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/publicnet"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/gateaction"
	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

func bindWorker(c *gin.Context, out any, limit int) bool {
	raw, err := publicnet.ReadBody(&http.Response{Body: c.Request.Body, Header: c.Request.Header}, int64(min(limit, 4<<20)), int64(limit))
	if err == nil {
		err = contracts.DecodeStrict(raw, out, limit)
	}
	if err != nil {
		failure(c, 400, "invalid_worker_payload")
		return false
	}
	return true
}

func (s *Server) agentPoll(c *gin.Context) {
	var req contracts.AgentPoll
	if !bindWorker(c, &req, contracts.MaxTaskPayload) {
		return
	}
	if req.Protocol != contracts.AgentProtocol || req.Resources.Validate() != nil || req.ExecutionSeq < 0 {
		failure(c, 400, "unsupported_agent_protocol")
		return
	}
	g, ctx := gateFrom(c), c.Request.Context()
	_, budget, err := s.workerPolicy(ctx)
	if err != nil {
		failInternal(c, 503, "worker_policy_unavailable", err)
		return
	}
	req.Resources.Concurrency = min(req.Resources.Concurrency, budget.Concurrency)
	if err := s.recordWorkerResources(ctx, g.ID, req.Resources); err != nil {
		failInternal(c, 503, "worker_resources_unavailable", err)
		return
	}
	if req.Capabilities != nil {
		if err := s.Store.Tasks.SetCapabilities(ctx, g.ID, *req.Capabilities); err != nil {
			failure(c, 400, "invalid_capabilities")
			return
		}
	}
	caps, err := s.Store.Tasks.Capabilities(ctx, g.ID)
	if err != nil {
		failure(c, 409, "capabilities_required")
		return
	}
	if err := s.migratePendingActions(ctx, g.ID); err != nil {
		failInternal(c, 503, "task_migration_failed", err)
		return
	}
	wake, unsubscribe := s.Store.Notifications.Subscribe("gate:" + itoa(g.ID))
	defer unsubscribe()
	all, unsubAll := s.Store.Notifications.Subscribe("workers")
	defer unsubAll()
	timer := time.NewTimer(25 * time.Second)
	defer timer.Stop()
	for {
		_, budget, err = s.workerPolicy(ctx)
		if err != nil {
			failInternal(c, 503, "worker_policy_unavailable", err)
			return
		}
		req.Resources.Concurrency = min(req.Resources.Concurrency, budget.Concurrency)
		configWake := s.waiters.wait(g.ID)
		taskWake := s.actionWaiters.wait(g.ID)
		cur, err := s.Store.Ent.Gate.Get(ctx, g.ID)
		if err != nil {
			failInternal(c, 503, "worker_unavailable", err)
			return
		}
		if cur.Status != gate.StatusActive {
			failure(c, 403, "gate_revoked")
			return
		}
		reply := contracts.AgentPollReply{Protocol: contracts.AgentProtocol, WorkerBudget: &budget, MeterProtocol: 2, HeartbeatProtocol: contracts.HeartbeatProtocol, PayloadEncodings: []string{"gzip"}}
		execution, err := s.loadExecution(ctx, g.ID)
		if err != nil {
			failInternal(c, 503, "execution_unavailable", err)
			return
		}
		if cur.ConfigRevision != req.ConfigRevision {
			cfg, e := s.compileGateConfig(ctx, cur)
			if e != nil {
				failInternal(c, 409, "config_unavailable", e)
				return
			}
			cfg.ExecutionSeq = execution.Seq
			reply.Config = &cfg
		} else if execution.Seq > req.ExecutionSeq {
			reply.Config = &contracts.GateConfig{Revision: cur.ConfigRevision, TraceID: taskTrace(ctx), ExecutionOnly: true, ExecutionSeq: execution.Seq, Budgets: execution.Budgets, ForceDisconnect: execution.ForceDisconnect}
		} else if raw, ok, e := s.Store.KV.Get(ctx, certPushKey(g.ID)); e != nil {
			failInternal(c, 503, "cert_sync_unavailable", e)
			return
		} else if ok && raw != "" {
			// 续签只换证书文件，不推进配置版本。新轮询通道也要把这次下发带上，否则 Gate 一直用旧文件。
			cfg, e := s.compileGateConfig(ctx, cur)
			if e != nil {
				failInternal(c, 503, "cert_sync_unavailable", e)
				return
			}
			cfg.CertOnly = true
			reply.Config = &cfg
		}
		cancel, e := s.Store.Tasks.Cancellations(ctx, g.ID)
		if e != nil {
			failInternal(c, 503, "tasks_unavailable", e)
			return
		}
		reply.Cancel = cancel
		rs, e := s.Store.Tasks.Claim(ctx, g.ID, caps, req.Resources, time.Now())
		if e != nil {
			failInternal(c, 503, "tasks_unavailable", e)
			return
		}
		for _, r := range rs {
			body, e := s.Crypto.Decrypt(r.Ciphertext)
			if e != nil {
				failInternal(c, 500, "task_decryption_failed", e)
				return
			}
			r.Task.Payload = json.RawMessage(body)
			if e = contracts.ValidateTaskPayload(r.Task.Kind, r.Task.Version, r.Task.Payload); e != nil {
				failInternal(c, 500, "task_schema_invalid", e)
				return
			}
			reply.Tasks = append(reply.Tasks, r.Task)
		}
		if reply.Config != nil || len(reply.Tasks) > 0 || len(reply.Cancel) > 0 {
			c.JSON(200, reply)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			c.JSON(200, reply)
			return
		case <-wake:
		case <-all:
		case <-configWake:
		case <-taskWake:
		}
	}
}

func (s *Server) agentTaskUpdate(c *gin.Context) {
	var u contracts.TaskUpdate
	if !bindWorker(c, &u, contracts.MaxTaskResult+65536) {
		return
	}
	ctx := c.Request.Context()
	id := c.Param("id")
	gid := gateFrom(c).ID
	var err error
	if c.Param("operation") == "renew" {
		var until time.Time
		until, err = s.Store.Tasks.Renew(ctx, id, gid, u, time.Now())
		if err == nil {
			c.JSON(200, gin.H{"lease_until": until})
			return
		}
	} else if c.Param("operation") == "progress" {
		if u.Progress == nil || u.Progress.Validate() != nil || len(u.Result) > 0 || u.Error != nil {
			failure(c, 400, "invalid_task_progress")
			return
		}
		err = s.Store.Tasks.Progress(ctx, id, gid, u, time.Now())
		if err == nil {
			c.Status(204)
			return
		}
	} else if c.Param("operation") == "result" {
		resultBytes := int64(len(u.Result))
		if u.Error != nil && u.Error.Validate() != nil {
			failure(c, 400, "invalid_task_error")
			return
		}
		if len(u.Result) > contracts.MaxTaskResult || (u.Error != nil && len(u.Result) > 0) || (u.Error == nil && len(u.Result) == 0) {
			failure(c, 400, "invalid_task_result")
			return
		}
		r, e := s.Store.Tasks.Get(ctx, id)
		if e != nil || r.GateID != gid {
			failure(c, 409, "task_lease_lost")
			return
		}
		if len(u.Result) > 0 {
			var result map[string]json.RawMessage
			if json.Unmarshal(u.Result, &result) != nil || result == nil {
				failure(c, 400, "invalid_task_result")
				return
			}
			if r.Task.Kind == contracts.TaskSubscriptionFetch || r.Task.Kind == contracts.TaskExternalConvert || r.Task.Kind == contracts.TaskReleaseQuery {
				if e = s.validateNetworkResult(ctx, r, u.Result); e != nil {
					failure(c, 400, "invalid_network_result")
					return
				}
			}
			if r.Task.Kind == contracts.TaskLayaClassify {
				if e = s.validateLayaResult(ctx, r, u.Result); e != nil {
					failure(c, 400, "invalid_laya_result")
					return
				}
			}
			if r.Task.Kind == contracts.ActionAssetFetch {
				if e = s.validateRelayResult(ctx, r, u.Result); e != nil {
					failure(c, 400, "invalid_relay_result")
					return
				}
			}
			if r.Task.Kind == contracts.TaskCoreInstall {
				if e = s.validateKernelInstallResult(ctx, r.Task.ID, u.Result); e != nil {
					failure(c, 400, "invalid_kernel_result")
					return
				}
			}
			if r.Task.Kind == contracts.TaskLayaInstall || r.Task.Kind == contracts.TaskLayaRemove {
				if e = s.validateLayaDeploymentResult(ctx, r, u.Result); e != nil {
					failure(c, 400, "invalid_laya_result")
					return
				}
			}
			if r.Task.Kind == contracts.TaskTraceRead {
				if e = s.validateTraceReadResult(ctx, r, u.Result); e != nil {
					failure(c, 400, "invalid_trace_result")
					return
				}
			}
			if r.Task.Kind == contracts.TaskNodeProbe || r.Task.Kind == contracts.TaskSubscriptionProbe {
				if e = s.validateProbeResult(ctx, r, u.Result); e != nil {
					failure(c, 400, "invalid_probe_result")
					return
				}
			}
			sealed, e := s.Crypto.Encrypt(string(u.Result))
			if e != nil {
				failInternal(c, 500, "task_encryption_failed", e)
				return
			}
			u.Result, _ = json.Marshal(struct {
				Sealed string `json:"sealed"`
			}{sealed})
		}
		err = s.Store.Tasks.FinishMeasured(ctx, id, gid, u, time.Now(), resultBytes)
		if err == nil {
			err = s.projectLegacyTask(ctx, id)
		}
		if err == nil {
			_ = s.Store.Notifications.Publish(ctx, "workers")
			_ = s.Store.Notifications.Publish(ctx, "task:"+id)
			c.Status(204)
			return
		}
	} else if c.Param("operation") == "cancelled" {
		err = s.Store.Tasks.AcknowledgeCancel(ctx, id, gid, u, time.Now())
		if err == nil {
			_ = s.projectLegacyTask(ctx, id)
			_ = s.Store.Notifications.Publish(ctx, "workers")
			_ = s.Store.Notifications.Publish(ctx, "task:"+id)
			c.Status(204)
			return
		}
	} else {
		failure(c, 404, "task_operation_unknown")
		return
	}
	if errors.Is(err, taskqueue.ErrLease) {
		failure(c, 409, "task_lease_lost")
		return
	}
	failInternal(c, 503, "task_update_failed", err)
}

// During rollout legacy HTTP callers await GateAction rows. They are a view of
// the durable queue for protocol 2 workers, never a second delivery source.
func (s *Server) projectLegacyTask(ctx context.Context, id string) error {
	r, err := s.Store.Tasks.Get(ctx, id)
	if err != nil {
		return err
	}
	if r.LegacyID == 0 {
		return nil
	}
	if r.Status != "succeeded" && r.Status != "failed" && r.Status != "expired" && r.Status != "unknown" && r.Status != "cancelled" {
		return nil
	}
	legacy, err := s.Store.Ent.GateAction.Get(ctx, r.LegacyID)
	if err != nil {
		return err
	}
	if legacy.Status == gateaction.StatusDone || legacy.Status == gateaction.StatusFailed || legacy.Status == gateaction.StatusExpired {
		return nil
	}
	status := gateaction.StatusDone
	message := ""
	if r.Status != "succeeded" {
		status = gateaction.StatusFailed
		message = "task_" + r.Status
		if r.Error != nil {
			message = r.Error.Code
		}
	}
	var result map[string]any
	if r.Task.Kind == contracts.ActionNodeTest && r.Error != nil {
		switch r.Error.Code {
		case "node_test_build_failed", "node_test_check_failed", "node_test_client_build_failed", "node_test_client_check_failed", "node_test_target_failed", "node_test_connect_failed", "node_test_start_failed":
			stage := strings.TrimSuffix(strings.TrimPrefix(r.Error.Code, "node_test_"), "_failed")
			result = map[string]any{"stage": stage}
		}
	}
	if len(r.Result) > 0 {
		body, e := s.workerResult(r.Result)
		if e != nil {
			return e
		}
		if err = json.Unmarshal(body, &result); err != nil {
			return err
		}
	}
	err = s.Store.Ent.GateAction.UpdateOneID(r.LegacyID).SetStatus(status).SetError(message).SetResult(result).SetFinishedAt(time.Now()).Exec(ctx)
	if err == nil {
		s.actionDone.notify(r.LegacyID)
	}
	return err
}

func (s *Server) workerResult(raw json.RawMessage) (json.RawMessage, error) {
	var envelope struct {
		Sealed string `json:"sealed"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Sealed == "" {
		return nil, errors.New("task_result_not_sealed")
	}
	body, err := s.Crypto.Decrypt(envelope.Sealed)
	return json.RawMessage(body), err
}

func (s *Server) enqueueWorkerTask(ctx context.Context, gid int, kind string, payload map[string]any, ttl time.Duration) (*ent.GateAction, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if err = contracts.ValidateTaskPayload(kind, contracts.TaskVersion, raw); err != nil {
		return nil, err
	}
	sealed, err := s.Crypto.Encrypt(string(raw))
	if err != nil {
		return nil, err
	}
	// The compatibility row intentionally contains no credentials or task body.
	a, err := s.Store.Ent.GateAction.Create().SetGateID(gid).SetKind(kind).SetPayload(map[string]any{"protocol": contracts.AgentProtocol}).SetStatus(gateaction.StatusRunning).SetExpiresAt(time.Now().Add(ttl)).Save(ctx)
	if err != nil {
		return nil, err
	}
	_, err = s.Store.Tasks.Enqueue(ctx, taskqueue.Request{Kind: kind, TraceID: taskTrace(ctx), Key: "legacy:" + itoa(a.ID), Payload: raw, Requirements: contracts.TaskRequirements{TargetGateID: gid}, Candidates: []int{gid}, Deadline: a.ExpiresAt, LegacyID: a.ID}, sealed, time.Now())
	if err != nil {
		_ = s.Store.Ent.GateAction.UpdateOneID(a.ID).SetStatus(gateaction.StatusFailed).SetError("task_enqueue_failed").Exec(ctx)
		return nil, err
	}
	_ = s.Store.Notifications.Publish(ctx, "gate:"+itoa(gid))
	s.actionWaiters.notify(gid)
	return a, nil
}

func (s *Server) migratePendingActions(ctx context.Context, gid int) error {
	return s.Store.Lock.Do(ctx, "legacy-actions:"+itoa(gid), func(ctx context.Context) error {
		items, err := s.Store.Ent.GateAction.Query().Where(gateaction.GateIDEQ(gid), gateaction.StatusEQ(gateaction.StatusPending)).Order(ent.Asc(gateaction.FieldID)).Limit(100).All(ctx)
		items = unexpiredActions(items, time.Now())
		if err != nil {
			return err
		}
		for _, a := range items {
			payload := a.Payload
			if payload == nil {
				payload = map[string]any{}
			}
			raw, err := json.Marshal(payload)
			if err == nil {
				err = contracts.ValidateTaskPayload(a.Kind, contracts.TaskVersion, raw)
			}
			if err != nil {
				if e := s.Store.Ent.GateAction.UpdateOneID(a.ID).SetStatus(gateaction.StatusFailed).SetError("task_schema_invalid").SetFinishedAt(time.Now()).Exec(ctx); e != nil {
					return e
				}
				s.actionDone.notify(a.ID)
				continue
			}
			sealed, err := s.Crypto.Encrypt(string(raw))
			if err != nil {
				return err
			}
			_, err = s.Store.Tasks.Enqueue(ctx, taskqueue.Request{Kind: a.Kind, TraceID: taskTrace(ctx), Key: "legacy:" + itoa(a.ID), Payload: raw, Requirements: contracts.TaskRequirements{TargetGateID: gid}, Candidates: []int{gid}, Deadline: a.ExpiresAt, LegacyID: a.ID}, sealed, time.Now())
			if err != nil {
				return err
			}
			// Once capabilities exist, the old delivery endpoint cannot claim
			// these rows. An interrupted migration resumes with the same key.
			if err = s.Store.Ent.GateAction.UpdateOneID(a.ID).SetStatus(gateaction.StatusRunning).SetPayload(map[string]any{"protocol": contracts.AgentProtocol}).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Server) workerMaintenance(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var lastPrune time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.state.RLock()
			if s.Store != nil {
				if time.Since(lastPrune) >= time.Minute {
					s.tasks.note("worker-retention", s.Store.Tasks.PruneObservations(ctx, time.Now(), 14*24*time.Hour, 1_000_000))
					s.tasks.note("observation-analysis-retention", s.pruneObservationAnalyses(ctx, time.Now(), 14*24*time.Hour, 1_000_000))
					s.tasks.note("laya-host-retention", s.maintainLayaHosting(ctx, time.Now()))
					s.tasks.note("asset-relay-retention", s.pruneRelaySessions(ctx, time.Now()))
					cacheCtx, cancelCache := context.WithTimeout(ctx, time.Second)
					err := s.Store.Lock.Do(cacheCtx, "center-file-distribution", func(ctx context.Context) error {
						return s.pruneKernelArchives(ctx, 0, "", time.Now())
					})
					cancelCache()
					if !errors.Is(err, context.DeadlineExceeded) {
						s.tasks.note("kernel-archive-retention", err)
					}
					s.tasks.note("observation-text-retention", s.pruneTextObservations(ctx, time.Now()))
					lastPrune = time.Now()
				}
				s.tasks.note("worker-communication", s.flushCommunication(ctx, time.Now(), false))
				_, expireErr := s.Store.Ent.GateAction.Update().Where(gateaction.StatusEQ(gateaction.StatusPending), gateaction.ExpiresAtLT(time.Now())).SetStatus(gateaction.StatusExpired).Save(ctx)
				s.tasks.note("legacy-action-expiry", expireErr)
				changed, err := s.Store.Tasks.SweepChanged(ctx, time.Now())
				s.tasks.note("worker-leases", err)
				if err == nil {
					if changed > 0 {
						_ = s.Store.Notifications.Publish(ctx, "workers")
					}
					rs, e := s.Store.Tasks.List(ctx, 500)
					if e == nil {
						for _, r := range rs {
							if r.LegacyID != 0 {
								_ = s.projectLegacyTask(ctx, r.Task.ID)
							}
						}
					}
				}
			}
			s.state.RUnlock()
		}
	}
}
