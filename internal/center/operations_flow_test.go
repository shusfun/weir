package center

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

func TestGateLayaDurableOperationTrace(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	ctx := t.Context()
	caps := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskLayaInstall: contracts.TaskVersion, contracts.TaskLayaRemove: contracts.TaskVersion}}
	if err := s.Store.Tasks.SetCapabilities(ctx, int(g.id), caps); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	res := contracts.WorkerResources{Concurrency: 1, Components: map[string]contracts.ComponentObservation{"laya": {State: "unknown", ObservedAt: time.Now().UTC(), ImageDigest: digest, DeploymentVersion: laya.OllayaVersion}}}
	if err := s.recordWorkerResources(ctx, int(g.id), res); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/admin/gates/" + itoa(int(g.id)) + "/laya"
	preview := mustJSON(t, admin, "POST", base+"/preview", map[string]any{"operation": "install"}, 200)
	const trace = "0123456789abcdef0123456789abcdef"
	w := admin.do("POST", base+"/"+preview["id"].(string)+"/confirm", nil, map[string]string{"traceparent": "00-" + trace + "-0123456789abcdef-01"})
	if w.Code != 202 {
		t.Fatalf("confirm %d %s", w.Code, w.Body)
	}
	operation := decode(t, w)["operation"].(map[string]any)
	taskID := operation["task_id"].(string)
	if operation["trace_id"] != trace {
		t.Fatal("trace lost at enqueue", operation)
	}
	duplicate := mustJSON(t, admin, "POST", base+"/"+preview["id"].(string)+"/confirm", nil, 200)
	if duplicate["operation"].(map[string]any)["task_id"] != taskID {
		t.Fatal("duplicate task")
	}
	second := mustJSON(t, admin, "POST", base+"/preview", map[string]any{"operation": "remove"}, 200)
	mustJSON(t, admin, "POST", base+"/"+second["id"].(string)+"/confirm", nil, 409)
	// The public state is reconstructed from the queue, without a UI job.
	state := mustJSON(t, admin, "GET", base, nil, 200)
	if state["operation"].(map[string]any)["status"] != "pending" {
		t.Fatal(state)
	}
	if count := s.Store.Ent.Node.Query().CountX(ctx); count != 0 {
		t.Fatal("Laya created a proxy node")
	}
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: contracts.AgentProtocol, Capabilities: &caps, Resources: res})
	var reply contracts.AgentPollReply
	if code != 200 || json.Unmarshal(body, &reply) != nil {
		t.Fatalf("poll %d %s", code, body)
	}
	if len(reply.Tasks) == 0 {
		revision := int64(0)
		if reply.Config != nil {
			revision = reply.Config.Revision
		}
		code, body = g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: contracts.AgentProtocol, ConfigRevision: revision, Resources: res})
		if code != 200 || json.Unmarshal(body, &reply) != nil {
			t.Fatalf("poll %d %s", code, body)
		}
	}
	if len(reply.Tasks) != 1 || reply.Tasks[0].ID != taskID || reply.Tasks[0].TraceID != trace {
		t.Fatalf("wrong lease %+v", reply)
	}
	task := reply.Tasks[0]
	result := contracts.LayaDeploymentResult{State: "installed", Version: laya.OllayaVersion, Model: laya.DefaultModel, ModelDigest: laya.ModelManifestSHA256, ImageDigest: digest}
	u := contracts.TaskUpdate{Attempt: task.Attempt, LeaseToken: task.LeaseToken, Result: json.RawMessage(jsonText(result))}
	for i := 0; i < 2; i++ {
		if code, body = g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", u); code != 204 {
			t.Fatalf("result %d %s", code, body)
		}
	}
	state = mustJSON(t, admin, "GET", base, nil, 200)
	if state["operation"].(map[string]any)["status"] != "succeeded" {
		t.Fatal(state)
	}
	rows := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("laya.install"), audit.ResultEQ("ok")).AllX(ctx)
	if len(rows) != 1 || rows[0].Detail["trace_id"] != trace || rows[0].Detail["task_id"] != taskID {
		t.Fatalf("audit mismatch %+v", rows)
	}
	t.Logf("trace=%s task=%s gate=%d stages=HTTP,queue,lease,validated-result,audit", trace, taskID, g.id)
}

func TestAuditFiltersAndSummaryIncludeAllPages(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	builders := make([]*ent.AuditCreate, 1105)
	for i := range builders {
		result := "ok"
		if i%2 == 0 {
			result = "denied policy"
		}
		builders[i] = s.Store.Ent.Audit.Create().SetActor(1).SetAction("fixture.aggregate").SetTarget("aggregate-fixture").SetResult(result).SetCreatedAt(time.Now().Add(-time.Minute))
	}
	s.Store.Ent.Audit.CreateBulk(builders...).SaveX(ctx)
	filtered := mustJSON(t, admin, "GET", "/api/v1/admin/audit?kind=failed&q=aggregate-fixture&page_size=20&page=2", nil, 200)
	if filtered["total"] != float64(553) || len(filtered["items"].([]any)) != 20 {
		t.Fatal(filtered)
	}
	start := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	summary := mustJSON(t, admin, "GET", "/api/v1/admin/audit/summary?days=1&today_from="+url.QueryEscape(start), nil, 200)
	total := 0
	for _, v := range summary["buckets"].([]any) {
		total += int(v.(map[string]any)["total"].(float64))
	}
	if total < 1105 || summary["today"].(map[string]any)["failed"].(float64) < 553 {
		t.Fatal("truncated summary", summary)
	}
}

