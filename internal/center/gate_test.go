package center

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/packages/contracts"
)

type fakeGate struct {
	t      *testing.T
	c      *testClient
	id     int64
	priv   ed25519.PrivateKey
	bearer string
}

// pairFakeGate 走真实流程：管理员生成邀请 → Gate 用一次性凭证配对 → 签名挑战换取访问令牌。
func pairFakeGate(t *testing.T, admin *testClient) *fakeGate {
	t.Helper()
	inv := decode(t, admin.do("POST", "/api/v1/admin/gates/invite", map[string]string{"name": "HK-1"}, nil))
	token, _ := inv["token"].(string)
	if token == "" || !strings.Contains(inv["command"].(string), "/install/gate.sh") {
		t.Fatalf("invite = %v", inv)
	}
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
	w := g.c.do(method, path, body, map[string]string{"Authorization": "Bearer " + g.bearer})
	return w.Code, w.Body.Bytes()
}

func (g *fakeGate) config(since int64) contracts.GateConfig {
	g.t.Helper()
	code, body := g.call("GET", "/api/v1/agent/config?since="+itoa(int(since)), nil)
	if code != 200 {
		g.t.Fatalf("config = %d %s", code, body)
	}
	var cfg contracts.GateConfig
	json.Unmarshal(body, &cfg)
	return cfg
}

func TestGatePairConfigMeterAndRevoke(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := context.Background()
	if _, err := s.kernelCache().Put("xray", "26.3.27", "amd64", []byte("xray-binary")); err != nil {
		t.Fatal(err)
	}
	g := pairFakeGate(t, admin)
	if code, _ := g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Version: "test", Hostname: "hk", Arch: "amd64", ListeningPorts: []contracts.ListeningPort{{Port: 54321, Proto: "tcp", Process: "x-ui"}}}); code != 200 {
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
	n := s.Store.Ent.Node.Create().SetGateID(gate.ID).SetName("香港").SetKernel("xray").SetProtocol("vless").SetPort(port).SetNetwork("tcp").SetSecurity("reality").
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
	if len(cfg.Inbounds) != 1 || len(cfg.Credentials) != 1 || cfg.Credentials[0].StatsKey != "c-u1" {
		t.Fatalf("config inbounds=%d credentials=%+v (disabled user must be excluded)", len(cfg.Inbounds), cfg.Credentials)
	}
	if b := cfg.Budgets[userKey(u.ID)]; b.Allocated != 10<<30 || b.Unlimited {
		t.Fatalf("single-gate user must get the whole remaining quota: %+v", b)
	}
	if cfg.Kernels["xray"].Version != "26.3.27" || !strings.Contains(cfg.Profiles["xray"], "kernel: xray") {
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
	report := contracts.MeterReport{Epoch: "e1", Seq: 1, Usage: []contracts.MeterSample{{StatsKey: "c-u1", Up: 300, Down: 700}}}
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
	// Gate 报告端口被占用：中心给节点换端口并提高配置版本。
	before := s.Store.Ent.Gate.GetX(ctx, gate.ID).ConfigRevision
	g.call("POST", "/api/v1/agent/state", contracts.GateState{AppliedRevision: cfg.Revision, ConflictPort: port})
	if moved := s.Store.Ent.Node.GetX(ctx, n.ID).Port; moved == port {
		t.Fatal("conflicting node port not reassigned")
	}
	if after := s.Store.Ent.Gate.GetX(ctx, gate.ID).ConfigRevision; after <= before {
		t.Fatal("revision not bumped after port reassignment")
	}
	// 普通用户不能调用管理接口。
	if w := admin.do("POST", "/api/v1/admin/gates/"+itoa(int(g.id))+"/revoke", nil, nil); w.Code != 200 {
		t.Fatalf("revoke = %d", w.Code)
	}
	if code, _ := g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{}); code != http.StatusUnauthorized {
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
	if code, _ := g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Arch: "amd64"}); code != 200 {
		t.Fatalf("valid token rejected: %d", code)
	}
	g.bearer = "forged"
	if code, _ := g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{}); code != 401 {
		t.Fatalf("forged token = %d", code)
	}
}
