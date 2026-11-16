package center

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/packages/contracts"
)

type fakeGate struct {
	t            *testing.T
	c            *testClient
	id           int64
	priv         ed25519.PrivateKey
	bearer       string
	mu           sync.Mutex
	tasks        map[int]contracts.Task
	executionSeq int64
}

// pairFakeGate 走真实流程：管理员生成邀请 → Gate 用一次性凭证配对 → 签名挑战换取访问令牌。
func pairFakeGate(t *testing.T, admin *testClient) *fakeGate {
	t.Helper()
	inv := decode(t, admin.do("POST", "/api/v1/admin/gates/invite", map[string]string{"name": "HK-1"}, nil))
	token, _ := inv["token"].(string)
	if token == "" || !strings.Contains(inv["command"].(string), "/install/gate.sh") {
		t.Fatalf("invite = %v", inv)
	}
	return pairWithToken(t, admin, token)
}

// pairWithToken 用已生成的一次性凭证配对一台假 Gate 并完成设备认证。
func pairWithToken(t *testing.T, admin *testClient, token string) *fakeGate {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	g := &fakeGate{t: t, c: &testClient{t: t, router: admin.router}, priv: priv}
	pair := contracts.PairRequest{Name: "ignored", PublicKey: base64.StdEncoding.EncodeToString(pub), Token: token}
	w := g.c.do("POST", "/api/v1/agent/pair", pair, nil)
	if w.Code != 201 {
		t.Fatalf("pair = %d %s", w.Code, w.Body.String())
	}
	var res contracts.PairResult
	json.Unmarshal(w.Body.Bytes(), &res)
	g.id = res.GateID
	if w = g.c.do("POST", "/api/v1/agent/pair", pair, nil); w.Code != 403 {
		t.Fatalf("pair token reused = %d", w.Code)
	}
	g.auth()
	tasks := map[string]int{}
	for _, kind := range []string{contracts.TaskCoreInstall, contracts.ActionAssetFetch, contracts.ActionSourceProbe, contracts.ActionNodeTest, contracts.ActionOutboundProbe, contracts.ActionChainProbe, contracts.ActionRealityProbe, contracts.ActionPanelConfigure, contracts.ActionPanelCert, contracts.ActionPanelDiscover, contracts.ActionPanelRemove, contracts.ActionACMEHTTP01, contracts.ActionDiagnostics, contracts.ActionCoreRestart, contracts.ActionHostRepair, contracts.ActionLiveStart, contracts.ActionReleaseReplace, contracts.ActionUninstall} {
		tasks[kind] = contracts.TaskVersion
	}
	caps := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: tasks}
	code, raw := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: contracts.AgentProtocol, ConfigRevision: -1, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1}})
	if code != 200 {
		t.Fatalf("capability registration: %d %s", code, raw)
	}
	return g
}

func (g *fakeGate) auth() {
	ch := decode(g.t, g.c.do("POST", "/api/v1/agent/challenge", map[string]int64{"gate_id": g.id}, nil))["challenge"].(string)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(g.priv, []byte(ch)))
	res := decode(g.t, g.c.do("POST", "/api/v1/agent/auth", map[string]any{"gate_id": g.id, "challenge": ch, "signature": sig}, nil))
	g.bearer, _ = res["token"].(string)
	if g.bearer == "" {
		g.t.Fatalf("auth = %v", res)
	}
}

func (g *fakeGate) call(method, path string, body any) (int, []byte) {
	w := g.c.do(method, path, body, g.requestHeaders(path))
	return w.Code, w.Body.Bytes()
}

func (g *fakeGate) requestHeaders(path string) map[string]string {
	headers := map[string]string{"Authorization": "Bearer " + g.bearer}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, task := range g.tasks {
		var request contracts.AssetFetch
		if task.Kind == contracts.ActionAssetFetch && json.Unmarshal(task.Payload, &request) == nil && strings.HasPrefix(path, "/api/v1/agent/uploads/"+request.UploadID) {
			headers["X-Weir-Task-ID"] = task.ID
			headers["X-Weir-Task-Attempt"] = strconv.Itoa(task.Attempt)
			headers["X-Weir-Task-Lease"] = task.LeaseToken
		}
	}
	return headers
}

