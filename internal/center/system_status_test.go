package center

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/dsh"
	"github.com/shusfun/weir/packages/laya"
)

func TestSystemStatusMissingAndStaleRemainUnknown(t *testing.T) {
	s, admin := newInstalledServer(t)
	now := time.Now().UTC()
	gate := pairFakeGate(t, admin)
	if _, err := s.Store.Ent.Gate.UpdateOneID(int(gate.id)).SetLastSeenAt(now).Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	caps := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", OS: "linux", Tasks: map[string]int{contracts.TaskSubscriptionFetch: 1}}
	if err := s.Store.Tasks.SetCapabilities(t.Context(), int(gate.id), caps); err != nil {
		t.Fatal(err)
	}
	if err := s.recordWorkerResources(t.Context(), int(gate.id), contracts.WorkerResources{Concurrency: 1, Components: map[string]contracts.ComponentObservation{"laya": {State: "available", ObservedAt: now.Add(-3 * time.Minute), Model: laya.DefaultModel, Digest: laya.ModelManifestSHA256}}}); err != nil {
		t.Fatal(err)
	}
	w := admin.do("GET", "/api/v1/admin/workers/system", nil, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("system snapshot: %d %s", w.Code, w.Body.String())
	}
	var snapshot struct {
		Center struct{ Host *contracts.HostSample }
		Gates  []systemGateSnapshot
	}
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Center.Host != nil || len(snapshot.Gates) != 1 || snapshot.Gates[0].Laya.State != "unknown" || snapshot.Gates[0].Laya.Code != "component_observation_stale" || snapshot.Gates[0].Kernels["xray"].State != "unknown" {
		t.Fatalf("unknown or stale observations became healthy: %s", w.Body.String())
	}
	if snapshot.Gates[0].Active == nil || *snapshot.Gates[0].Active != 0 || snapshot.Gates[0].Limit == nil || *snapshot.Gates[0].Limit != 1 {
		t.Fatalf("known worker occupancy missing: %s", w.Body.String())
	}
	if got := processHealth(contracts.ProcessSample{State: "running", ObservedAt: now.Add(time.Hour)}, true, "v1", now); got.State != "unknown" {
		t.Fatal("future process observation accepted")
	}
	if got := processHealth(contracts.ProcessSample{State: "stopped", ObservedAt: now}, true, "v1", now); got.State != "stopped" {
		t.Fatal("confirmed stop reported as unknown")
	}
	if err := s.recordWorkerResources(t.Context(), int(gate.id), contracts.WorkerResources{Concurrency: 1, Components: map[string]contracts.ComponentObservation{"laya": {State: "available", ObservedAt: now.Add(time.Hour)}}}); err == nil {
		t.Fatal("future component observation accepted")
	}
}

func TestSystemStatusDSHProjectionExcludesTokensAndOldHeartbeat(t *testing.T) {
	s, admin := newInstalledServer(t)
	if err := s.saveDSHSettings(t.Context(), DSHSettings{Enabled: true, Version: "test", AutoAnalyze: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s.dsh.mu.Lock()
	s.dsh.domain, s.dsh.verified = "dsh.example.test", true
	s.dsh.desired = dsh.Desired{Generation: 9}
	s.dsh.cacheAt = now
	s.dsh.cache = dsh.AgentStatus{Status: dsh.Status{State: "running", Version: "test", Logs: []string{"PRIVATE_TASK_TEXT"}}, Generation: 9, TrustedHost: "dsh.example.test", Port: 12345, Token: "PRIVATE_LAUNCH_TOKEN", Heartbeat: now}
	s.dsh.mu.Unlock()
	w := admin.do("GET", "/api/v1/admin/workers/system", nil, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE_") || !strings.Contains(w.Body.String(), `"background_enabled":true`) {
		t.Fatalf("dsh status projection: %d %s", w.Code, w.Body.String())
	}
	s.dsh.mu.Lock()
	s.dsh.cacheAt = time.Now()
	s.dsh.cache.Heartbeat = now.Add(-time.Minute)
	s.dsh.mu.Unlock()
	value, err := s.dshHealth(t.Context(), now)
	if err != nil || value["health"].(componentHealth).State != "unknown" || value["background_enabled"] != false {
		t.Fatalf("stale dsh stayed healthy: %+v %v", value, err)
	}
	for _, kind := range []string{"laya", "dsh"} {
		w = admin.do("POST", "/api/v1/admin/workers/diagnostics/preview", map[string]any{"items": []map[string]string{{"kind": kind, "id": "current"}}}, nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE_") {
			t.Fatalf("component diagnostic leaked secrets: %s %d %s", kind, w.Code, w.Body.String())
		}
	}
}

func TestSystemStatusLayaSnapshotFollowsConfiguration(t *testing.T) {
	s, _ := newInstalledServer(t)
	configured := layaSettings{Enabled: true, Mode: "center", URL: "http://127.0.0.1:11435", Model: laya.DefaultModel}
	if _, err := s.Store.Ent.Setting.Create().SetKey("laya").SetValue(toMap(configured)).Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, revision, err := s.readLayaSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s.layaHealth.revision = revision
	s.layaHealth.model = contracts.ComponentObservation{State: "available", ObservedAt: now, Model: laya.DefaultModel, Digest: laya.ModelManifestSHA256}
	value := s.centerLayaHealth(t.Context(), now)
	if value["health"].(componentHealth).State != "available" || value["inference"] != "not_checked" {
		t.Fatalf("model and inference readiness conflated: %+v", value)
	}
	s.layaHealth.revision = "another-configuration"
	if value := s.centerLayaHealth(t.Context(), now); value["health"].(componentHealth).State != "unknown" {
		t.Fatal("old configuration's health applied to new configuration")
	}
}
