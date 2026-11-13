package center

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
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
	out := gin.H{"gate_id": g.ID, "gate_name": g.Name, "online": gateOnline(g), "colocated": colocated(g), "component": observed, "prepared": observed.ImageDigest != "" && observed.DeploymentVersion != ""}
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
	c.Header("Cache-Control", "no-store")
	c.JSON(200, out)
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
	if req.Operation != "install" && req.Operation != "remove" {
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
	if observed.ImageDigest == "" || observed.DeploymentVersion == "" {
		failure(c, 409, "laya_approved_deployment_required")
		return
	}
	kind := contracts.TaskLayaInstall
	if req.Operation == "remove" {
		kind = contracts.TaskLayaRemove
	}
	supported, err := s.gateSupportsTask(c.Request.Context(), g, kind, 0)
	if err != nil {
		failInternal(c, 503, "capabilities_unavailable", err)
		return
	}
	if !supported {
		failure(c, 409, "agent_upgrade_required")
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
		if !gateOnline(g) || observed.ImageDigest != p.Digest || observed.DeploymentVersion != p.Version {
			failure(c, 409, "laya_deployment_changed")
			return nil
		}
		kind := contracts.TaskLayaInstall
		var payload any = contracts.LayaInstallTask{DeploymentVersion: p.Version, Manifest: json.RawMessage(jsonText(map[string]string{"image_digest": p.Digest}))}
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
		if _, err = s.Store.Ent.Audit.Create().SetActor(operation.Actor).SetAction("laya." + operation.Operation).SetTarget("gate:" + itoa(operation.GateID)).SetResult(result).SetDetail(detail).Save(ctx); err != nil {
			return err
		}
		return s.Store.KV.Set(ctx, key, "1", 0)
	})
}