func (g *fakeGate) config(since int64) contracts.GateConfig {
	g.t.Helper()
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: contracts.AgentProtocol, ConfigRevision: since, ExecutionSeq: g.executionSeq, Resources: contracts.WorkerResources{Concurrency: 1}})
	if code != 200 {
		g.t.Fatalf("config = %d %s", code, body)
	}
	var reply contracts.AgentPollReply
	if err := json.Unmarshal(body, &reply); err != nil || reply.Config == nil {
		g.t.Fatalf("missing configuration: %s", body)
	}
	cfg := *reply.Config
	g.executionSeq = cfg.ExecutionSeq
	return cfg
}

func TestGatePairConfigMeterAndRevoke(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := context.Background()
	if _, err := s.kernelCache().Put("xray", "26.3.27", "amd64", []byte("xray-binary")); err != nil {
		t.Fatal(err)
	}
	g := pairFakeGate(t, admin)
	if code, _ := g.sendHeartbeat(contracts.Heartbeat{NodeTestVersion: 3, Version: "test", Hostname: "hk", Arch: "amd64", ListeningPorts: []contracts.ListeningPort{{Port: 54321, Proto: "tcp", Process: "x-ui"}}}); code != 200 {
		t.Fatalf("heartbeat = %d", code)
	}
	gate, _ := s.Store.Ent.Gate.Get(ctx, int(g.id))
	if gate.Name != "HK-1" || gate.PublicHost != "192.0.2.1" || gate.XrayStatsPort == 0 || gate.XrayStatsPort == 54321 {
		t.Fatalf("gate after heartbeat = %+v", gate)
	}
	// 节点端口避开 Gate 上报被其他程序占用的端口。
	port, err := s.AllocateNodePort(ctx, gate)
	if err != nil || port == 54321 || port < portMin || port > portMax {
		t.Fatalf("node port = %d %v", port, err)
	}
	n := s.Store.Ent.Node.Create().SetGateID(gate.ID).SetExitID(s.Store.Ent.Exit.Query().Where(entexit.GateIDEQ(gate.ID), entexit.TagEQ("direct")).OnlyX(t.Context()).ID).SetName("香港").SetKernel("xray").SetProtocol("vless").SetPort(port).SetNetwork("tcp").SetSecurity("reality").
		SetSettings(map[string]any{"reality_dest": "www.microsoft.com:443", "reality_server_names": "www.microsoft.com", "reality_private_key": "kKB1Vv7m0wN3hOq1oQv0b9nN5mF3V6Q2l1c8pZ0yGXE", "reality_short_id": "a1b2c3d4"}).SaveX(ctx)
	u := s.Store.Ent.User.Create().SetEmail("u1@example.test").SetPasswordHash("x").SetQuota(10 << 30).SaveX(ctx)
	off := s.Store.Ent.User.Create().SetEmail("off@example.test").SetPasswordHash("x").SetEnabled(false).SaveX(ctx)
	link := s.Store.Ent.Link.Create().SetUserID(u.ID).SetToken(randomToken(16)).SaveX(ctx)
	cred := s.Store.Ent.Credential.Create().SetNodeID(n.ID).SetLinkID(link.ID).SetUserID(u.ID).SetStatsKey("c-u1").SetUUID("11111111-1111-4111-8111-111111111111").SaveX(ctx)
	s.Store.Ent.Credential.Create().SetNodeID(n.ID).SetUserID(off.ID).SetStatsKey("c-off").SetUUID("22222222-2222-4222-8222-222222222222").SaveX(ctx)
	if _, err = s.rebalanceUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	cfg := g.config(0)
	active := 0
	for _, c := range cfg.Credentials {
		if c.Revoke != "" || c.Internal {
			continue
		}
		active++
		if c.StatsKey != "c-u1" {
			t.Fatalf("active credential = %+v", c)
		}
	}
	if len(cfg.Inbounds) != 1 || active != 1 {
		t.Fatalf("config inbounds=%d credentials=%+v (disabled user must stay revoked)", len(cfg.Inbounds), cfg.Credentials)
	}
	if b := cfg.Budgets[userKey(u.ID)]; b.Allocated != 10<<30 || b.Unlimited {
		t.Fatalf("single-gate user must get the whole remaining quota: %+v", b)
	}
	if cfg.Kernels["xray"].Version != "26.3.27" || !strings.Contains(cfg.Drivers["xray"], "kernel: xray") {
		t.Fatalf("kernels=%v", cfg.Kernels)
	}
	// 长轮询：版本未变时挂起，管理员改动后立即返回。
	done := make(chan contracts.GateConfig, 1)
	go func() { done <- g.config(cfg.Revision) }()
	time.Sleep(200 * time.Millisecond)
	if w := admin.do("PATCH", "/api/v1/admin/gates/"+itoa(int(g.id)), map[string]string{"name": "HK-01"}, nil); w.Code != 200 {
		t.Fatalf("update gate = %d", w.Code)
	}
	select {
	case next := <-done:
		if next.Revision <= cfg.Revision {
			t.Fatalf("revision did not advance: %d", next.Revision)
		}
		cfg = next
	case <-time.After(5 * time.Second):
		t.Fatal("long poll was not woken by the change")
	}
	if code, _ := g.call("POST", "/api/v1/agent/state", contracts.GateState{AppliedRevision: cfg.Revision}); code != 200 {
		t.Fatal("state")
	}
	// 记账：同纪元重复上报不重复扣减；纪元变化从零起算。
	report := contracts.MeterReport{Protocol: 2, Epoch: "e1", Seq: 1, Usage: []contracts.MeterSample{{StatsKey: "c-u1", Up: 300, Down: 700}}}
	for i := 0; i < 2; i++ {
		if code, _ := g.call("POST", "/api/v1/agent/meter", report); code != 200 {
			t.Fatal("meter")
		}
	}
	report.Seq, report.Usage[0].Up = 2, 800
	g.call("POST", "/api/v1/agent/meter", report)
	report.Epoch, report.Seq, report.Usage[0].Up, report.Usage[0].Down = "e2", 1, 100, 0
	g.call("POST", "/api/v1/agent/meter", report)
	if got := s.Store.Ent.User.GetX(ctx, u.ID).Used; got != 1000+500+100 {
		t.Fatalf("user used = %d, want 1600", got)
	}
	if got := s.Store.Ent.Link.GetX(ctx, link.ID).Used; got != 1600 {
		t.Fatalf("link used = %d", got)
	}
	if c := s.Store.Ent.Credential.GetX(ctx, cred.ID); c.MeterEpoch != "e2" || c.MeterUp != 100 {
		t.Fatalf("credential meter = %+v", c)
	}
	// A late packet from a retired epoch must not switch the baseline back.
	old := report
	old.Epoch, old.Seq = "e1", 99
	old.Usage = []contracts.MeterSample{{StatsKey: "c-u1", Up: 900, Down: 700}}
	code, body := g.call("POST", "/api/v1/agent/meter", old)
	var ack contracts.MeterAck
	_ = json.Unmarshal(body, &ack)
	if code != 200 || ack.Accepted || s.Store.Ent.User.GetX(ctx, u.ID).Used != 1600 {
		t.Fatalf("old epoch counted again: %d %s", code, body)
	}
	partial := contracts.MeterReport{Protocol: 2, Partial: true, Epoch: "e2", Seq: 2, Status: "fresh"}
	if code, body = g.call("POST", "/api/v1/agent/meter", partial); code != 200 {
		t.Fatalf("partial: %d %s", code, body)
	}
	if c := s.Store.Ent.Credential.GetX(ctx, cred.ID); c.MeterEpoch != "e2" || c.MeterUp != 100 {
		t.Fatalf("absent credential reset: %+v", c)
	}
	partial.Epoch = "e3"
	if code, _ = g.call("POST", "/api/v1/agent/meter", partial); code != 409 {
		t.Fatalf("new epoch accepted incomplete baseline: %d", code)
	}
	// Gate 报告端口被占用：中心给节点换端口并提高配置版本。
	before := s.Store.Ent.Gate.GetX(ctx, gate.ID).ConfigRevision
	g.call("POST", "/api/v1/agent/state", contracts.GateState{AppliedRevision: cfg.Revision, ConflictPort: port})
	if moved := s.Store.Ent.Node.GetX(ctx, n.ID).Port; moved == port {
		t.Fatal("conflicting node port not reassigned")
	}
	if after := s.Store.Ent.Gate.GetX(ctx, gate.ID).ConfigRevision; after <= before {
		t.Fatal("revision not bumped after port reassignment")
	}
	// 内核绑定统计端口失败（被出站连接占着）：换统计端口，下发的配置里也是新端口。
	stats := g.config(0).Ports.XrayStats
	g.call("POST", "/api/v1/agent/state", contracts.GateState{FailedRevision: s.Store.Ent.Gate.GetX(ctx, gate.ID).ConfigRevision, Error: "xray 启动后健康检查未通过", ConflictPort: stats})
	if got := g.config(0).Ports.XrayStats; got == stats || got == 0 {
		t.Fatalf("stats port not reassigned: %d → %d", stats, got)
	}
	// 普通用户不能调用管理接口。
	if w := admin.do("POST", "/api/v1/admin/gates/"+itoa(int(g.id))+"/revoke", nil, nil); w.Code != 200 {
		t.Fatalf("revoke = %d", w.Code)
	}
	if code, _ := g.sendHeartbeat(contracts.Heartbeat{}); code != http.StatusForbidden {
		t.Fatalf("revoked gate still accepted: %d", code)
	}
	_ = ent.IsNotFound
}

