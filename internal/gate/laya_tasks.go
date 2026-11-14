package gate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/layahost"
	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

func layaTaskFailure(code, origin string) error {
	return &contracts.TaskError{Code: code, Origin: origin}
}

func (a *Agent) layaHelperOperation(ctx context.Context, operation string, doc layaDeployment) (json.RawMessage, error) {
	if operation == "ensure" || operation == "remove" {
		if err := a.checkExecutionLease(ctx); err != nil {
			return nil, err
		}
	}
	finish := beginTaskStage(ctx, "laya.container."+operation, "gate.layaOperation")
	raw, err := a.helper.Laya(ctx, operation, doc.Spec.Digest, doc.Version)
	finish(err)
	return raw, err
}

func (a *Agent) layaTask(ctx context.Context, task contracts.Task) (output json.RawMessage, taskErr error) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return nil, layaTaskFailure("laya_hosting_unsupported", "gate.layaTask")
	}
	if task.Kind == contracts.TaskLayaPrepare {
		if err := a.checkExecutionLease(ctx); err != nil {
			return nil, err
		}
		finish := beginTaskStage(ctx, "laya.host.prepare", "gate.prepareLayaHost")
		defer func() { finish(taskErr) }()
		var result map[string]any
		if err := a.helper.call(ctx, "POST", "/laya/prepare", struct{}{}, &result); err != nil {
			return nil, layaTaskFailure("laya_preparation_unavailable", "gate.prepareLayaHost")
		}
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			var state struct {
				State string `json:"state"`
			}
			if err := a.helper.call(ctx, "GET", "/laya/prepare", nil, &state); err != nil {
				return nil, layaTaskFailure("laya_preparation_unavailable", "gate.prepareLayaHost")
			}
			if state.State == "failed" {
				return nil, layaTaskFailure("laya_preparation_failed", "gate.prepareLayaHost")
			}
			if state.State == "done" {
				doc, e := readLayaDeployment(filepath.Join(a.P.Root, "laya"))
				if e != nil {
					return nil, layaTaskFailure("laya_approved_deployment_required", "gate.prepareLayaHost")
				}
				return json.Marshal(contracts.LayaDeploymentResult{State: "prepared", Version: doc.Version, Model: doc.Model, ImageDigest: doc.Spec.Digest, ModelDigest: laya.ModelManifestSHA256})
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-tick.C:
			}
		}
	}
	doc, err := readLayaDeployment(filepath.Join(a.P.Root, "laya"))
	if err != nil {
		return nil, layaTaskFailure("laya_approved_deployment_required", "gate.readLayaDeployment")
	}
	switch task.Kind {
	case contracts.TaskLayaInstall:
		release, err := a.downloads.acquire(ctx, false)
		if err != nil {
			return nil, err
		}
		defer release()
		var req contracts.LayaInstallTask
		if contracts.DecodeStrict(task.Payload, &req, contracts.MaxTaskPayload) != nil || req.WarmupSeconds < 0 || req.WarmupSeconds > 1800 || req.WarmupSeconds > 0 && req.WarmupSeconds < 30 {
			return nil, layaTaskFailure("laya_install_invalid", "gate.layaTask")
		}
		var manifest struct {
			ImageDigest string `json:"image_digest"`
		}
		if contracts.DecodeStrict(req.Manifest, &manifest, 4096) != nil || manifest.ImageDigest != doc.Spec.Digest || req.DeploymentVersion != doc.Version {
			return nil, layaTaskFailure("laya_approved_deployment_mismatch", "gate.layaTask")
		}
		model := laya.FixedManifest()
		if doc.Model != model.Name || doc.Spec.MemoryBytes < uint64(model.MinimumMemory) {
			return nil, layaTaskFailure("laya_model_policy_mismatch", "gate.layaTask")
		}
		beforeRaw, beforeErr := a.layaHelperOperation(ctx, "status", doc)
		var beforeDetail *contracts.TaskError
		created := errors.As(beforeErr, &beforeDetail) && beforeDetail.Code == "laya_container_missing"
		if beforeErr != nil && !created {
			return nil, beforeErr
		}
		var before struct {
			Status laya.DockerStatus `json:"status"`
		}
		if beforeErr == nil && json.Unmarshal(beforeRaw, &before) != nil {
			return nil, layaTaskFailure("laya_status_invalid", "gate.layaTask")
		}
		resources := sampleWorkerResources()
		if (created || before.Status.State != laya.StateRunning) && resources.AvailableMemory == nil {
			return nil, layaTaskFailure("laya_resources_unknown", "gate.sampleWorkerResources")
		}
		if (created || before.Status.State != laya.StateRunning) && *resources.AvailableMemory < uint64(model.MinimumMemory) {
			return nil, layaTaskFailure("laya_resources_insufficient", "gate.layaTask")
		}
		raw, err := a.layaHelperOperation(ctx, "ensure", doc)
		if err != nil {
			return nil, err
		}
		completed := false
		defer func() {
			if created && !completed {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
				defer cancel()
				_, _ = a.layaHelperOperation(cleanup, "stop", doc)
			}
		}()
		endpoint, err := layaEndpoint(raw)
		if err != nil {
			return nil, err
		}
		client := laya.Client{BaseURL: endpoint, Observer: layaTaskObserver(ctx)}
		storageRaw, err := a.layaHelperOperation(ctx, "model-storage", doc)
		if err != nil {
			return nil, err
		}
		var storage layahost.Response
		if json.Unmarshal(storageRaw, &storage) != nil || storage.Storage == nil {
			return nil, layaTaskFailure("laya_resources_unknown", "gate.layaTask")
		}
		if storage.Storage.AvailableBytes < storage.Storage.RequiredBytes {
			return nil, layaTaskFailure("laya_resources_insufficient", "gate.layaTask")
		}
		if err = client.PrepareModel(laya.WithTrace(ctx, task.TraceID), model, nil); err != nil {
			var detail *laya.APIError
			if errors.As(err, &detail) {
				return nil, layaTaskFailure("laya_"+detail.Code, "laya.Client.PrepareModel")
			}
			return nil, err
		}
		warmup := req.WarmupSeconds
		if warmup == 0 {
			warmup = 300
		}
		finishWarmup := beginTaskStage(ctx, "laya.model.warmup", "laya.Client.Warmup")
		_, err = client.Warmup(laya.WithTrace(ctx, task.TraceID), laya.Request{
			Model:     model.Name + "@sha256:" + laya.ModelManifestSHA256,
			State:     "The service has started and is answering a health check.",
			Questions: map[string]laya.Question{"health": {Type: laya.AnswerChoice, Criteria: map[string]string{"ready": "The service is ready.", "unknown": "The service state is unclear."}}},
		}, time.Duration(warmup)*time.Second)
		finishWarmup(err)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, layaTaskFailure("laya_model_health_failed", "laya.Client.Classify")
		}
		completed = true
		return json.Marshal(contracts.LayaDeploymentResult{State: "installed", Version: doc.Version, Model: model.Name, ModelDigest: laya.ModelManifestSHA256, ImageDigest: doc.Spec.Digest})
	case contracts.TaskLayaRemove:
		if _, err := a.layaHelperOperation(ctx, "remove", doc); err != nil {
			return nil, err
		}
		return json.Marshal(contracts.LayaDeploymentResult{State: "removed", Version: doc.Version, Model: doc.Model, ModelDigest: laya.ModelManifestSHA256, ImageDigest: doc.Spec.Digest})
	case contracts.TaskLayaClassify:
		var req contracts.LayaClassifyTask
		if contracts.DecodeStrict(task.Payload, &req, contracts.MaxTaskPayload) != nil || req.Model != doc.Model && req.Model != doc.Model+"@sha256:"+laya.ModelManifestSHA256 || len(req.Questions) == 0 || len(req.Questions) > 16 {
			return nil, layaTaskFailure("laya_classify_invalid", "gate.layaTask")
		}
		statusRaw, err := a.layaHelperOperation(ctx, "status", doc)
		if err != nil {
			return nil, err
		}
		endpoint, err := layaEndpoint(statusRaw)
		if err != nil {
			return nil, err
		}
		questions := map[string]laya.Question{}
		for key, q := range req.Questions {
			questions[key] = laya.Question{Type: laya.AnswerType(q.Type), Instructions: q.Instructions, Criteria: q.Criteria, Ordinal: q.Ordinal}
		}
		client := laya.Client{BaseURL: endpoint, Observer: layaTaskObserver(ctx)}
		model := req.Model
		if req.ExpectedRuntimeVersion == "" || len(req.ExpectedRuntimeVersion) > 100 {
			return nil, layaTaskFailure("laya_runtime_version_unknown", "gate.layaTask")
		}
		finishVersion := beginTaskStage(ctx, "laya.configuration.check", "laya.Client.RuntimeVersion")
		actualVersion, versionErr := client.RuntimeVersion(laya.WithTrace(ctx, task.TraceID))
		if versionErr == nil && actualVersion != req.ExpectedRuntimeVersion {
			versionErr = layaTaskFailure("laya_runtime_version_changed", "laya.Client.RuntimeVersion")
		}
		finishVersion(versionErr)
		if versionErr != nil || actualVersion != req.ExpectedRuntimeVersion {
			return nil, layaTaskFailure("laya_runtime_version_changed", "laya.Client.RuntimeVersion")
		}
		finishModel := beginTaskStage(ctx, "laya.model.prepare", "laya.Client.ShowModel")
		installed, modelErr := client.ShowModel(laya.WithTrace(ctx, task.TraceID), doc.Model+"@sha256:"+laya.ModelManifestSHA256)
		if modelErr != nil {
			finishModel(modelErr)
			return nil, layaTaskFailure("model_not_ready", "laya.Client.ShowModel")
		}
		if installed.Digest != laya.ModelManifestSHA256 {
			err := layaTaskFailure("laya_model_digest_mismatch", "laya.Client.ShowModel")
			finishModel(err)
			return nil, err
		}
		finishModel(nil)
		if model == laya.DefaultModel {
			model += "@sha256:" + laya.ModelManifestSHA256
		}
		finishInference := beginTaskStage(ctx, "laya.model.inference", "laya.Client.Classify")
		remaining := time.Until(task.Deadline)
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < remaining {
			remaining = time.Until(deadline)
		}
		response, err := client.Warmup(laya.WithTrace(ctx, task.TraceID), laya.Request{Model: model, State: req.State, Questions: questions}, remaining)
		finishInference(err)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, layaTaskFailure(laya.FailureCode(err), "laya.Client.Classify")
		}
		if after, err := client.RuntimeVersion(laya.WithTrace(ctx, task.TraceID)); err != nil || after != actualVersion {
			return nil, layaTaskFailure("laya_runtime_version_changed", "laya.Client.RuntimeVersion")
		}
		return json.Marshal(response)
	default:
		return nil, layaTaskFailure("laya_task_unknown", "gate.layaTask")
	}
}

