package gate

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"time"

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

func (a *Agent) layaTask(ctx context.Context, task contracts.Task) (json.RawMessage, error) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return nil, layaTaskFailure("laya_hosting_unsupported", "gate.layaTask")
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
		if contracts.DecodeStrict(task.Payload, &req, contracts.MaxTaskPayload) != nil {
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
		resources := sampleWorkerResources()
		var disk uint64
		for _, file := range model.Files {
			disk += uint64(file.Size)
		}
		if resources.AvailableMemory == nil || resources.AvailableDisk == nil {
			return nil, layaTaskFailure("laya_resources_unknown", "gate.sampleWorkerResources")
		}
		if *resources.AvailableMemory < uint64(model.MinimumMemory) || *resources.AvailableDisk < disk {
			return nil, layaTaskFailure("laya_resources_insufficient", "gate.layaTask")
		}
		_, beforeErr := a.layaHelperOperation(ctx, "status", doc)
		var beforeDetail *contracts.TaskError
		created := errors.As(beforeErr, &beforeDetail) && beforeDetail.Code == "laya_container_missing"
		if beforeErr != nil && !created {
			return nil, beforeErr
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
		if err = client.PrepareModel(laya.WithTrace(ctx, task.TraceID), model, nil); err != nil {
			var detail *laya.APIError
			if errors.As(err, &detail) {
				return nil, layaTaskFailure("laya_"+detail.Code, "laya.Client.PrepareModel")
			}
			return nil, err
		}
		_, err = client.Classify(laya.WithTrace(ctx, task.TraceID), laya.Request{
			Model:     model.Name + "@sha256:" + laya.ModelManifestSHA256,
			State:     "The service has started and is answering a health check.",
			Questions: map[string]laya.Question{"health": {Type: laya.AnswerChoice, Criteria: map[string]string{"ready": "The service is ready.", "unknown": "The service state is unclear."}}},
		})
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
		actualVersion, versionErr := client.RuntimeVersion(laya.WithTrace(ctx, task.TraceID))
		if versionErr != nil || actualVersion != req.ExpectedRuntimeVersion {
			return nil, layaTaskFailure("laya_runtime_version_changed", "laya.Client.RuntimeVersion")
		}
		installed, modelErr := client.ShowModel(laya.WithTrace(ctx, task.TraceID), doc.Model+"@sha256:"+laya.ModelManifestSHA256)
		if modelErr != nil || installed.Digest != laya.ModelManifestSHA256 {
			return nil, layaTaskFailure("laya_model_digest_mismatch", "laya.Client.ShowModel")
		}
		if model == laya.DefaultModel {
			model += "@sha256:" + laya.ModelManifestSHA256
		}
		response, err := client.Classify(laya.WithTrace(ctx, task.TraceID), laya.Request{Model: model, State: req.State, Questions: questions})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, layaTaskFailure("laya_classification_failed", "laya.Client.Classify")
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
		if doc, err := readLayaDeployment(filepath.Join(a.P.Root, "laya")); err == nil && doc.Schema == 1 && doc.Spec.Validate(doc.Policy) == nil {
			for _, kind := range []string{contracts.TaskLayaInstall, contracts.TaskLayaRemove, contracts.TaskLayaClassify} {
				caps.Tasks[kind] = contracts.TaskVersion
			}
		}
	}
	return caps
}
