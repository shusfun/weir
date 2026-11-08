package center

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/credential"
)

func readyDeployedNode(t *testing.T, preset string) (*Server, *testClient, *ent.Node) {
	t.Helper()
	s, admin, fake := setupGate(t)
	created := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": fake.id, "preset": preset, "skip_probe": true}, 200)
	n := s.Store.Ent.Node.GetX(t.Context(), int(created["id"].(float64)))
	g := s.Store.Ent.Gate.GetX(t.Context(), n.GateID)
	s.Store.Ent.Gate.UpdateOne(g).SetAppliedRevision(g.ConfigRevision).SetLastSeenAt(time.Now()).SetKernelsRunning(map[string]string{n.Kernel: "1.14.4"}).SetListeningPorts([]map[string]any{{"port": n.Port, "proto": nodeProtos(n.Protocol)[0]}}).ExecX(t.Context())
	simCtx, stopSim := context.WithCancel(t.Context())
	doneSim := make(chan struct{})
	go func() {
		defer close(doneSim)
		timer := time.NewTicker(10 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-simCtx.Done():
				return
			case <-timer.C:
				rows, e := s.Store.Ent.Gate.Query().All(simCtx)
				if e != nil {
					continue
				}
				for _, row := range rows {
					if row.AppliedRevision < row.ConfigRevision {
						_ = row.Update().SetAppliedRevision(row.ConfigRevision).Exec(simCtx)
					}
				}
			}
		}
	}()
	t.Cleanup(func() { stopSim(); <-doneSim })
	s.origin.value, s.origin.loaded = "https://center.example.test", true
	return s, admin, n
}

