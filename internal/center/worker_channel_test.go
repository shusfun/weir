package center

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/gateaction"
	"github.com/shusfun/weir/packages/contracts"
)

func TestWorkerPollDeliversCertReplacementWithoutRevisionBump(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	caps := contracts.CapabilityDocument{Protocol: 2, Version: "test", Arch: "amd64"}
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1}})
	if code != 200 {
		t.Fatalf("poll: %d %s", code, body)
	}
	var first contracts.AgentPollReply
	if json.Unmarshal(body, &first) != nil || first.Config == nil {
		t.Fatalf("config missing: %s", body)
	}
	if err := s.Store.KV.Set(t.Context(), certPushKey(int(g.id)), "1", 0); err != nil {
		t.Fatal(err)
	}
	code, body = g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, ConfigRevision: first.Config.Revision, Resources: contracts.WorkerResources{Concurrency: 1}})
	var next contracts.AgentPollReply
	if json.Unmarshal(body, &next) != nil || code != 200 || next.Config == nil || !next.Config.CertOnly || next.Config.Revision != first.Config.Revision {
		t.Fatalf("cert replacement was not delivered on the current revision: %d %s", code, body)
	}
}

func TestWorkerPollDeliversExecutionWithoutRevisionBump(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	caps := contracts.CapabilityDocument{Protocol: 2, Version: "test", Arch: "amd64"}
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1}})
	var first contracts.AgentPollReply
	if json.Unmarshal(body, &first) != nil || code != 200 || first.Config == nil {
		t.Fatalf("first config: %d %s", code, body)
	}
	u := s.Store.Ent.User.Create().SetEmail("execution@test.local").SetPasswordHash("unused").SetQuota(1000).SaveX(t.Context())
	row := s.Store.Ent.Budget.Create().SetGateID(int(g.id)).SetUserID(u.ID).SetAllocated(700).SetUsed(100).SetRevision(unconfirmable).SaveX(t.Context())
	if err := s.publishExecution(t.Context(), int(g.id)); err != nil {
		t.Fatal(err)
	}
	code, body = g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, ConfigRevision: first.Config.Revision, Resources: contracts.WorkerResources{Concurrency: 1}})
	var next contracts.AgentPollReply
	if json.Unmarshal(body, &next) != nil || code != 200 || next.Config == nil || !next.Config.ExecutionOnly || next.Config.Revision != first.Config.Revision || next.Config.ExecutionSeq == 0 {
		t.Fatalf("execution missing: %d %s", code, body)
	}
	if b := next.Config.Budgets[userKey(u.ID)]; b.Allocated != 700 || b.Used != 100 {
		t.Fatalf("budget: %+v", b)
	}
	// 旧通知不能确认后来分配的额度。
	row.Update().SetAllocated(900).ExecX(t.Context())
	if err := s.publishExecution(t.Context(), int(g.id)); err != nil {
		t.Fatal(err)
	}
	if err := s.confirmExecution(t.Context(), int(g.id), next.Config.ExecutionSeq); err != nil {
		t.Fatal(err)
	}
	if got := s.Store.Ent.Budget.GetX(t.Context(), row.ID); got.Confirmed != 0 {
		t.Fatalf("stale acknowledgement: %+v", got)
	}
	current, err := s.loadExecution(t.Context(), int(g.id))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.confirmExecution(t.Context(), int(g.id), current.Seq); err != nil {
		t.Fatal(err)
	}
	if got := s.Store.Ent.Budget.GetX(t.Context(), row.ID); got.Confirmed != 900 {
		t.Fatalf("latest acknowledgement: %+v", got)
	}
}