func layaEndpoint(raw json.RawMessage) (string, error) {
	var status struct {
		Status laya.DockerStatus `json:"status"`
	}
	if json.Unmarshal(raw, &status) != nil || status.Status.State != laya.StateRunning {
		return "", layaTaskFailure("laya_not_running", "gate.layaTask")
	}
	return status.Status.Endpoint, nil
}

func (a *Agent) workerCapabilities() contracts.CapabilityDocument {
	caps := workerCapabilities()
	a.workerObserved.RLock()
	caps.Sources = make(map[string]bool, len(a.workerObserved.sources))
	for name, reachable := range a.workerObserved.sources {
		caps.Sources[name] = reachable
	}
	a.workerObserved.RUnlock()
	for name, observation := range a.workerComponentSnapshot() {
		if observation.Version == "" {
			continue
		}
		if caps.Components == nil {
			caps.Components = map[string]contracts.ComponentCapability{}
		}
		component := contracts.ComponentCapability{Version: observation.Version}
		if observation.Model != "" && observation.Digest != "" {
			component.Models = map[string]string{observation.Model: observation.Digest}
		}
		caps.Components[name] = component
	}
	if runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		if unit, e := os.ReadFile("/etc/systemd/system/weir-laya-prepare.service"); e == nil && strings.Contains(string(unit), "ExecStart=/opt/weir/current/weir laya prepare gate") {
			caps.Tasks[contracts.TaskLayaPrepare] = contracts.TaskVersion
		}
		if doc, err := readLayaDeployment(filepath.Join(a.P.Root, "laya")); err == nil && doc.Schema == 1 && doc.Spec.Validate(doc.Policy) == nil {
			for _, kind := range []string{contracts.TaskLayaInstall, contracts.TaskLayaRemove, contracts.TaskLayaClassify} {
				caps.Tasks[kind] = contracts.TaskVersion
			}
		}
	}
	return caps
}
