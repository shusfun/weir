package center

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/budget"
	"github.com/shusfun/weir/internal/store/ent/credential"
)

func readyDeployedNode(t *testing.T, preset string) (*Server, *testClient, *ent.Node) {
	t.Helper()
	s, admin, fake := setupGate(t)
	created := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": fake.id, "preset": preset, "skip_probe": true}, 200)
	n := s.Store.Ent.Node.GetX(t.Context(), int(created["id"].(float64)))
	g := s.Store.Ent.Gate.GetX(t.Context(), n.GateID)
	s.Store.Ent.Gate.UpdateOne(g).SetAppliedRevision(g.ConfigRevision).SetLastSeenAt(time.Now()).SetKernelsRunning(map[string]string{n.Kernel: "1.14.4"}).SetListeningPorts([]map[string]any{{"port": n.Port, "proto": nodeProtos(n.Protocol)[0]}}).ExecX(t.Context())
	s.origin.value, s.origin.loaded = "https://center.example.test", true
	return s, admin, n
}

func stubDeployedProbe(t *testing.T, run func(context.Context, nodeprobe.Options) (nodeprobe.Result, error)) {
	t.Helper()
	oldRun, oldBin := deployedNodeProbe, deployedProbeBinary
	deployedNodeProbe = run
	deployedProbeBinary = func(*Server, context.Context) (string, kernelconf.Driver, error) {
		return "test-sing-box", kernelconf.Driver{Run: []string{"run", "-c", "{config}"}, Check: []string{"check", "-c", "{config}"}}, nil
	}
	t.Cleanup(func() { deployedNodeProbe, deployedProbeBinary = oldRun, oldBin })
}

