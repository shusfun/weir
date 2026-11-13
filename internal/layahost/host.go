// Package layahost is Weir's privileged adapter for the official Laya runtime.
package layahost

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

// Deployment is read from a root-owned approval file, never from an HTTP body.
type Deployment struct {
	Schema  int               `json:"schema"`
	Version string            `json:"version"`
	Model   string            `json:"model"`
	Spec    laya.DockerSpec   `json:"spec"`
	Policy  laya.DockerPolicy `json:"policy"`
}

type Request struct {
	Operation       string `json:"operation"`
	ExpectedDigest  string `json:"expected_digest"`
	ExpectedVersion string `json:"expected_version"`
}

type Response struct {
	Status  laya.DockerStatus `json:"status"`
	Version string            `json:"version"`
	Model   string            `json:"model"`
	Storage *ModelStorage     `json:"storage,omitempty"`
}

func failure(code, origin string) error { return &contracts.TaskError{Code: code, Origin: origin} }

func (d Deployment) Validate() error {
	if d.Schema != 1 || d.Version == "" || len(d.Version) > 100 || d.Model != laya.DefaultModel || d.Spec.Repository != "ghcr.io/ollaya-dev/ollaya" {
		return failure("laya_deployment_policy_mismatch", "layahost.Controller")
	}
	if err := d.Spec.Validate(d.Policy); err != nil {
		return failure("laya_deployment_policy_mismatch", "layahost.Controller")
	}
	return nil
}

type Controller struct {
	mu      sync.Mutex
	Load    func() (Deployment, error)
	Execute laya.DockerExecute
}

func (c *Controller) Operation(ctx context.Context, req Request) (Response, error) {
	var result Response
	switch req.Operation {
	case "ensure", "stop", "remove", "status", "model-storage":
	default:
		return result, failure("unsupported_laya_operation", "layahost.Controller")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if c.Load == nil {
		return result, failure("approved_laya_deployment_unavailable", "layahost.Controller")
	}
	doc, err := c.Load()
	if err != nil {
		return result, failure("approved_laya_deployment_unavailable", "layahost.Controller")
	}
	if doc.Validate() != nil || req.ExpectedDigest != doc.Spec.Digest || req.ExpectedVersion != doc.Version {
		return result, failure("laya_deployment_policy_mismatch", "layahost.Controller")
	}
	execute := c.Execute
	if execute == nil {
		execute = ExecuteDocker
	}
	ctx = laya.WithTrace(ctx, releasefiles.Trace(ctx))
	runtime := laya.DockerRuntime{Spec: doc.Spec, Policy: doc.Policy, Execute: execute, Observer: func(ctx context.Context, event laya.Event) {
		slog.InfoContext(ctx, "laya container stage", "trace", event.TraceID, "stage", event.Operation, "status", event.Status, "code", event.Code, "ms", event.Finished.Sub(event.Started).Milliseconds())
	}}
	result.Version, result.Model = doc.Version, doc.Model
	switch req.Operation {
	case "ensure":
		result.Status, err = runtime.Ensure(ctx)
	case "stop":
		err = runtime.Stop(ctx)
		result.Status.State = laya.StateStopped
	case "remove":
		err = runtime.Remove(ctx)
		result.Status.State = laya.StateStopped
	case "status":
		result.Status, err = runtime.Status(ctx)
	case "model-storage":
		// Validate container ownership and isolation before inspecting its volume.
		result.Status, err = runtime.Status(ctx)
		if err == nil {
			result.Storage, err = inspectModelStorage(ctx, execute, doc.Spec.ModelVolume)
		}
	}
	return result, normalize(err, result.Status.ErrorCode)
}

func normalize(err error, statusCode string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var detail *contracts.TaskError
	if errors.As(err, &detail) {
		return err
	}
	code := "laya_container_operation_failed"
	if errors.Is(err, laya.ErrContainerMissing) {
		code = "laya_container_missing"
	} else if statusCode == "container_policy_mismatch" {
		code = "laya_container_policy_mismatch"
	}
	return failure(code, "layahost.Controller")
}
