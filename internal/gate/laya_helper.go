package gate

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/shusfun/weir/internal/layahost"
	"github.com/shusfun/weir/packages/laya"
)

type layaDeployment = layahost.Deployment
type layaHelperRequest = layahost.Request

type layaHelperState struct {
	mu      sync.Mutex
	execute laya.DockerExecute
	load    func() (layaDeployment, error)
}

func (h *Helper) layaOperation(w http.ResponseWriter, r *http.Request) {
	h.laya.mu.Lock()
	defer h.laya.mu.Unlock()
	load := h.laya.load
	if load == nil {
		load = func() (layaDeployment, error) { return readLayaDeployment(filepath.Join(h.P.Root, "laya")) }
	}
	controller := layahost.Controller{Load: load, Execute: h.laya.execute}
	controller.Handler().ServeHTTP(w, r)
}

func (h *helperClient) Laya(ctx context.Context, operation, digest, version string) (json.RawMessage, error) {
	var out json.RawMessage
	err := h.call(ctx, "POST", "/laya", layaHelperRequest{Operation: operation, ExpectedDigest: digest, ExpectedVersion: version}, &out)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil && ctx.Err() == nil {
		// Only fixed helper codes may cross the task boundary. HTTP errors or
		// daemon stderr must not become activity text.
		code := "laya_helper_unavailable"
		switch err.Error() {
		case "approved_laya_deployment_unavailable", "laya_deployment_policy_mismatch", "laya_container_missing", "laya_container_policy_mismatch", "laya_container_operation_failed", "laya_operation_cancelled", "laya_operation_timeout", "laya_docker_unavailable", "laya_image_pull_failed", "laya_docker_output_limit", "laya_resources_unknown", "laya_resources_insufficient":
			code = err.Error()
		}
		return nil, layaTaskFailure(code, "gate.layaOperation")
	}
	return out, err
}
