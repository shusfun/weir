package center

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

type layaGateOperation struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id,omitempty"`
	Actor     int    `json:"actor"`
	GateID    int    `json:"gate_id"`
	Operation string `json:"operation"`
	TraceID   string `json:"trace_id"`
}

type gateLayaPreview struct {
	Actor     int       `json:"actor"`
	GateID    int       `json:"gate_id"`
	Operation string    `json:"operation"`
	Digest    string    `json:"digest"`
	Version   string    `json:"version"`
	Expires   time.Time `json:"expires"`
}

func (s *Server) gateLayaObservation(ctx context.Context, id int) (contracts.ComponentObservation, error) {
	r, err := s.workerResources(ctx, id)
	if err != nil || r == nil {
		return contracts.ComponentObservation{}, err
	}
	return r.Resources.Components["laya"], nil
}

func (s *Server) getGateLaya(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	observed, err := s.gateLayaObservation(c.Request.Context(), g.ID)
	if err != nil {
		failInternal(c, 503, "laya_hosting_unavailable", err)
		return
	}
	canPrepare, err := s.gateSupportsTask(c.Request.Context(), g, contracts.TaskLayaPrepare)
	if err != nil {
		failInternal(c, 503, "capabilities_unavailable", err)
		return
	}
	out := gin.H{"can_prepare": canPrepare, "gate_id": g.ID, "gate_name": g.Name, "online": gateOnline(g), "colocated": colocated(g), "component": observed, "prepared": observed.ImageDigest != "" && observed.DeploymentVersion != ""}
	if raw, ok, _ := s.Store.KV.Get(c.Request.Context(), "laya-gate-job:"+itoa(g.ID)); ok {
		var operation layaGateOperation
		if json.Unmarshal([]byte(raw), &operation) == nil {
			if operation.TaskID != "" {
				record, err := s.Store.Tasks.Get(c.Request.Context(), operation.TaskID)
				if err == nil {
					status := gin.H{"status": record.Status, "task_id": operation.TaskID, "operation": operation.Operation}
					if record.Progress != nil {
						status["stage"] = record.Progress.Stage
					}
					if record.Error != nil {
						status["code"] = record.Error.Code
					}
					out["operation"] = status
				} else {
					out["operation"] = gin.H{"status": "unknown", "task_id": operation.TaskID, "code": "task_record_unavailable", "operation": operation.Operation}
				}
			} else {
				out["operation"] = gin.H{"status": "unknown", "id": operation.ID, "operation": operation.Operation}
			}
		}
	}
	state := gateLayaState(g, observed, canPrepare, out["operation"])
	for action, kind := range map[string]string{"install": contracts.TaskLayaInstall, "remove": contracts.TaskLayaRemove} {
		supported, err := s.gateSupportsTask(c.Request.Context(), g, kind)
		if err != nil {
			failInternal(c, 503, "capabilities_unavailable", err)
			return
		}
		if !supported {
			state.Next = slices.DeleteFunc(state.Next, func(v string) bool { return v == action })
		}
	}
	out["object_state"] = state
	c.Header("Cache-Control", "no-store")
	c.JSON(200, out)
}

func gateLayaState(g *ent.Gate, observed contracts.ComponentObservation, canPrepare bool, operation any) objectState {
	v := gateObjectState(g)
	v.Next = []string{}
	if !gateOnline(g) || g.Status == "revoked" {
		return v
	}
	if colocated(g) {
		v.Status, v.Label, v.Reason, v.Next = "pending", "在中心管理", "本机执行端与中心共用 Laya 部署", []string{"center"}
		return v
	}
	v.Status, v.Label, v.Reason = "unknown", "状态未知", "尚未收到 Laya 组件的有效观测"
	if !observed.ObservedAt.IsZero() {
		v.ObservedAt = &observed.ObservedAt
	}
	if canPrepare {
		v.Next = append(v.Next, "prepare")
		v.Status, v.Label, v.Reason = "pending", "待准备", "可通过固定宿主服务准备部署权限"
	} else {
		v.Reason = "VPS 未声明准备能力，请核对固定宿主服务及能力上报"
	}
	if !observed.ObservedAt.IsZero() && !freshObservation(observed.ObservedAt, time.Now(), 90*time.Second) {
		v.Status, v.Label, v.Reason = "stale", "观测已过期", "Laya 组件状态需要新的 VPS 观测"
	}
	if freshObservation(observed.ObservedAt, time.Now(), 90*time.Second) && observed.ImageDigest != "" && observed.DeploymentVersion != "" {
		v.Next = append(v.Next, "install", "remove")
		v.Status, v.Label, v.Reason = "pending", "待验证", "部署权限已准备，运行状态待验证"
		if observed.State == "available" {
			v.Status, v.Label, v.Reason = "ok", "模型可用", "模型运行检查通过；启用后台分析仍需保存接入配置并确认"
			v.Next = append(v.Next, "use")
		}
	}
	if op, ok := operation.(gin.H); ok {
		status, _ := op["status"].(string)
		if slices.Contains([]string{"pending", "leased", "cancelling"}, status) {
			v.Status, v.Label, v.Reason, v.Next = "pending", "正在部署", "等待 VPS 执行和验证回执", []string{}
		} else if slices.Contains([]string{"failed", "expired"}, status) {
			v.Status, v.Label, v.Reason = "failed", "部署未完成", "部署失败或超时，请查看任务阶段和原因后重试"
		}
	}
	return v
}

