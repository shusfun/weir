package center

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
		validated := operationStage(ctx, "worker.capabilities.validate", "gate_id", g.ID)
		err := req.Capabilities.Validate()
		validated(err)
		if err != nil {
			failure(c, 400, "invalid_worker_payload")
			return
		}
		if err := s.Store.Lock.Do(ctx, "gate-capabilities:"+itoa(g.ID), func(ctx context.Context) (err error) {
			previous, err := s.Store.Tasks.Capabilities(ctx, g.ID)
			invalid := errors.Is(err, taskqueue.ErrInvalidCapabilities)
			finished := operationStage(ctx, "worker.capabilities.refresh", "gate_id", g.ID, "previous_invalid", invalid)
			defer func() { finished(err) }()
			if err != nil && !errors.Is(err, taskqueue.ErrNotFound) && !invalid {
				return err
			}
			if invalid || previous.ConfigFeatures[contracts.ConfigDomainObservation] != req.Capabilities.ConfigFeatures[contracts.ConfigDomainObservation] {
				// Advance first: a failed capability write can safely retry, whereas
				// publishing the capability first could permanently skip deployment.
				// An invalid cache has no trustworthy features to compare.
				if _, err := s.bumpGate(ctx, g.ID); err != nil {
					return err
				}
			}
			return s.Store.Tasks.SetCapabilities(ctx, g.ID, *req.Capabilities)
		}); err != nil {
			failInternal(c, 503, "capabilities_update_failed", err)
			return
		}
	}
	caps, err := s.Store.Tasks.Capabilities(ctx, g.ID)
	if err != nil {
		failure(c, 409, "capabilities_required")
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
			cfg.TraceID = s.configRevisionTrace(ctx, cur)
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
			if r.Task.Kind == contracts.TaskSubscriptionFetch || r.Task.Kind == contracts.TaskReleaseQuery {
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
			if r.Task.Kind == contracts.TaskLayaPrepare || r.Task.Kind == contracts.TaskLayaInstall || r.Task.Kind == contracts.TaskLayaRemove {
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
			if r.Task.Kind == contracts.TaskNodeProbe || r.Task.Kind == contracts.TaskNodeProbeXray || r.Task.Kind == contracts.TaskSubscriptionProbe {
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

// GateAction rows retain stable business operation IDs. They project durable
// task results and are never a second delivery source.
func (s *Server) projectLegacyTask(ctx context.Context, id string) error {
	return s.Store.Lock.Do(ctx, "worker-projection:"+id, func(ctx context.Context) error {
		pending, err := s.Store.Tasks.NeedsProjection(ctx, id)
		if err != nil || !pending {
			return err
		}
		if err = s.applyWorkerProjection(ctx, id); err != nil {
			return errors.Join(err, s.Store.Tasks.ResolveProjection(ctx, id, time.Now().Add(time.Minute)))
		}
		return s.Store.Tasks.ResolveProjection(ctx, id, time.Time{})
	})
}

func (s *Server) applyWorkerProjection(ctx context.Context, id string) error {
	r, err := s.Store.Tasks.Get(ctx, id)
	if err != nil {
		return err
	}
	if r.LegacyID == 0 {
		if r.Task.Kind == contracts.TaskLayaPrepare || r.Task.Kind == contracts.TaskLayaInstall || r.Task.Kind == contracts.TaskLayaRemove {
			return s.auditGateLayaTask(ctx, r)
		}
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
	if err = contracts.ValidateTaskPayload(kind, contracts.TaskSchemaVersion(kind), raw); err != nil {
		return nil, err
	}
	sealed, err := s.Crypto.Encrypt(string(raw))
	if err != nil {
		return nil, err
	}
	// The operation row keeps its stable ID and never contains the task body or credentials.
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
				ctx := s.hotspotContext(ctx)
				candidates, projected := 0, 0
				finish := operationStage(ctx, "worker.maintenance",
					"projection_candidates", stageValue(func() slog.Value { return slog.IntValue(candidates) }),
					"projected", stageValue(func() slog.Value { return slog.IntValue(projected) }))
				var maintenanceErr error
				note := func(name string, err error) {
					maintenanceErr = errors.Join(maintenanceErr, err)
					s.tasks.note(name, err)
				}
				if time.Since(lastPrune) >= time.Minute {
					doc, policyErr := s.retentionPolicy(ctx)
					note("retention-policy", policyErr)
					if policyErr == nil {
						note("worker-retention", s.Store.Tasks.PruneObservations(ctx, time.Now(), time.Duration(doc.Policy.TaskDays)*24*time.Hour, doc.Policy.MaxRecords))
						note("observation-analysis-retention", s.pruneObservationAnalyses(ctx, time.Now(), time.Duration(doc.Policy.ObservationDays)*24*time.Hour, doc.Policy.MaxRecords))
						note("audit-retention", s.pruneAudit(ctx, time.Now(), doc.Policy))
					}
					note("subscription-cache-retention", s.Store.PruneSubscriptionCacheReservations(ctx, s.Config.Store.Maintenance.Effective().RecordBatch))
					note("laya-host-retention", s.maintainLayaHosting(ctx, time.Now()))
					note("asset-relay-retention", s.pruneRelaySessions(ctx, time.Now()))
					cacheCtx, cancelCache := context.WithTimeout(ctx, time.Second)
					err := s.Store.Lock.Do(cacheCtx, "center-file-distribution", func(ctx context.Context) error {
						return s.pruneKernelArchives(ctx, 0, "", time.Now())
					})
					cancelCache()
					if !errors.Is(err, context.DeadlineExceeded) {
						note("kernel-archive-retention", err)
					}
					note("observation-text-retention", s.pruneTextObservations(ctx, time.Now()))
					lastPrune = time.Now()
				}
				note("worker-communication", s.flushCommunication(ctx, time.Now(), false))
				_, expireErr := s.Store.Ent.GateAction.Update().Where(gateaction.StatusEQ(gateaction.StatusPending), gateaction.ExpiresAtLT(time.Now())).SetStatus(gateaction.StatusExpired).Save(ctx)
				note("legacy-action-expiry", expireErr)
				changed, err := s.Store.Tasks.SweepTransitions(ctx, time.Now())
				note("worker-leases", err)
				if len(changed) > 0 {
					_ = s.Store.Notifications.Publish(ctx, "workers")
					for _, id := range changed {
						_ = s.Store.Notifications.Publish(ctx, "task:"+id)
					}
				}
				ids, projectionErr := s.Store.Tasks.PendingProjections(ctx, time.Now())
				candidates = len(ids)
				for _, id := range ids {
					if applyErr := s.projectLegacyTask(ctx, id); applyErr != nil {
						projectionErr = errors.Join(projectionErr, applyErr)
					} else {
						projected++
					}
				}
				note("worker-projection", projectionErr)
				finish(maintenanceErr)
			}
			s.state.RUnlock()
		}
	}
}