func stubDeployedProbe(t *testing.T, run func(context.Context, nodeprobe.Options) (nodeprobe.Result, error)) {
	t.Helper()
	oldRun, oldBin, oldSupported, oldBatch := deployedNodeProbe, deployedProbeBinary, externalProbeSupported, batchNodeProbe
	externalProbeSupported = func() bool { return true }
	batchNodeProbe = func(ctx context.Context, o nodeprobe.BatchOptions) (map[string]nodeprobe.Check, error) {
		results := map[string]nodeprobe.Check{}
		for _, p := range o.Proxies {
			results[p["name"].(string)] = nodeprobe.Check{Status: "passed"}
		}
		return results, nil
	}
	deployedNodeProbe = run
	deployedProbeBinary = func(*Server, context.Context) (string, kernelconf.Driver, error) {
		return "test-mihomo", kernelconf.Driver{Run: []string{"-f", "{config}"}, Check: []string{"-t", "-f", "{config}"}}, nil
	}
	t.Cleanup(func() {
		deployedNodeProbe, deployedProbeBinary, externalProbeSupported, batchNodeProbe = oldRun, oldBin, oldSupported, oldBatch
	})
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

func TestInternalProbeCredentialLifecycle(t *testing.T) {
	s, _, n := readyDeployedNode(t, "ss2022")
	ctx := t.Context()
	s.Store.Ent.User.Update().SetQuota(1).SetUsed(1).ExecX(ctx)
	s.Store.Ent.Link.Delete().ExecX(ctx)
	c, revoke, err := s.issueProbeCredential(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	row := s.Store.Ent.Credential.GetX(ctx, c.ID)
	if !row.Internal || row.UserID != nil || row.LinkID != nil || row.Purpose != "probe" || row.ExpiresAt == nil || row.Password == c.Password {
		t.Fatal("内部凭据必须独立且加密")
	}
	if err = s.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.Store.Ent.Credential.Query().Where(credential.IDEQ(c.ID)).ExistX(ctx) {
		t.Fatal("同步误删测试凭据")
	}
	g := s.Store.Ent.Gate.GetX(ctx, n.GateID)
	binding, err := s.probeFingerprint(ctx, n, g)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.storeNodeTest(ctx, n.ID, nodeTestResult{Status: "passed", Binding: binding}); err != nil {
		t.Fatal(err)
	}
	revoke()
	if s.Store.Ent.Credential.Query().Where(credential.IDEQ(c.ID)).ExistX(ctx) {
		t.Fatal("凭据未回收")
	}
	if got := s.loadNodeTest(ctx, n); got == nil || got.Status != "passed" {
		t.Fatal("凭据回收使结果失效")
	}
	s.Store.Ent.Node.UpdateOne(n).SetPort(n.Port + 1).ExecX(ctx)
	if got := s.loadNodeTest(ctx, s.Store.Ent.Node.GetX(ctx, n.ID)); got == nil || got.Status != "unknown" {
		t.Fatal("业务变化未失效")
	}
}

func TestDeployedNodeTestDedupTraceAndIsolation(t *testing.T) {
	s, admin, n := readyDeployedNode(t, "ss2022")
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
		if !o.UDP || o.Traceparent == "" || len(o.Client.Proxies) != 1 {
			t.Error("incomplete real client request")
		}
		if o.Client.Proxies[0]["server"] != "192.0.2.1" {
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
	s.checkReachability(t.Context(), n.ID)
	s.CheckReachabilityAll(t.Context(), time.Date(2026, 11, 6, 10, 10, 0, 0, time.UTC))
	reachRunning.Lock()
	reachRunning.Unlock()
	if got := s.loadNodeTest(t.Context(), n); got == nil || got.Status != "passed" || calls.Load() != 1 {
		t.Fatal("定时检查覆盖了完整实测结果")
	}
	if s.Store.Ent.Credential.Query().Where(credential.IDEQ(r.CredentialID)).ExistX(t.Context()) {
		t.Fatal("实测后内部凭据未回收")
	}
	s.Store.Ent.Gate.UpdateOneID(n.GateID).AddConfigRevision(1).ExecX(t.Context())
	if got := s.loadNodeTest(t.Context(), n); got == nil || got.Status != "passed" {
		t.Fatal("凭据回收版本变化使结果失效")
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
	if !customTraceRoute("/api/v1/node-test/download") {
		t.Fatal("download requests must retain trace logs")
	}
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
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("首个实测没有启动")
	}
	queued := startNodeTest(t, admin, nodes[2].ID)
	select {
	case <-entered:
		t.Fatal("并发实测绕过内部授权锁，会导致内核反复重载")
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
	deadline := time.Now().Add(3 * time.Second)
	for len(s.probeSlots()) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("execution slots leaked")
		}
		time.Sleep(10 * time.Millisecond)
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
	if r.Status != "unknown" || r.Location != "colocated" || r.External != "unknown" || r.TCP.Status != "unknown" || r.UDP.Status != "unknown" {
		t.Fatalf("partial failure flattened: %+v", r)
	}
	// 配置应用检查不覆盖手动实测结果。
	s.checkAfterApplied(t.Context(), n.ID, n.GateID)
	if got := s.loadNodeTest(t.Context(), n); got == nil || got.Status != "unknown" {
		t.Fatal("application check overwrote full result")
	}
}

func TestDeployedNodeTestCancelDiscardsLateSuccess(t *testing.T) {
	s, admin, n := readyDeployedNode(t, "ss2022")
	entered := make(chan struct{})
	stubDeployedProbe(t, func(ctx context.Context, o nodeprobe.Options) (nodeprobe.Result, error) {
		close(entered)
		<-ctx.Done()
		return nodeprobe.Result{TCP: nodeprobe.Check{Status: "passed"}, UDP: nodeprobe.Check{Status: "passed"}}, nil
	})
	j := startNodeTest(t, admin, n.ID)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("test did not start")
	}
	if err := s.jobs.Cancel(jobViewer{admin: true}, j.ID); err != nil {
		t.Fatal(err)
	}
	r := finishNodeTest(t, s, j)
	if r.Status != "unknown" || r.TCP.Status != "unknown" || r.UDP.Status != "unknown" || r.External != "unknown" {
		t.Fatalf("cancelled task retained late success: %+v", r)
	}
	if job, _ := s.jobs.Get(j.ID); job.Status != jobCancelled {
		t.Fatalf("cancelled task status = %s", job.Status)
	}
}