func startNodeTest(t *testing.T, admin *testClient, id int) Job {
	t.Helper()
	w := admin.do("POST", "/api/v1/admin/nodes/"+itoa(id)+"/test", map[string]any{}, map[string]string{"traceparent": "00-123456789012345678901234567890ab-1234567890123456-01"})
	if w.Code != 202 {
		t.Fatalf("test = %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Job Job `json:"job"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Job
}

func finishNodeTest(t *testing.T, s *Server, j Job) nodeTestResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	j, ok := s.jobs.Wait(ctx, j.ID)
	if !ok || j.Status == jobRunning {
		t.Fatalf("test did not finish: %+v", j)
	}
	b, _ := json.Marshal(j.Result["body"])
	var body struct {
		Test nodeTestResult `json:"test"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	return body.Test
}

func TestDeployedNodeTestSelectionAndQuota(t *testing.T) {
	s, _, n := readyDeployedNode(t, "ss2022")
	ctx := t.Context()
	g := s.Store.Ent.Gate.GetX(ctx, n.GateID)
	adminCred, err := s.selectNodeTestCredential(ctx, n, g, 1, 0)
	if err != nil || *adminCred.UserID != 1 {
		t.Fatalf("admin preference: %v %v", adminCred, err)
	}
	u := s.Store.Ent.User.Create().SetEmail("probe-user@example.test").SetPasswordHash("unused").SetQuota(1024).SetAllNodes(true).SaveX(ctx)
	l := s.Store.Ent.Link.Create().SetUserID(u.ID).SetToken(randomHex(16)).SaveX(ctx)
	if err := s.SyncUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	c, err := s.selectNodeTestCredential(ctx, n, g, u.ID, 0)
	if err != nil || *c.UserID != u.ID {
		t.Fatalf("user preference: %v %v", c, err)
	}
	s.Store.Ent.Link.UpdateOne(l).SetEndsAt(time.Now().Add(-time.Minute)).ExecX(ctx)
	if _, err := s.selectNodeTestCredential(ctx, n, g, u.ID, c.ID); err == nil {
		t.Fatal("expired link accepted")
	}
	s.Store.Ent.Link.UpdateOne(l).ClearEndsAt().SetStartsAt(time.Now().Add(time.Hour)).ExecX(ctx)
	if _, err := s.selectNodeTestCredential(ctx, n, g, u.ID, c.ID); err == nil {
		t.Fatal("future link accepted")
	}
	s.Store.Ent.Link.UpdateOne(l).ClearStartsAt().ExecX(ctx)
	s.Store.Ent.Budget.Update().Where(budget.UserIDEQ(u.ID)).SetUsed(1024).SetAllocated(1024).ExecX(ctx)
	if _, err := s.selectNodeTestCredential(ctx, n, g, u.ID, c.ID); err == nil {
		t.Fatal("exhausted budget accepted")
	}
	selected, err := s.selectNodeTestCredential(ctx, n, g, u.ID, 0)
	if err != nil || selected.ID != adminCred.ID {
		t.Fatal("did not fall back to eligible normal credential")
	}
	s.Store.Ent.Credential.Create().SetNodeID(n.ID).SetInternal(true).SetStatsKey("internal-probe-test").SetPassword("not-a-user").SaveX(ctx)
	s.Store.Ent.User.UpdateOneID(1).SetEnabled(false).ExecX(ctx)
	if _, err := s.selectNodeTestCredential(ctx, n, g, 1, 0); err == nil {
		t.Fatal("internal credential or disabled user selected")
	}
}

func TestWireGuardProbeCredentialLifecycle(t *testing.T) {
	s, _, n := readyDeployedNode(t, "wireguard")
	ctx := t.Context()
	g := s.Store.Ent.Gate.GetX(ctx, n.GateID)
	parent, err := s.selectNodeTestCredential(ctx, n, g, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := s.wireGuardProbeCredential(ctx, parent)
	if err != nil {
		t.Fatal(err)
	}
	if probe.ID == parent.ID || probe.Password == parent.Password || !sameCredentialOwner(probe, parent) {
		t.Fatal("probe must use a distinct authorized peer")
	}
	again, err := s.wireGuardProbeCredential(ctx, parent)
	if err != nil || again.ID != probe.ID {
		t.Fatal("probe peer was recreated")
	}
	if err := s.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.Store.Ent.Credential.Query().Where(credential.IDEQ(probe.ID)).ExistX(ctx) {
		t.Fatal("normal reconciliation deleted active probe")
	}
	u := s.Store.Ent.User.GetX(ctx, *parent.UserID)
	l := s.Store.Ent.Link.GetX(ctx, *parent.LinkID)
	entries, err := s.selectSubscriptionEntries(ctx, u, l)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.credential.ID == probe.ID {
			t.Fatal("probe leaked into subscription")
		}
	}
	view := s.nodeView(ctx, n)
	if view["credentials"] != 1 {
		t.Fatalf("probe counted as user access: %v", view["credentials"])
	}
	s.Store.Ent.Link.UpdateOne(l).SetEndsAt(time.Now().Add(-time.Minute)).ExecX(ctx)
	if err := s.SyncUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if s.Store.Ent.Credential.Query().Where(credential.IDEQ(probe.ID)).ExistX(ctx) {
		t.Fatal("expired link kept probe")
	}
	s.Store.Ent.Link.UpdateOne(l).ClearEndsAt().ExecX(ctx)
	probe, err = s.wireGuardProbeCredential(ctx, parent)
	if err != nil { t.Fatal(err) }
	s.Store.Ent.Link.UpdateOne(l).SetEnabled(false).ExecX(ctx)
	if err := s.SyncUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if s.Store.Ent.Credential.Query().Where(credential.IDEQ(probe.ID)).ExistX(ctx) {
		t.Fatal("disabled link kept probe")
	}
}

func TestDeployedNodeTestDedupTraceAndIsolation(t *testing.T) {
	s, admin, n := readyDeployedNode(t, "hysteria2")
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	stubDeployedProbe(t, func(ctx context.Context, o nodeprobe.Options) (nodeprobe.Result, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return nodeprobe.Result{}, ctx.Err()
		}
		if !o.UDP || o.Traceparent == "" || len(o.Client.Outbounds) != 1 {
			t.Error("incomplete real client request")
		}
		if o.Client.Outbounds[0]["server"] != "192.0.2.1" {
			t.Error("deployed endpoint replaced with loopback")
		}
		return nodeprobe.Result{TCP: nodeprobe.Check{Status: "passed", Bytes: nodeprobe.PayloadSize}, UDP: nodeprobe.Check{Status: "passed"}}, nil
	})
	j := startNodeTest(t, admin, n.ID)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("probe not started")
	}
	duplicate := startNodeTest(t, admin, n.ID)
	if duplicate.ID != j.ID {
		t.Fatal("duplicate test started")
	}
	if got := s.loadNodeTest(t.Context(), n); got == nil || got.Status != "running" {
		t.Fatal("running state missing from node")
	}
	close(release)
	r := finishNodeTest(t, s, j)
	if r.Status != "passed" || calls.Load() != 1 || r.TraceID != "123456789012345678901234567890ab" {
		t.Fatalf("result: %+v", r)
	}
	row := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("node.test")).OnlyX(t.Context())
	if row.Detail["trace_id"] != r.TraceID || row.Detail["job_id"] != j.ID {
		t.Fatal("audit trace missing")
	}
	if raw, ok, _ := s.Store.KV.Get(t.Context(), nodeTestKey(n.ID)); !ok || strings.Contains(raw, "password") {
		t.Fatal("invalid stored result")
	}
	oldDial := reachDial
	reachDial = func(context.Context, string) (net.Conn, error) { a, b := net.Pipe(); b.Close(); return a, nil }
	t.Cleanup(func() { reachDial = oldDial })
	s.checkReachability(t.Context(), n.ID)
	if got := s.loadNodeTest(t.Context(), n); got == nil || got.Status != "passed" || calls.Load() != 1 {
		t.Fatal("lightweight check changed full result or ran client")
	}
	s.Store.Ent.Gate.UpdateOneID(n.GateID).AddConfigRevision(1).ExecX(t.Context())
	if got := s.loadNodeTest(t.Context(), n); got == nil || got.Status != "unknown" {
		t.Fatal("stale result remained successful")
	}
	if w := admin.do("POST", "/api/v1/admin/nodes/999999/test", map[string]any{}, nil); w.Code != 404 {
		t.Fatal("missing node accepted")
	}
	s.Store.Ent.Node.UpdateOne(n).SetEnabled(false).ExecX(t.Context())
	if w := admin.do("POST", "/api/v1/admin/nodes/"+itoa(n.ID)+"/test", map[string]any{}, nil); w.Code != 422 {
		t.Fatal("disabled node accepted")
	}
}

func TestDeployedNodeTestUnknownAndDownloadGrant(t *testing.T) {
	s, admin, n := readyDeployedNode(t, "http-local")
	stubDeployedProbe(t, func(context.Context, nodeprobe.Options) (nodeprobe.Result, error) {
		t.Fatal("missing origin must not run a client")
		return nodeprobe.Result{}, nil
	})
	s.origin.value = ""
	r := finishNodeTest(t, s, startNodeTest(t, admin, n.ID))
	if r.Status != "unknown" || !strings.Contains(r.Why, "HTTPS") {
		t.Fatalf("missing origin: %+v", r)
	}
	token, revoke := s.probeGrant()
	defer revoke()
	w := admin.do("GET", "/api/v1/node-test/download", nil, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != 200 || w.Body.Len() != nodeprobe.PayloadSize {
		t.Fatal("grant download invalid")
	}
	if w := admin.do("GET", "/api/v1/node-test/download", nil, map[string]string{"Authorization": "Bearer " + token}); w.Code != 404 {
		t.Fatal("replayed grant accepted")
	}
	s.nodeProbes.Lock()
	s.nodeProbes.targets["expired"] = time.Now().Add(-time.Second)
	s.nodeProbes.Unlock()
	if w := admin.do("GET", "/api/v1/node-test/download", nil, map[string]string{"Authorization": "Bearer expired"}); w.Code != 404 {
		t.Fatal("expired grant accepted")
	}
}

func TestDeployedNodeTestQueueCancelAndChangedConfiguration(t *testing.T) {
	s, admin, n := readyDeployedNode(t, "ss2022")
	ctx := t.Context()
	nodes := []*ent.Node{n}
	for range 2 {
		v := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": n.GateID, "preset": "ss2022", "skip_probe": true}, 200)
		nodes = append(nodes, s.Store.Ent.Node.GetX(ctx, int(v["id"].(float64))))
	}
	g := s.Store.Ent.Gate.GetX(ctx, n.GateID)
	ports := []map[string]any{}
	for _, n := range nodes {
		ports = append(ports, map[string]any{"port": n.Port, "proto": "tcp"})
	}
	s.Store.Ent.Gate.UpdateOne(g).SetAppliedRevision(g.ConfigRevision).SetListeningPorts(ports).ExecX(ctx)
	entered, release := make(chan struct{}, 3), make(chan struct{})
	stubDeployedProbe(t, func(ctx context.Context, o nodeprobe.Options) (nodeprobe.Result, error) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nodeprobe.Result{}, ctx.Err()
		}
		return nodeprobe.Result{TCP: nodeprobe.Check{Status: "passed"}, UDP: nodeprobe.Check{Status: "passed"}}, nil
	})
	jobs := []Job{startNodeTest(t, admin, nodes[0].ID), startNodeTest(t, admin, nodes[1].ID)}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("two execution slots did not start")
		}
	}
	queued := startNodeTest(t, admin, nodes[2].ID)
	select {
	case <-entered:
		t.Fatal("third client bypassed concurrency limit")
	case <-time.After(100 * time.Millisecond):
	}
	if got := s.loadNodeTest(ctx, nodes[2]); got == nil || got.Status != "running" {
		t.Fatal("queued job not discoverable")
	}
	if err := s.jobs.Cancel(jobViewer{admin: true}, queued.ID); err != nil {
		t.Fatal(err)
	}
	if r := finishNodeTest(t, s, queued); r.Status != "unknown" || r.TCP.Status != "unknown" || r.External != "unknown" {
		t.Fatalf("cancel retained positive result: %+v", r)
	}
	s.Store.Ent.Node.UpdateOne(nodes[0]).SetPort(nodes[0].Port + 200).ExecX(ctx)
	close(release)
	for i, j := range jobs {
		r := finishNodeTest(t, s, j)
		if i == 0 && (r.Status != "unknown" || r.TCP.Status != "unknown") {
			t.Fatalf("changed endpoint retained result: %+v", r)
		}
		if i == 1 && r.Status != "passed" {
			t.Fatalf("unrelated node result invalidated: %+v", r)
		}
	}
	if len(s.probeSlots()) != 0 {
		t.Fatal("execution slots leaked")
	}
}

func TestDeployedNodeTestFailureAndAuthorization(t *testing.T) {
	s, admin, n := readyDeployedNode(t, "ss2022")
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "node-test-user@example.test", "password": "UserTest123", "all_nodes": true}, 200)
	user := login(t, s, "node-test-user@example.test", "UserTest123")
	if w := user.do("POST", "/api/v1/admin/nodes/"+itoa(n.ID)+"/test", map[string]any{}, nil); w.Code != 403 {
		t.Fatalf("ordinary user accepted: %d %s", w.Code, w.Body.String())
	}
	g := s.Store.Ent.Gate.GetX(t.Context(), n.GateID)
	s.Store.Ent.Gate.UpdateOne(g).SetAppliedRevision(g.ConfigRevision).SetCenterURL("http://127.0.0.1:19810").ExecX(t.Context())
	stubDeployedProbe(t, func(context.Context, nodeprobe.Options) (nodeprobe.Result, error) {
		return nodeprobe.Result{TCP: nodeprobe.Check{Status: "passed"}, UDP: nodeprobe.Check{Status: "failed", Why: "UDP 请求超时"}}, nil
	})
	r := finishNodeTest(t, s, startNodeTest(t, admin, n.ID))
	if r.Status != "failed" || r.Location != "colocated" || r.External != "unknown" || r.TCP.Status != "passed" || r.UDP.Status != "failed" {
		t.Fatalf("partial failure flattened: %+v", r)
	}
	// Configuration-application checks retain the lightweight path too.
	s.checkAfterApplied(t.Context(), n.ID, n.GateID)
	if got := s.loadNodeTest(t.Context(), n); got == nil || got.Status != "failed" {
		t.Fatal("application check overwrote full result")
	}
}
