package center

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

func TestLayaGateUsesBoundLeasedTaskAndValidatesAnswer(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	caps := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskLayaClassify: 1}}
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1}})
	var first contracts.AgentPollReply
	if code != 200 || json.Unmarshal(body, &first) != nil || first.Config == nil {
		t.Fatalf("poll: %d %s", code, body)
	}
	view := saveVerifiedLayaFixture(t, s, admin, map[string]any{"mode": "gate", "gate_id": g.id, "model": "laya:multilingual"})
	if view["key_set"] != false {
		t.Fatal("Gate requires a center API key")
	}
	settings, ok := s.layaConfig(t.Context())
	if !ok || settings.GateID != int(g.id) {
		t.Fatal("Gate config unavailable")
	}
	resources := contracts.WorkerResources{Concurrency: 1, Components: map[string]contracts.ComponentObservation{"laya": {State: "available", ObservedAt: time.Now().UTC(), Version: "0.18.0"}}}
	if err := s.recordWorkerResources(t.Context(), int(g.id), resources); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	type result struct {
		v   *Verdict
		err error
	}
	done := make(chan result, 1)
	const trace = "0123456789abcdef0123456789abcdef"
	go func() {
		v, err := s.classify(withTrace(ctx, trace), settings, "sanitized test event")
		done <- result{v, err}
	}()
	// 轮询上报的是完整资源快照。漏掉已观测的运行时，会在领取任务前把版本冲掉。
	code, body = g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, ConfigRevision: first.Config.Revision, Resources: resources})
	var reply contracts.AgentPollReply
	if code != 200 || json.Unmarshal(body, &reply) != nil || len(reply.Tasks) != 1 {
		t.Fatalf("task: %d %s", code, body)
	}
	task := reply.Tasks[0]
	if task.Kind != contracts.TaskLayaClassify || task.Requirements.TargetGateID != int(g.id) || task.TraceID != trace {
		t.Fatalf("wrong binding: %+v", task)
	}
	var payload contracts.LayaClassifyTask
	if json.Unmarshal(task.Payload, &payload) != nil || payload.ExpectedRuntimeVersion != "0.18.0" {
		t.Fatal("task omitted observed runtime binding")
	}
	u := contracts.TaskUpdate{Attempt: task.Attempt, LeaseToken: task.LeaseToken, Result: json.RawMessage(`{"answers":{"category":{"choice":"execute_shell","confidence":1}}}`)}
	if code, body = g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", u); code != 400 {
		t.Fatalf("invalid answer: %d %s", code, body)
	}
	u.Result = json.RawMessage(`{"answers":{"category":{"choice":"suspicious","confidence":0.95}}}`)
	u.LeaseToken = strings.Repeat("0", 48)
	if code, _ = g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", u); code != 409 {
		t.Fatalf("stale result accepted: %d", code)
	}
	u.LeaseToken = task.LeaseToken
	if code, body = g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", u); code != 204 {
		t.Fatalf("result: %d %s", code, body)
	}
	select {
	case got := <-done:
		if got.err != nil || got.v == nil || got.v.Outcome != "needed" {
			t.Fatalf("classification: %+v %v", got.v, got.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stored, err := s.Store.Tasks.Get(ctx, task.ID)
	if err != nil || strings.Contains(stored.Ciphertext, "sanitized") || strings.Contains(string(stored.Result), "suspicious") {
		t.Fatalf("plaintext retained: %v", err)
	}
}

func TestLayaClassifyStopsWhenSnapshotDropsRuntime(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	caps := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskLayaClassify: 1}}
	if code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1}}); code != 200 {
		t.Fatalf("poll: %d %s", code, body)
	}
	saveVerifiedLayaFixture(t, s, admin, map[string]any{"mode": "gate", "gate_id": g.id, "model": "laya:multilingual"})
	settings, ok := s.layaConfig(t.Context())
	if !ok {
		t.Fatal("Gate config unavailable")
	}
	observed := contracts.WorkerResources{Concurrency: 1, Components: map[string]contracts.ComponentObservation{"laya": {State: "available", ObservedAt: time.Now().UTC(), Version: "0.18.0"}}}
	if err := s.recordWorkerResources(t.Context(), int(g.id), observed); err != nil {
		t.Fatal(err)
	}
	if code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, ConfigRevision: -1, Resources: contracts.WorkerResources{Concurrency: 1}}); code != 200 {
		t.Fatalf("snapshot: %d %s", code, body)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err := s.classify(withTrace(ctx, "abcdef0123456789abcdef0123456789"), settings, "sanitized test event")
	if err == nil || !strings.Contains(err.Error(), "laya_runtime_version_unknown") {
		t.Fatalf("stale runtime accepted: %v", err)
	}
	listed, err := s.Store.Tasks.List(t.Context(), 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range listed {
		if record.Task.Kind == contracts.TaskLayaClassify {
			t.Fatalf("classification dispatched without a current runtime: %s", record.Task.ID)
		}
	}
}

func TestLayaWorkerFailurePreservesCodeAndSource(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	caps := contracts.CapabilityDocument{Protocol: 2, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskLayaRemove: 1}}
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1}})
	var first contracts.AgentPollReply
	if code != 200 || json.Unmarshal(body, &first) != nil {
		t.Fatalf("%d %s", code, body)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := s.dispatchWorkerTask(ctx, contracts.TaskLayaRemove, contracts.EmptyTask{}, contracts.TaskRequirements{TargetGateID: int(g.id)}, []int{int(g.id)})
		done <- err
	}()
	code, body = g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, ConfigRevision: first.Config.Revision, Resources: contracts.WorkerResources{Concurrency: 1}})
	var reply contracts.AgentPollReply
	if code != 200 || json.Unmarshal(body, &reply) != nil || len(reply.Tasks) != 1 {
		t.Fatalf("%d %s", code, body)
	}
	task := reply.Tasks[0]
	u := contracts.TaskUpdate{Attempt: task.Attempt, LeaseToken: task.LeaseToken, Error: &contracts.TaskError{Code: "laya_container_policy_mismatch", Origin: "gate.layaOperation", Message: "private-token"}}
	if code, _ = g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", u); code != 400 {
		t.Fatalf("raw error accepted: %d", code)
	}
	u.Error.Message = ""
	if code, body = g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", u); code != 204 {
		t.Fatalf("%d %s", code, body)
	}
	select {
	case err := <-done:
		var detail *contracts.TaskError
		if !errors.As(err, &detail) || detail.Code != u.Error.Code || detail.Origin != u.Error.Origin {
			t.Fatalf("specific failure lost: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestLayaDeploymentResultMustMatchApprovedVersionAndDigests(t *testing.T) {
	s, _ := newInstalledServer(t)
	image := "sha256:" + strings.Repeat("a", 64)
	req := contracts.LayaInstallTask{DeploymentVersion: "v0.18.0", Manifest: json.RawMessage(`{"image_digest":"` + image + `"}`)}
	sealed, err := s.Crypto.Encrypt(jsonText(req))
	if err != nil {
		t.Fatal(err)
	}
	record := taskqueue.Record{Task: contracts.Task{Kind: contracts.TaskLayaInstall}, Ciphertext: sealed}
	valid := contracts.LayaDeploymentResult{State: "installed", Version: "v0.18.0", Model: laya.DefaultModel, ModelDigest: laya.ModelManifestSHA256, ImageDigest: image}
	if err = s.validateLayaDeploymentResult(t.Context(), record, json.RawMessage(jsonText(valid))); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*contracts.LayaDeploymentResult){
		func(r *contracts.LayaDeploymentResult) { r.Version = "v0.19.0" },
		func(r *contracts.LayaDeploymentResult) { r.ModelDigest = strings.Repeat("b", 64) },
		func(r *contracts.LayaDeploymentResult) { r.ImageDigest = "sha256:" + strings.Repeat("c", 64) },
		func(r *contracts.LayaDeploymentResult) { r.State = "running" },
	} {
		bad := valid
		mutate(&bad)
		if s.validateLayaDeploymentResult(t.Context(), record, json.RawMessage(jsonText(bad))) == nil {
			t.Fatalf("unapproved result accepted: %+v", bad)
		}
	}
}

func TestLayaCenterLoopbackDoesNotRequireAPIKey(t *testing.T) {
	s, admin := newInstalledServer(t)
	saveVerifiedLayaFixture(t, s, admin, map[string]any{"url": "http://127.0.0.1:11435", "model": "laya:multilingual"})
	if _, ok := s.layaConfig(t.Context()); !ok {
		t.Fatal("official local instance unnecessarily requires API key")
	}
}