func TestGateAuthRejectsBadSignatureAndReplay(t *testing.T) {
	_, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	ch := decode(t, g.c.do("POST", "/api/v1/agent/challenge", map[string]int64{"gate_id": g.id}, nil))["challenge"].(string)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	bad := base64.StdEncoding.EncodeToString(ed25519.Sign(other, []byte(ch)))
	if w := g.c.do("POST", "/api/v1/agent/auth", map[string]any{"gate_id": g.id, "challenge": ch, "signature": bad}, nil); w.Code != 401 {
		t.Fatalf("wrong key = %d", w.Code)
	}
	good := base64.StdEncoding.EncodeToString(ed25519.Sign(g.priv, []byte(ch)))
	if w := g.c.do("POST", "/api/v1/agent/auth", map[string]any{"gate_id": g.id, "challenge": ch, "signature": good}, nil); w.Code != 401 {
		t.Fatalf("challenge reused after a failed attempt = %d", w.Code)
	}
	if code, _ := g.sendHeartbeat(contracts.Heartbeat{Arch: "amd64"}); code != 200 {
		t.Fatalf("valid token rejected: %d", code)
	}
	g.bearer = "forged"
	if code, _ := g.sendHeartbeat(contracts.Heartbeat{}); code != 401 {
		t.Fatalf("forged token = %d", code)
	}
}