func (s *Server) previewGateLaya(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	var req struct {
		Operation string `json:"operation"`
	}
	if !bindWorker(c, &req, 4096) {
		return
	}
	if req.Operation != "install" && req.Operation != "remove" && req.Operation != "prepare" {
		failure(c, 400, "invalid_operation")
		return
	}
	if g.Status != "active" || !gateOnline(g) {
		failure(c, 409, "gate_offline")
		return
	}
	if colocated(g) {
		failure(c, 409, "laya_use_center_hosting")
		return
	}
	observed, err := s.gateLayaObservation(c.Request.Context(), g.ID)
	if err != nil {
		failInternal(c, 503, "laya_hosting_unavailable", err)
		return
	}
	if req.Operation != "prepare" && (observed.ImageDigest == "" || observed.DeploymentVersion == "") {
		failure(c, 409, "laya_approved_deployment_required")
		return
	}
	kind := contracts.TaskLayaInstall
	if req.Operation == "prepare" {
		kind = contracts.TaskLayaPrepare
	}
	if req.Operation == "remove" {
		kind = contracts.TaskLayaRemove
	}
	supported, err := s.gateSupportsTask(c.Request.Context(), g, kind)
	if err != nil {
		failInternal(c, 503, "capabilities_unavailable", err)
		return
	}
	if !supported {
		failure(c, 409, "laya_capability_unavailable")
		return
	}
	p := gateLayaPreview{c.GetInt("user_id"), g.ID, req.Operation, observed.ImageDigest, observed.DeploymentVersion, time.Now().Add(10 * time.Minute)}
	id := randomHex(16)
	b, _ := json.Marshal(p)
	if err = s.Store.KV.Set(c.Request.Context(), "laya-gate-preview:"+id, string(b), 10*time.Minute); err != nil {
		failInternal(c, 503, "save_failed", err)
		return
	}
	c.JSON(200, gin.H{"id": id, "gate_name": g.Name, "operation": req.Operation, "version": p.Version, "image_digest": p.Digest, "expires_at": p.Expires})
}

