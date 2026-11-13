package gate

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/shusfun/weir/internal/publicnet"
	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

type workerObservations struct {
	sync.RWMutex
	sources    map[string]bool
	sourcesAt  time.Time
	components map[string]contracts.ComponentObservation
}

func (a *Agent) workerComponentSnapshot() map[string]contracts.ComponentObservation {
	a.workerObserved.RLock()
	defer a.workerObserved.RUnlock()
	out := make(map[string]contracts.ComponentObservation, len(a.workerObserved.components))
	for name, observation := range a.workerObserved.components {
		out[name] = observation
	}
	return out
}

// Probes run outside the downlink loop so a slow runtime cannot block config
// synchronization. Their results contain no upstream body, endpoint or token.
func (a *Agent) observeWorkerComponents(ctx context.Context) {
	var sourcesAt time.Time
	for ctx.Err() == nil {
		if time.Since(sourcesAt) >= 5*time.Minute {
			sources := probeWorkerSources(ctx, a.workerHTTP(publicnet.Client(3*time.Second)))
			a.workerObserved.Lock()
			a.workerObserved.sources = sources
			a.workerObserved.sourcesAt = time.Now().UTC()
			a.workerObserved.Unlock()
			sourcesAt = time.Now()
		}
		components := map[string]contracts.ComponentObservation{}
		if doc, err := readLayaDeployment(filepath.Join(a.P.Root, "laya")); err == nil && a.helper != nil {
			observed := contracts.ComponentObservation{State: "unknown", Code: "runtime_status_unavailable", ObservedAt: time.Now().UTC(), ImageDigest: doc.Spec.Digest, DeploymentVersion: doc.Version}
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			raw, err := a.helper.Laya(probeCtx, "status", doc.Spec.Digest, doc.Version)
			if err == nil {
				endpoint, endpointErr := layaEndpoint(raw)
				if endpointErr == nil {
					client := laya.Client{BaseURL: endpoint}
					if version, err := client.RuntimeVersion(probeCtx); err == nil {
						observed.Version = version
					}
					model, modelErr := client.ShowModel(probeCtx, doc.Model)
					if modelErr == nil && model.Digest == laya.ModelManifestSHA256 {
						observed.State, observed.Code, observed.Digest, observed.Model = "available", "", model.Digest, model.Name
					} else if modelErr == nil {
						observed.Code = "model_digest_mismatch"
					} else {
						observed.Code = "model_status_unavailable"
					}
				} else {
					observed.Code = "runtime_not_running"
				}
			}
			cancel()
			components["laya"] = observed
		}
		a.workerObserved.Lock()
		a.workerObserved.components = components
		a.workerObserved.Unlock()
		sleep(ctx, time.Minute)
	}
}

func probeWorkerSources(ctx context.Context, client *http.Client) map[string]bool {
	defer client.CloseIdleConnections()
	endpoints := []struct{ name, url string }{
		{"github", "https://api.github.com/"},
		{"huggingface", "https://huggingface.co/"},
		{"hf_mirror", "https://hf-mirror.com/"},
		{"modelscope", "https://modelscope.cn/"},
	}
	out := map[string]bool{}
	for _, endpoint := range endpoints {
		if ctx.Err() != nil {
			break
		}
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		req, err := http.NewRequestWithContext(probeCtx, http.MethodHead, endpoint.url, nil)
		if err == nil {
			var response *http.Response
			response, err = client.Do(req)
			if err == nil {
				out[endpoint.name] = response.StatusCode >= 200 && response.StatusCode < 400
				response.Body.Close()
			}
		}
		if err != nil && ctx.Err() == nil {
			out[endpoint.name] = false
		}
		cancel()
	}
	return out
}
