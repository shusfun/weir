package center

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/diagnostic"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/packages/contracts"
)

type componentHealth struct {
	State      string     `json:"state"`
	Code       string     `json:"code,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	Version    string     `json:"version,omitempty"`
	Model      string     `json:"model,omitempty"`
	Digest     string     `json:"digest,omitempty"`
}

func diagnosticComponent(health componentHealth, prefix string, external map[string]string) componentHealth {
	if health.Version != "" {
		external[prefix+".reported_version"] = diagnostic.Text(health.Version)
	}
	if health.Model != "" {
		external[prefix+".reported_model"] = diagnostic.Text(health.Model)
	}
	health.Version, health.Model = "", ""
	return health
}

type systemGateSnapshot struct {
	ID                int                           `json:"id"`
	Name              string                        `json:"name"`
	State             string                        `json:"state"`
	LastSeen          *time.Time                    `json:"last_seen_at"`
	SourcesObservedAt *time.Time                    `json:"sources_observed_at"`
	Capabilities      *contracts.CapabilityDocument `json:"capabilities"`
	Resources         *workerResourceObservation    `json:"resources"`
	Host              *contracts.HostSample         `json:"host"`
	Active            *int                          `json:"active"`
	Limit             *int                          `json:"concurrency_limit"`
	Kernels           map[string]componentHealth    `json:"kernels"`
	Laya              componentHealth               `json:"laya"`
}

func freshObservation(at time.Time, now time.Time, ttl time.Duration) bool {
	return !at.IsZero() && !at.After(now.Add(time.Minute)) && now.Sub(at) <= ttl
}

func processHealth(sample contracts.ProcessSample, present bool, version string, now time.Time) componentHealth {
	out := componentHealth{State: "unknown", Code: "process_observation_missing", Version: version}
	if !present || sample.ObservedAt.IsZero() {
		return out
	}
	at := sample.ObservedAt
	out.ObservedAt = &at
	if !freshObservation(at, now, 2*time.Minute) {
		out.Code = "process_observation_stale"
		return out
	}
	if sample.Error != "" {
		out.Code = "process_observation_failed"
		return out
	}
	if sample.State == "running" || sample.State == "stopped" {
		out.State, out.Code = sample.State, ""
	}
	return out
}

func observedComponent(observation contracts.ComponentObservation, now time.Time) componentHealth {
	at := observation.ObservedAt
	out := componentHealth{State: "unknown", Code: "component_observation_stale", ObservedAt: &at, Version: observation.Version, Model: observation.Model, Digest: observation.Digest}
	if observation.Validate() == nil && freshObservation(at, now, 2*time.Minute) {
		out.State, out.Code = observation.State, observation.Code
	}
	return out
}

func snapshotHost(host *contracts.HostSample, now time.Time) *contracts.HostSample {
	if host == nil || !freshObservation(host.ObservedAt, now, 2*time.Minute) {
		return nil
	}
	copy := *host
	copy.Processes = nil
	return &copy
}

func (s *Server) systemStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ctx, now := c.Request.Context(), time.Now().UTC()
	rows, err := s.Store.Ent.Gate.Query().Order(ent.Asc(gate.FieldID)).All(ctx)
	if err != nil {
		failInternal(c, 503, "system_status_unavailable", err)
		return
	}
	inventory, err := s.Store.Tasks.WorkerInventory(ctx)
	if err != nil {
		failInternal(c, 503, "system_status_unavailable", err)
		return
	}
	policy, budget, err := s.workerPolicy(ctx)
	if err != nil {
		failInternal(c, 503, "system_status_unavailable", err)
		return
	}
	items := make([]systemGateSnapshot, 0, len(rows))
	for _, g := range rows {
		item := systemGateSnapshot{ID: g.ID, Name: g.Name, State: "unknown", LastSeen: g.LastSeenAt, Kernels: map[string]componentHealth{}, Laya: componentHealth{State: "unknown", Code: "component_observation_missing"}}
		if g.Status == gate.StatusRevoked {
			item.State = "revoked"
		} else if g.LastSeenAt != nil {
			item.State = "offline"
			if freshObservation(*g.LastSeenAt, now, 90*time.Second) {
				item.State = "online"
			}
		}
		var host contracts.HostSample
		if data, err := json.Marshal(g.Host); err == nil {
			_ = json.Unmarshal(data, &host)
		}
		if item.State == "online" {
			item.Host = snapshotHost(&host, now)
		}
		for _, name := range []string{"xray", "sing-box"} {
			process, ok := host.Processes[name]
			item.Kernels[name] = processHealth(process, ok && item.State == "online", g.KernelsRunning[name], now)
		}
		if worker, ok := inventory[g.ID]; ok {
			caps, active := worker.Capabilities, worker.Active
			item.Capabilities, item.Active = &caps, &active
		}
		res, err := s.workerResources(ctx, g.ID)
		if err != nil {
			failInternal(c, 503, "system_status_unavailable", err)
			return
		}
		if res != nil && item.State == "online" {
			item.Resources = res
			item.SourcesObservedAt = res.Resources.SourcesObservedAt
			limit := min(res.Resources.Concurrency, budget.Concurrency)
			item.Limit = &limit
			if observation, ok := res.Resources.Components["laya"]; ok {
				item.Laya = observedComponent(observation, now)
			}
		}
		items = append(items, item)
	}
	s.resources.RLock()
	host := snapshotHost(s.resources.host, now)
	s.resources.RUnlock()
	dsh, err := s.dshHealth(ctx, now)
	if err != nil {
		failInternal(c, 503, "system_status_unavailable", err)
		return
	}
	c.JSON(200, gin.H{"observed_at": now, "center": gin.H{"environment": diagnosticEnvironment(), "host": host}, "gates": items, "dsh": dsh, "laya": s.centerLayaHealth(ctx, now), "worker_policy_revision": policy.Revision})
}

func (s *Server) dshHealth(ctx context.Context, now time.Time) (gin.H, error) {
	var configured DSHSettings
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(dshSettingKey)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	if err == nil {
		raw, err := json.Marshal(row.Value)
		if err != nil {
			return nil, err
		}
		if err := contracts.DecodeStrict(raw, &configured, 16<<10); err != nil {
			return nil, err
		}
	}
	out := componentHealth{State: "disabled"}
	status := s.agentStatus()
	if configured.Enabled {
		out.State, out.Code = "unknown", "supervisor_observation_missing"
		if !status.Heartbeat.IsZero() {
			at := status.Heartbeat
			out.ObservedAt, out.Version = &at, status.Version
			out.Code = "supervisor_observation_stale"
			if freshObservation(at, now, 30*time.Second) {
				out.Code = "desired_state_not_ready"
				if _, _, ok := s.dshEndpoint(); ok {
					out.State, out.Code = "running", ""
				} else if status.State == "failed" {
					out.State, out.Code = "failed", "runtime_failed"
				} else if status.State == "stopped" {
					out.State, out.Code = "stopped", ""
				}
			}
		}
	}
	s.dsh.mu.Lock()
	verified, wantedGeneration := s.dsh.verified, s.dsh.desired.Generation
	s.dsh.mu.Unlock()
	return gin.H{"health": out, "background_enabled": configured.Enabled && configured.AutoAnalyze && verified && out.State == "running", "plugin_verified": verified, "generation": status.Generation, "desired_generation": wantedGeneration}, nil
}