func (g *fakeGate) sendHeartbeat(h contracts.Heartbeat) (int, []byte) {
	if h.Version == "" {
		h.Version = "test"
	}
	if h.Arch == "" {
		h.Arch = "amd64"
	}
	h.Protocol, h.Full = contracts.HeartbeatProtocol, true
	h.Digest, _ = contracts.HeartbeatDigest(h)
	return g.call("POST", "/api/v1/agent/heartbeat/v2", h)
}

func (g *fakeGate) pollActions() (int, []byte) {
	code, raw := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: contracts.AgentProtocol, ConfigRevision: -1, Resources: contracts.WorkerResources{Concurrency: 1}})
	if code != 200 {
		return code, raw
	}
	var reply contracts.AgentPollReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		g.t.Error(err)
		return 500, nil
	}
	actions := []contracts.Action{}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.tasks == nil {
		g.tasks = map[int]contracts.Task{}
	}
	for _, task := range reply.Tasks {
		id, err := strconv.Atoi(strings.TrimPrefix(task.IdempotencyKey, "legacy:"))
		if err != nil {
			g.t.Errorf("operation key: %s", task.IdempotencyKey)
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			g.t.Error(err)
			continue
		}
		g.tasks[id] = task
		actions = append(actions, contracts.Action{ID: int64(id), Kind: task.Kind, Payload: payload, ExpiresAt: task.Deadline})
	}
	body, _ := json.Marshal(actions)
	return code, body
}

func (g *fakeGate) finishTask(id int, result contracts.ActionResult) (int, []byte) {
	g.mu.Lock()
	task, ok := g.tasks[id]
	g.mu.Unlock()
	if !ok {
		g.t.Errorf("unleased operation: %d", id)
		return 409, nil
	}
	if result.Result == nil {
		result.Result = map[string]any{}
	}
	body, _ := json.Marshal(result.Result)
	update := contracts.TaskUpdate{Attempt: task.Attempt, LeaseToken: task.LeaseToken, Result: body}
	if !result.OK {
		code := result.Error
		if (&contracts.TaskError{Code: code, Origin: "fixture"}).Validate() != nil {
			code = "action_failed"
		}
		update.Error = &contracts.TaskError{Code: code, Origin: "fixture"}
		update.Result = nil
	}
	code, raw := g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", update)
	return code, raw
}
