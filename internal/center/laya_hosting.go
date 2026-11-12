package center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/layahost"
	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

const layaOperationKind = "laya-host-operation"

type LayaHostConfig struct {
	Directory string `json:"directory,omitempty"`
	Socket    string `json:"socket,omitempty"`
}

type layaHostingState struct {
	mu      sync.Mutex
	load    func() (layahost.Deployment, error)
	operate func(context.Context, layahost.Request) (layahost.Response, error)
}

type layaHostOperation struct {
	ID          string          `json:"id"`
	Operation   string          `json:"operation"`
	TraceID     string          `json:"trace_id"`
	Actor       int             `json:"actor"`
	Revision    string          `json:"deployment_revision"`
	Version     string          `json:"version"`
	ImageDigest string          `json:"image_digest"`
	Status      string          `json:"status"`
	Stage       string          `json:"stage"`
	Code        string          `json:"code,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	Deadline    time.Time       `json:"deadline"`
	FinishedAt  *time.Time      `json:"finished_at,omitempty"`
	Stages      []layaHostStage `json:"stages"`
}

type layaHostStage struct {
	Name     string    `json:"name"`
	Origin   string    `json:"origin"`
	Started  time.Time `json:"started_at"`
	Finished time.Time `json:"finished_at"`
	Code     string    `json:"code,omitempty"`
}

func layaHostingCode(err error) string {
	if err == nil {
		return ""
	}
	var api *laya.APIError
	if errors.As(err, &api) {
		switch api.Code {
		case "model_digest_mismatch", "model_manifest_invalid", "model_not_found", "model_storage_failed", "model_registry_unavailable", "model_pull_interrupted", "model_progress_invalid", "runtime_readiness_timeout", "model_operation_in_progress", "connection_failed":
			return "laya_" + api.Code
		}
		return "laya_model_api_failed"
	}
	switch err.Error() {
	case "laya_deployment_changed", "laya_operation_record_failed", "laya_endpoint_mismatch", "laya_resources_unknown", "laya_resources_insufficient", "laya_model_policy_mismatch":
		return err.Error()
	}
	return layahost.ErrorCode(err)
}

type layaHostPreview struct {
	ID        string              `json:"id"`
	HostID    string              `json:"host_id"`
	Actor     int                 `json:"actor"`
	ExpiresAt time.Time           `json:"expires_at"`
	Operation string              `json:"operation"`
	Revision  string              `json:"deployment_revision"`
	Before    laya.RuntimeState   `json:"before"`
	Approved  layahost.Deployment `json:"approved"`
}

func (s *Server) layaHostID() string {
	name, _ := os.Hostname()
	return digest(name + "\x00" + s.Config.LayaHost.Directory + "\x00" + s.Config.LayaHost.Socket)
}

func (s *Server) layaHostKey(id string) string { return s.layaHostID() + ":" + id }

func (s *Server) layaHostDeployment() (layahost.Deployment, error) {
	s.layaHosting.mu.Lock()
	load := s.layaHosting.load
	s.layaHosting.mu.Unlock()
	if load == nil {
		dir := s.Config.LayaHost.Directory
		if dir == "" {
			dir = "/opt/weir/center-laya"
		}
		load = func() (layahost.Deployment, error) { return layahost.ReadDeployment(dir) }
	}
	doc, err := load()
	if err == nil {
		err = doc.Validate()
	}
	return doc, err
}

func (s *Server) layaHostCall(ctx context.Context, doc layahost.Deployment, operation string) (layahost.Response, error) {
	s.layaHosting.mu.Lock()
	operate := s.layaHosting.operate
	s.layaHosting.mu.Unlock()
	if operate == nil {
		socket := s.Config.LayaHost.Socket
		if socket == "" {
			socket = "/run/weir-laya/helper.sock"
		}
		if !filepath.IsAbs(socket) {
			return layahost.Response{}, errors.New("invalid_laya_helper_socket")
		}
		client := layahost.NewClient(socket)
		defer client.HTTP.CloseIdleConnections()
		operate = client.Operation
	}
	ctx = releasefiles.WithTrace(ctx, "00-"+taskTrace(ctx)+"-"+randomHex(8)+"-01")
	return operate(ctx, layahost.Request{Operation: operation, ExpectedVersion: doc.Version, ExpectedDigest: doc.Spec.Digest})
}

func (s *Server) layaHostingRoutes(admin *gin.RouterGroup) {
	admin.GET("/defense/laya/hosting", s.getLayaHosting)
	admin.POST("/defense/laya/hosting/preview", s.previewLayaHosting)
	admin.POST("/defense/laya/hosting/:id/confirm", s.confirmLayaHosting)
}

func (s *Server) getLayaHosting(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	doc, err := s.layaHostDeployment()
	if err != nil {
		code := "approved_laya_deployment_unavailable"
		if runtime.GOOS != "linux" {
			code = "laya_hosting_unsupported"
		}
		c.JSON(200, gin.H{"available": false, "state": "unknown", "code": code})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	status, err := s.layaHostCall(ctx, doc, "status")
	state, code := status.Status.State, ""
	if err != nil {
		state, code = laya.StateUnknown, layahost.ErrorCode(err)
	}
	if code == "laya_container_missing" {
		state = laya.StateStopped
	}
	rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(layaOperationKind), resource.KeyHasPrefix(s.layaHostKey(""))).Order(ent.Desc(resource.FieldID)).Limit(30).All(ctx)
	if err != nil {
		failInternal(c, 503, "laya_hosting_unavailable", err)
		return
	}
	items := make([]layaHostOperation, 0, len(rows))
	for _, row := range rows {
		var op layaHostOperation
		if decodeStrict(row.Data, &op) != nil {
			failure(c, 503, "laya_hosting_unavailable")
			return
		}
		if op.Status == "running" && time.Now().After(op.Deadline) {
			op.Status, op.Code = "unknown", "laya_operation_interrupted"
		}
		items = append(items, op)
	}
	c.JSON(200, gin.H{"available": true, "state": state, "code": code, "deployment_revision": version(doc), "approved": doc, "operations": items})
}

func (s *Server) previewLayaHosting(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req struct {
		Operation string `json:"operation"`
	}
	if !bindWorker(c, &req, 4096) {
		return
	}
	if req.Operation != "ensure" && req.Operation != "stop" && req.Operation != "remove" {
		failure(c, 400, "invalid_laya_operation")
		return
	}
	count, err := s.Store.KV.Incr(c.Request.Context(), "laya-host-preview-rate:"+itoa(c.GetInt("user_id")), time.Minute)
	if err != nil {
		failInternal(c, 503, "laya_hosting_unavailable", err)
		return
	}
	if count > 5 {
		failure(c, 429, "laya_host_preview_rate_limited")
		return
	}
	doc, err := s.layaHostDeployment()
	if err != nil {
		failure(c, 409, "approved_laya_deployment_unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	status, err := s.layaHostCall(ctx, doc, "status")
	if err != nil && layahost.ErrorCode(err) != "laya_container_missing" {
		failure(c, 409, layahost.ErrorCode(err))
		return
	}
	before := status.Status.State
	if err != nil {
		before = laya.StateStopped
	}
	if req.Operation == "ensure" {
		if err = s.layaHostResources(doc); err != nil {
			failure(c, 409, err.Error())
			return
		}
	}
	p := layaHostPreview{ID: randomHex(24), Actor: c.GetInt("user_id"), HostID: s.layaHostID(), ExpiresAt: time.Now().Add(10 * time.Minute).UTC(), Operation: req.Operation, Revision: version(doc), Before: before, Approved: doc}
	raw, _ := json.Marshal(p)
	sealed, err := s.Crypto.Encrypt(string(raw))
	if err == nil {
		err = s.Store.KV.Set(ctx, "laya-host-preview:"+p.ID, sealed, 10*time.Minute)
	}
	if err != nil {
		failInternal(c, 503, "laya_hosting_unavailable", err)
		return
	}
	c.JSON(200, p)
}

func (s *Server) layaHostResources(doc layahost.Deployment) error {
	model := laya.FixedManifest()
	if doc.Spec.MemoryBytes < uint64(model.MinimumMemory) {
		return errors.New("laya_model_policy_mismatch")
	}
	s.resources.RLock()
	host := snapshotHost(s.resources.host, time.Now())
	s.resources.RUnlock()
	if host == nil || host.MemTotal == nil || host.MemUsed == nil || host.DiskTotal == nil || host.DiskUsed == nil {
		return errors.New("laya_resources_unknown")
	}
	var disk uint64
	for _, file := range model.Files {
		disk += uint64(file.Size)
	}
	if *host.MemUsed > *host.MemTotal || *host.DiskUsed > *host.DiskTotal || *host.MemTotal-*host.MemUsed < uint64(model.MinimumMemory) || *host.DiskTotal-*host.DiskUsed < disk {
		return errors.New("laya_resources_insufficient")
	}
	return nil
}

func (s *Server) confirmLayaHosting(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := c.Param("id")
	if !validTraceHex(id, 48) {
		failure(c, 404, "laya_host_preview_expired")
		return
	}
	ctx := c.Request.Context()
	var op layaHostOperation
	var doc layahost.Deployment
	err := s.Store.Lock.Do(ctx, "laya-host-approval:"+s.layaHostID(), func(ctx context.Context) error {
		if existing, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(layaOperationKind), resource.KeyEQ(s.layaHostKey(id))).Only(ctx); err == nil {
			if err := decodeStrict(existing.Data, &op); err != nil {
				return err
			}
			if op.Actor != c.GetInt("user_id") {
				return errors.New("laya_host_preview_expired")
			}
			return nil
		} else if !ent.IsNotFound(err) {
			return err
		}
		busy, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(layaOperationKind), resource.KeyHasPrefix(s.layaHostKey("")), resource.CreatedAtGT(time.Now().Add(-31*time.Minute)), func(selector *entsql.Selector) {
			selector.Where(sqljson.ValueEQ(resource.FieldData, "running", sqljson.Path("status")))
		}).Exist(ctx)
		if err != nil {
			return err
		}
		if busy {
			return errors.New("laya_host_operation_busy")
		}
		sealed, ok, err := s.Store.KV.Get(ctx, "laya-host-preview:"+id)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("laya_host_preview_expired")
		}
		raw, err := s.Crypto.Decrypt(sealed)
		var p layaHostPreview
		if err != nil || contracts.DecodeStrict([]byte(raw), &p, 32<<10) != nil || p.Actor != c.GetInt("user_id") || p.ID != id || p.HostID != s.layaHostID() || !time.Now().Before(p.ExpiresAt) {
			return errors.New("laya_host_preview_expired")
		}
		doc, err = s.layaHostDeployment()
		if err != nil || version(doc) != p.Revision {
			return errors.New("laya_deployment_changed")
		}
		if p.Operation == "ensure" {
			if err = s.layaHostResources(doc); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		op = layaHostOperation{ID: id, Operation: p.Operation, Actor: p.Actor, TraceID: c.GetString("trace_id"), Revision: p.Revision, Version: doc.Version, ImageDigest: doc.Spec.Digest, Status: "running", Stage: "queued", CreatedAt: now, Deadline: now.Add(30 * time.Minute)}
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.Resource.Create().SetKind(layaOperationKind).SetKey(s.layaHostKey(id)).SetData(toMap(op)).Save(ctx); err != nil {
			return err
		}
		if _, err = tx.Audit.Create().SetActor(op.Actor).SetAction("laya.host.confirm").SetTarget(id).SetResult("accepted").SetDetail(map[string]any{"trace_id": op.TraceID, "operation": op.Operation, "revision": op.Revision}).Save(ctx); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		switch err.Error() {
		case "laya_host_preview_expired":
			failure(c, 404, err.Error())
		case "laya_deployment_changed", "laya_resources_unknown", "laya_resources_insufficient", "laya_model_policy_mismatch", "laya_host_operation_busy":
			failure(c, 409, err.Error())
		default:
			failInternal(c, 503, "laya_hosting_unavailable", err)
		}
		return
	}
	if doc.Version == "" {
		c.JSON(200, gin.H{"operation": op, "duplicate": true})
		return
	}
	job := s.jobs.StartWith(jobSpec{ID: "laya:" + op.ID, Kind: "laya", Title: "中心 Laya 托管", Owner: op.Actor, Trace: op.TraceID, CancelMsg: "已请求取消；Docker 操作可能已完成，请刷新运行状态后重新预演"}, func(ctx context.Context, run JobRun) (map[string]any, error) {
		return s.runLayaHosting(ctx, doc, op, run)
	})
	c.JSON(202, gin.H{"operation": op, "job": job})
}

func (s *Server) saveLayaHostOperation(ctx context.Context, op layaHostOperation) error {
	return s.Store.Ent.Resource.Update().Where(resource.KindEQ(layaOperationKind), resource.KeyEQ(s.layaHostKey(op.ID))).SetData(toMap(op)).Exec(ctx)
}

func (s *Server) runLayaHosting(ctx context.Context, doc layahost.Deployment, op layaHostOperation, run JobRun) (result map[string]any, err error) {
	ctx, cancel := context.WithDeadline(withTrace(ctx, op.TraceID), op.Deadline)
	defer cancel()
	defer func() {
		now := time.Now().UTC()
		op.FinishedAt, op.Status = &now, "succeeded"
		if err != nil {
			op.Status, op.Code = "failed", layaHostingCode(err)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				op.Status = "unknown"
			}
		}
		persist, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		persistErr := s.saveLayaHostOperation(persist, op)
		if persistErr == nil {
			_, persistErr = s.Store.Ent.Audit.Create().SetActor(op.Actor).SetAction("laya.host.result").SetTarget(op.ID).SetResult(op.Status).SetDetail(map[string]any{"trace_id": op.TraceID, "code": op.Code, "stage": op.Stage, "revision": op.Revision}).Save(persist)
		}
		if persistErr != nil {
			err = errors.Join(err, errors.New("laya_operation_record_failed"))
		}
	}()
	err = s.Store.Lock.Do(ctx, "laya-host-runtime:"+s.layaHostID(), func(ctx context.Context) error {
		current, err := s.layaHostDeployment()
		if err != nil || version(current) != op.Revision {
			return errors.New("laya_deployment_changed")
		}
		stage := func(name, origin string, call func() error) error {
			op.Stage = name
			if err := s.saveLayaHostOperation(ctx, op); err != nil {
				return errors.New("laya_operation_record_failed")
			}
			entry := layaHostStage{Name: name, Origin: origin, Started: time.Now().UTC()}
			err := call()
			entry.Finished, entry.Code = time.Now().UTC(), layaHostingCode(err)
			op.Stages = append(op.Stages, entry)
			return err
		}
		// Only a container created by this attempt may be stopped on failure.
		// Never stop an already running instance because a model call failed.
		_, beforeErr := s.layaHostCall(ctx, doc, "status")
		created := layahost.ErrorCode(beforeErr) == "laya_container_missing"
		if beforeErr != nil && !created {
			return beforeErr
		}
		var status layahost.Response
		run.Step("执行已批准的容器操作")
		err = stage("container."+op.Operation, "layahost.Controller", func() error {
			var err error
			status, err = s.layaHostCall(ctx, doc, op.Operation)
			return err
		})
		if err != nil {
			return err
		}
		if op.Operation != "ensure" {
			result = map[string]any{"state": status.Status.State}
			return nil
		}
		completed := false
		defer func() {
			if !created || completed {
				return
			}
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
			defer cancel()
			entry := layaHostStage{Name: "container.rollback.stop", Origin: "layahost.Controller", Started: time.Now().UTC()}
			_, rollbackErr := s.layaHostCall(cleanup, doc, "stop")
			entry.Finished, entry.Code = time.Now().UTC(), layaHostingCode(rollbackErr)
			op.Stages = append(op.Stages, entry)
		}()
		endpoint := status.Status.Endpoint
		if endpoint != "http://127.0.0.1:"+itoa(doc.Spec.HostPort) {
			return errors.New("laya_endpoint_mismatch")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		defer transport.CloseIdleConnections()
		client := laya.Client{BaseURL: endpoint, HTTP: &http.Client{Transport: transport, Timeout: 30 * time.Minute}}
		run.Step("下载并校验官方固定模型")
		err = stage("model.install", "laya.Client.PrepareModel", func() error {
			return client.PrepareModel(laya.WithTrace(ctx, op.TraceID), laya.FixedManifest(), func(p laya.ModelProgress) { run.Stage(p.Stage) })
		})
		if err != nil {
			return err
		}
		run.Step("检查官方模型推理")
		err = stage("model.health", "laya.Client.Classify", func() error {
			inference, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			_, err := client.Classify(laya.WithTrace(inference, op.TraceID), laya.Request{Model: doc.Model + "@sha256:" + laya.ModelManifestSHA256, State: "The service is answering a synthetic health check.", Questions: map[string]laya.Question{"health": {Type: laya.AnswerChoice, Criteria: map[string]string{"ready": "The service answered the health check.", "unknown": "The service state is unclear."}}}})
			return err
		})
		completed = err == nil
		if err == nil {
			result = map[string]any{"state": "installed", "version": doc.Version, "model": doc.Model, "model_digest": laya.ModelManifestSHA256, "endpoint": endpoint, "connection": "not_enabled"}
		}
		return err
	})
	return result, err
}