func TestWorkerChannelConfigurationTaskAndLease(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	ctx := context.Background()
	caps := contracts.CapabilityDocument{Protocol: 2, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.ActionLiveStart: 1}}
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1}})
	if code != 200 {
		t.Fatalf("poll: %d %s", code, body)
	}
	var first contracts.AgentPollReply
	if e := json.Unmarshal(body, &first); e != nil || first.Config == nil {
		t.Fatalf("config missing: %s %v", body, e)
	}
	a, e := s.enqueueAction(ctx, int(g.id), contracts.ActionLiveStart, map[string]any{"seconds": 30}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := a.Payload["seconds"]; ok {
		t.Fatal("task body persisted in legacy row")
	}
	code, body = g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, ConfigRevision: first.Config.Revision, Resources: contracts.WorkerResources{Concurrency: 1}})
	var next contracts.AgentPollReply
	_ = json.Unmarshal(body, &next)
	if code != 200 || len(next.Tasks) != 1 {
		t.Fatalf("task delivery: %d %s", code, body)
	}
	task := next.Tasks[0]
	if !validTraceHex(task.TraceID, 32) {
		t.Fatalf("background task has no root trace: %q", task.TraceID)
	}
	if string(task.Payload) != `{"seconds":30}` || task.Kind != contracts.ActionLiveStart || task.LeaseToken == "" {
		t.Fatalf("task: %+v", task)
	}
	u := contracts.TaskUpdate{Attempt: task.Attempt, LeaseToken: strings.Repeat("0", 48), Result: json.RawMessage(`{}`)}
	code, _ = g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", u)
	if code != 409 {
		t.Fatalf("stale lease accepted: %d", code)
	}
	other := pairFakeGate(t, admin)
	u.LeaseToken = task.LeaseToken
	code, _ = g.call("POST", "/api/v1/agent/actions/"+itoa(a.ID), contracts.ActionResult{OK: true})
	if code != 409 {
		t.Fatalf("legacy endpoint bypassed lease: %d", code)
	}
	code, _ = other.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", u)
	if code != 409 {
		t.Fatalf("wrong Gate accepted: %d", code)
	}
	code, body = g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", u)
	if code != 204 {
		t.Fatalf("result: %d %s", code, body)
	}
	got, e := s.waitAction(ctx, a.ID, time.Second)
	if e != nil || got.Status != gateaction.StatusDone {
		t.Fatalf("legacy projection: %v %v", got, e)
	}
	stored, e := s.Store.Tasks.Get(ctx, task.ID)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(stored.Ciphertext, "seconds") {
		t.Fatal("task plaintext persisted")
	}
}

func TestWorkerUpgradeMigratesPendingAndAcknowledgesCancellation(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	ctx := context.Background()
	a, err := s.enqueueAction(ctx, int(g.id), contracts.ActionPanelConfigure, map[string]any{"nonce": "one-time", "port": 18276, "https": false}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	caps := contracts.CapabilityDocument{Protocol: 2, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.ActionPanelConfigure: 1}}
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1}})
	var reply contracts.AgentPollReply
	if err = json.Unmarshal(body, &reply); err != nil || code != 200 || len(reply.Tasks) != 1 {
		t.Fatalf("migration: %d %s %v", code, body, err)
	}
	task := reply.Tasks[0]
	if task.Kind != a.Kind || !strings.Contains(string(task.Payload), `"port":18276`) {
		t.Fatalf("lost settings: %+v", task)
	}
	stored, _ := s.Store.Ent.GateAction.Get(ctx, a.ID)
	if stored.Status != gateaction.StatusRunning || len(stored.Payload) != 1 {
		t.Fatalf("legacy row: %+v", stored)
	}
	if err = s.migratePendingActions(ctx, int(g.id)); err != nil {
		t.Fatal(err)
	}
	rs, err := s.Store.Tasks.List(ctx, 10)
	if err != nil || len(rs) != 1 {
		t.Fatalf("migration duplicated: %d %v", len(rs), err)
	}
	if err = s.Store.Tasks.Cancel(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	u := contracts.TaskUpdate{Attempt: task.Attempt, LeaseToken: task.LeaseToken}
	code, body = g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/cancelled", u)
	if code != 204 {
		t.Fatalf("ack: %d %s", code, body)
	}
	ids, err := s.Store.Tasks.Cancellations(ctx, int(g.id))
	if err != nil || len(ids) != 0 {
		t.Fatalf("repeated cancellation: %v %v", ids, err)
	}
	stored, _ = s.Store.Ent.GateAction.Get(ctx, a.ID)
	if stored.Status != gateaction.StatusFailed || stored.Error != "task_cancelled" {
		t.Fatalf("cancel projection: %+v", stored)
	}
}

func TestWorkerChannelStrictEnvelope(t *testing.T) {
	_, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	for _, req := range []any{
		map[string]any{"protocol": 2, "resources": map[string]any{"concurrency": 1}, "shell": "id"},
		contracts.AgentPoll{Protocol: 999, Resources: contracts.WorkerResources{Concurrency: 1}},
		contracts.AgentPoll{Protocol: 2, Resources: contracts.WorkerResources{Concurrency: 100}},
	} {
		code, body := g.call("POST", "/api/v1/agent/poll", req)
		if code != 400 {
			t.Fatalf("invalid envelope: %d %s", code, body)
		}
	}
}