func (s *Server) confirmGateLaya(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	raw, ok, err := s.Store.KV.Get(ctx, "laya-gate-preview:"+c.Param("preview"))
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	var p gateLayaPreview
	if !ok || json.Unmarshal([]byte(raw), &p) != nil || p.Actor != c.GetInt("user_id") || p.GateID != g.ID || time.Now().After(p.Expires) {
		failure(c, 409, "host_preview_expired")
		return
	}
	previewID := c.Param("preview")
	err = s.Store.Lock.Do(ctx, "laya-gate-confirm:"+itoa(g.ID), func(ctx context.Context) error {
		operationID := "laya-gate:" + previewID
		if raw, ok, e := s.Store.KV.Get(ctx, "laya-gate-operation:"+operationID); e != nil {
			return e
		} else if ok {
			var previous layaGateOperation
			if json.Unmarshal([]byte(raw), &previous) == nil && previous.Actor == p.Actor && previous.GateID == g.ID {
				c.JSON(200, gin.H{"operation": previous, "duplicate": true})
				return nil
			}
		}
		if raw, ok, e := s.Store.KV.Get(ctx, "laya-gate-job:"+itoa(g.ID)); e != nil {
			return e
		} else if ok {
			var current layaGateOperation
			if json.Unmarshal([]byte(raw), &current) == nil {
				if current.ID == operationID && current.Actor == p.Actor {
					c.JSON(200, gin.H{"operation": current, "duplicate": true})
					return nil
				}
				if current.TaskID != "" {
					record, taskErr := s.Store.Tasks.Get(ctx, current.TaskID)
					if taskErr != nil {
						return taskErr
					}
					if taskErr == nil && (record.Status == "pending" || record.Status == "leased" || record.Status == "cancelling") {
						failure(c, 409, "laya_operation_in_progress")
						return nil
					}
				}
			}
		}
		observed, e := s.gateLayaObservation(ctx, g.ID)
		if e != nil {
			return e
		}
		if !gateOnline(g) || p.Operation != "prepare" && (observed.ImageDigest != p.Digest || observed.DeploymentVersion != p.Version) {
			failure(c, 409, "laya_deployment_changed")
			return nil
		}
		kind := contracts.TaskLayaInstall
		warmup := layaWarmupSeconds(s.loadLaya(ctx))
		var payload any = contracts.LayaInstallTask{WarmupSeconds: warmup, DeploymentVersion: p.Version, Manifest: json.RawMessage(jsonText(map[string]string{"image_digest": p.Digest}))}
		if p.Operation == "prepare" {
			kind, payload = contracts.TaskLayaPrepare, struct{}{}
		}
		if p.Operation == "remove" {
			kind, payload = contracts.TaskLayaRemove, struct{}{}
		}
		requirements := contracts.TaskRequirements{TargetGateID: g.ID}
		if _, e = s.workerCandidatesFor(ctx, kind, requirements); e != nil {
			return e
		}
		body, e := json.Marshal(payload)
		if e != nil {
			return e
		}
		sealed, e := s.Crypto.Encrypt(string(body))
		if e != nil {
			return e
		}
		// The preview is the durable idempotency key, including if the center
		// stops after enqueueing but before writing the operation pointer.
		record, e := s.Store.Tasks.Enqueue(ctx, taskqueue.Request{Kind: kind, TraceID: taskTrace(ctx), Key: operationID, Payload: body, Requirements: requirements, Candidates: []int{g.ID}, Deadline: time.Now().Add(30 * time.Minute)}, sealed, time.Now())
		if e != nil {
			return e
		}
		operation := layaGateOperation{ID: operationID, TaskID: record.Task.ID, Actor: p.Actor, GateID: g.ID, Operation: p.Operation, TraceID: record.Task.TraceID}
		if e = s.Store.KV.Set(ctx, "laya-gate-operation:"+operationID, jsonText(operation), 0); e != nil {
			return e
		}
		if e = s.Store.KV.Set(ctx, "laya-gate-job:"+itoa(g.ID), jsonText(operation), 0); e != nil {
			return e
		}
		_, e = s.Store.Ent.Audit.Create().SetActor(p.Actor).SetAction("laya." + p.Operation).SetTarget("gate:" + itoa(g.ID)).SetResult("queued").SetDetail(map[string]any{"trace_id": record.Task.TraceID, "task_id": record.Task.ID, "operation_id": operationID}).Save(ctx)
		if e != nil {
			return e
		}
		_ = s.Store.Notifications.Publish(ctx, "workers")
		c.JSON(202, gin.H{"operation": operation})
		return nil
	})
	if err != nil {
		failInternal(c, 503, "laya_hosting_unavailable", err)
	}
}

func (s *Server) auditGateLayaTask(ctx context.Context, record taskqueue.Record) error {
	if record.Status == "pending" || record.Status == "leased" || record.Status == "cancelling" {
		return nil
	}
	return s.Store.Lock.Do(ctx, "laya-gate-audit:"+record.Task.ID, func(ctx context.Context) error {
		key := "laya-gate-audited:" + record.Task.ID
		if _, ok, err := s.Store.KV.Get(ctx, key); err != nil || ok {
			return err
		}
		raw, ok, err := s.Store.KV.Get(ctx, "laya-gate-operation:"+record.Task.IdempotencyKey)
		if err != nil || !ok {
			return err
		}
		var operation layaGateOperation
		if err = json.Unmarshal([]byte(raw), &operation); err != nil {
			return err
		}
		result := "ok"
		if record.Status != "succeeded" {
			result = "failed"
		}
		detail := map[string]any{"trace_id": record.Task.TraceID, "task_id": record.Task.ID, "operation_id": operation.ID, "status": record.Status}
		if record.Error != nil {
			detail["code"] = record.Error.Code
		}
		action, target := "laya."+operation.Operation, "gate:"+itoa(operation.GateID)
		previous, err := s.Store.Ent.Audit.Query().Where(audit.ActionEQ(action), audit.TargetEQ(target), audit.ResultEQ(result)).Select(audit.FieldDetail).All(ctx)
		if err != nil {
			return err
		}
		for _, entry := range previous {
			if entry.Detail["task_id"] == record.Task.ID {
				return s.Store.KV.Set(ctx, key, "1", 0)
			}
		}
		if _, err = s.Store.Ent.Audit.Create().SetActor(operation.Actor).SetAction(action).SetTarget(target).SetResult(result).SetDetail(detail).Save(ctx); err != nil {
			return err
		}
		return s.Store.KV.Set(ctx, key, "1", 0)
	})
}
