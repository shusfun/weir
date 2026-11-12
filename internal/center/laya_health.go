package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

type layaHealthState struct {
	sync.RWMutex
	revision string
	model    contracts.ComponentObservation
	traceID  string
}

func (s *Server) readLayaSettings(ctx context.Context) (layaSettings, string, error) {
	configured := layaSettings{Mode: "center", URL: "http://127.0.0.1:11435", Model: laya.DefaultModel}
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ("laya")).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return configured, "", err
	}
	if err == nil {
		raw, err := json.Marshal(row.Value)
		if err != nil {
			return configured, "", err
		}
		if err := contracts.DecodeStrict(raw, &configured, 16<<10); err != nil {
			return configured, "", err
		}
	}
	if configured.Mode == "" {
		configured.Mode = "center"
	}
	raw, err := json.Marshal(configured)
	if err != nil {
		return configured, "", err
	}
	digest := sha256.Sum256(raw)
	return configured, hex.EncodeToString(digest[:]), nil
}

func (s *Server) centerLayaHealth(ctx context.Context, now time.Time) gin.H {
	configured, revision, err := s.readLayaSettings(ctx)
	observation := componentHealth{State: "unknown", Code: "model_observation_missing"}
	if err != nil {
		observation.Code = "configuration_unavailable"
		return gin.H{"health": observation, "inference": "not_checked"}
	}
	out := gin.H{"mode": configured.Mode, "gate_id": configured.GateID, "configured_model": configured.Model, "configuration_revision": revision, "inference": "not_checked"}
	out["compatibility"] = s.currentLayaCompatibility(ctx, configured)
	if !configured.Enabled {
		observation.State, observation.Code = "disabled", ""
	} else if configured.Mode == "gate" {
		observation.Code = "see_gate_observation"
	} else {
		s.layaHealth.RLock()
		if s.layaHealth.revision == revision {
			observation = observedComponent(s.layaHealth.model, now)
			out["trace_id"] = s.layaHealth.traceID
		}
		s.layaHealth.RUnlock()
	}
	out["health"] = observation
	return out
}

func (s *Server) observeLayaHealth(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		s.state.RLock()
		ready := s.Config.Installed && s.Store != nil
		if ready {
			s.probeCenterLaya(ctx)
		}
		s.state.RUnlock()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) probeCenterLaya(ctx context.Context) {
	configured, revision, err := s.readLayaSettings(ctx)
	if err != nil || !configured.Enabled || configured.Mode != "center" {
		return
	}
	trace := randomHex(16)
	ctx, cancel := context.WithTimeout(withTrace(ctx, trace), 5*time.Second)
	defer cancel()
	observation := contracts.ComponentObservation{State: "unknown", Code: "model_status_unavailable", ObservedAt: time.Now().UTC()}
	key := configured.Key
	if key != "" {
		key, err = s.reveal(key)
	}
	if err == nil {
		// Runtime connections never inherit environment proxy credentials.
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client := laya.Client{BaseURL: configured.URL, APIKey: key, HTTP: &http.Client{Transport: transport, Timeout: 5 * time.Second}, Observer: func(ctx context.Context, event laya.Event) {
			slog.InfoContext(ctx, "laya health observation", "trace", trace, "operation", event.Operation, "status", event.Status, "code", event.Code, "ms", event.Finished.Sub(event.Started).Milliseconds())
		}}
		model, modelErr := client.ShowModel(laya.WithTrace(ctx, trace), configured.Model)
		if modelErr == nil {
			observation.State, observation.Code, observation.Model, observation.Digest = "available", "", model.Name, model.Digest
			if runtimeVersion, err := client.RuntimeVersion(laya.WithTrace(ctx, trace)); err == nil {
				observation.Version = runtimeVersion
			}
			if strings.Split(configured.Model, "@")[0] == laya.DefaultModel && model.Digest != laya.ModelManifestSHA256 {
				observation.State, observation.Code = "unknown", "model_digest_mismatch"
			}
		}
		transport.CloseIdleConnections()
	} else {
		observation.Code = "runtime_credentials_unavailable"
	}
	observation.ObservedAt = time.Now().UTC()
	s.layaHealth.Lock()
	s.layaHealth.revision, s.layaHealth.model, s.layaHealth.traceID = revision, observation, trace
	s.layaHealth.Unlock()
}