func TestSubscriptionPreviewAuthorizationAndNoPullRecord(t *testing.T) {
	s, admin, g := setupGate(t)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "ss2022"}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "preview@example.test", "password": "Passw0rd", "quota": int64(10 << 30), "all_nodes": false, "node_ids": []int{int(n["id"].(float64))}}, 200)
	uid := int(u["id"].(float64))
	client := login(t, s, "preview@example.test", "Passw0rd")
	links := s.Store.Ent.Link.Query().AllX(t.Context())
	preview := mustJSON(t, client, "GET", "/api/v1/proxy/preview?target=clash", nil, 200)
	if !strings.Contains(preview["body"].(string), "proxies:") {
		t.Fatal("wrong selected format", preview)
	}
	mustJSON(t, client, "GET", "/api/v1/proxy/preview?user=1&target=clash", nil, 403)
	mustJSON(t, client, "GET", "/api/v1/admin/audit/summary?days=1", nil, 403)
	mustJSON(t, client, "GET", "/api/v1/admin/center/local-gate", nil, 403)
	for _, before := range links {
		after := s.Store.Ent.Link.GetX(t.Context(), before.ID)
		if !equalTime(before.LastUsedAt, after.LastUsedAt) {
			t.Fatal("preview counted as pull", uid)
		}
	}
}

func equalTime(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

func TestDeletedNodeCancelsPendingProbeAndRejectsLateResult(t *testing.T) {
	s, admin, g := setupGate(t)
	created := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "ss2022"}, 200)
	id := int(created["id"].(float64))
	mustJSON(t, admin, "DELETE", "/api/v1/admin/nodes/"+itoa(id), nil, 204)
	for _, key := range []string{pendingNodeProbePrefix + itoa(id), nodeTestKey(id)} {
		if _, ok, err := s.Store.KV.Get(t.Context(), key); err != nil || ok {
			t.Fatal("deleted node retains probe", key, err)
		}
	}
	if err := s.storeNodeTest(t.Context(), id, nodeTestResult{Status: "passed"}); !ent.IsNotFound(err) {
		t.Fatal("late receipt accepted", err)
	}
}

func TestLayaCanVerifySavedConfigurationBeforeEnabling(t *testing.T) {
	s, admin := newInstalledServer(t)
	instance := opsLaya(t, "suspicious")
	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": true, "url": instance.URL, "model": "observer", "key": "fixture-only-key"}, 409)
	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": false, "url": instance.URL, "model": "observer", "key": "fixture-only-key"}, 200)
	if _, ok := s.layaConfig(t.Context()); ok {
		t.Fatal("disabled configuration enabled background judgment")
	}
	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": true, "url": instance.URL, "model": "observer"}, 409)
	mustJSON(t, admin, "POST", "/api/v1/admin/defense/laya/test", nil, 200)
	view := mustJSON(t, admin, "GET", "/api/v1/admin/defense/laya", nil, 200)
	if view["verified"] != true || view["enabled"] != false {
		t.Fatal("verification enabled judgment or was not saved", view)
	}
	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": true, "url": instance.URL, "model": "observer"}, 200)
	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": true, "url": instance.URL, "model": "changed-observer"}, 409)
	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": false, "url": instance.URL, "model": "changed-observer"}, 200)
	view = mustJSON(t, admin, "GET", "/api/v1/admin/defense/laya", nil, 200)
	if view["verified"] != false {
		t.Fatal("old verification applied to changed configuration")
	}
	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": true, "url": instance.URL, "model": "changed-observer"}, 409)
}

func TestLayaUnknownCannotEnableJudgment(t *testing.T) {
	_, admin := newInstalledServer(t)
	instance := opsLaya(t, "unknown")
	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": false, "url": instance.URL, "model": "observer"}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/defense/laya/test", nil, 200)
	view := mustJSON(t, admin, "GET", "/api/v1/admin/defense/laya", nil, 200)
	if view["verified"] != false || view["enabled"] != false {
		t.Fatal("unknown verdict verified configuration", view)
	}
	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": true, "url": instance.URL, "model": "observer"}, 409)
}

// Worker and observation tests supply a verified prerequisite without invoking
// an unrelated inference task. The HTTP verification flow is exercised above.
func saveVerifiedLayaFixture(t *testing.T, s *Server, admin *testClient, values map[string]any) map[string]any {
	t.Helper()
	values["enabled"] = false
	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", values, 200)
	l, _, err := s.readLayaSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.KV.Set(t.Context(), layaVerificationKey(l), "fixture-verified", time.Hour); err != nil {
		t.Fatal(err)
	}
	values["enabled"] = true
	delete(values, "key")
	return mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", values, 200)
}
