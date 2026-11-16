package center

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

func TestDomainObservationReplayEpochPrivacyAndAuthorization(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	gid := int(g.id)
	exit, err := s.directExit(ctx, s.Store.Ent, gid)
	if err != nil {
		t.Fatal(err)
	}
	n := s.Store.Ent.Node.Create().SetName("observed").SetGateID(gid).SetExitID(exit.ID).SetKernel("sing-box").SetProtocol("socks").SetPort(31003).SaveX(ctx)
	u := s.Store.Ent.User.Create().SetEmail("domains@example.test").SetPasswordHash("unused").SetUnlimited(true).SaveX(ctx)
	now := time.Now().UTC()
	people := 2
	rep := contracts.MeterReport{Protocol: 2, Epoch: "domain-a", Seq: 1, Status: "fresh", Domains: []contracts.DomainObservation{{Engine: "sing-box", Day: now.Format("2006-01-02"), Domain: "domestic.example", NodeID: int64(n.ID), Bytes: 100, Users: &people, ObservedAt: now}}, DomainCoverage: []contracts.DomainCoverage{{Engine: "sing-box", Status: "partial", Reason: "connection_snapshots", ObservedAt: now}}}
	for range 2 {
		if code, b := g.call("POST", "/api/v1/agent/meter", rep); code != 200 {
			t.Fatalf("report: %d %s", code, b)
		}
	}
	root := "/api/v1/admin/domain-candidates"
	check := func(want float64) {
		t.Helper()
		v := mustJSON(t, admin, "GET", root, nil, 200)
		items := v["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["bytes"] != want {
			t.Fatal(v)
		}
	}
	check(100)
	rep.Seq++
	rep.Domains[0].Bytes = 150
	if code, b := g.call("POST", "/api/v1/agent/meter", rep); code != 200 {
		t.Fatalf("delta: %d %s", code, b)
	}
	check(150)
	rep.Seq++
	rep.Domains[0].Bytes = 1
	if code, _ := g.call("POST", "/api/v1/agent/meter", rep); code != 500 {
		t.Fatal("counter rollback acknowledged", code)
	}
	if s.Store.Ent.Gate.GetX(ctx, gid).MeterSeq != 2 {
		t.Fatal("bad report sequence committed")
	}
	check(150)
	rep.Epoch = "domain-b"
	rep.Seq = 1
	rep.Domains[0].Bytes = 20
	if code, b := g.call("POST", "/api/v1/agent/meter", rep); code != 200 {
		t.Fatalf("epoch: %d %s", code, b)
	}
	check(170)
	other := pairFakeGate(t, admin)
	rep.Epoch = "foreign"
	rep.Domains[0].Bytes = 10000
	if code, b := other.call("POST", "/api/v1/agent/meter", rep); code != 200 {
		t.Fatalf("foreign: %d %s", code, b)
	}
	check(170)
	if s.Store.Ent.User.GetX(ctx, u.ID).Used != 0 {
		t.Fatal("domain optimization billed user")
	}
	mustJSON(t, admin, "POST", root+"/domestic.example/ignore", map[string]any{"ignored": true}, 200)
	view := mustJSON(t, admin, "GET", root+"?domain=domestic.example&gate="+itoa(gid), nil, 200)
	if view["items"].([]any)[0].(map[string]any)["ignored"] != true || len(view["coverage"].([]any)) != 1 {
		t.Fatal("ignore or coverage filter", view)
	}
	mustJSON(t, admin, "POST", root+"/domestic.example/direct", map[string]any{"user_id": u.ID, "revision": 0}, 400)
	owner := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "domain-owner@example.test", "password": "DomainOwner2026"}, 200)
	_ = owner
	user := login(t, s, "domain-owner@example.test", "DomainOwner2026")
	mustJSON(t, user, "GET", root, nil, 403)
	mustJSON(t, user, "POST", root+"/domestic.example/ignore", map[string]any{"ignored": false}, 403)
}

func TestDomainObserverStableSealedPorts(t *testing.T) {
	s, _, fake := setupGate(t)
	ctx := t.Context()
	g := s.Store.Ent.Gate.GetX(ctx, int(fake.id))
	makeConfig := func() contracts.GateConfig {
		pins, drivers, err := s.gateKernels(ctx, g, map[string]bool{"sing-box": true})
		if err != nil {
			t.Fatal(err)
		}
		return contracts.GateConfig{Kernels: pins, Drivers: drivers}
	}
	cfg := makeConfig()
	if err := s.compileDomainObservers(ctx, g, &cfg); err != nil || len(cfg.Observers) != 0 {
		t.Fatal("old Gate", err)
	}
	if err := s.Store.Tasks.SetCapabilities(ctx, g.ID, contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Arch: "amd64", Version: "test", Tasks: map[string]int{contracts.TaskCoreInstall: contracts.TaskVersion}, ConfigFeatures: map[string]int{contracts.ConfigDomainObservation: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.compileDomainObservers(ctx, g, &cfg); err != nil {
		t.Fatal(err)
	}
	first := cfg.Observers["sing-box"]
	if first.Port == 0 || len(first.Secret) < 32 {
		t.Fatal("missing plan")
	}
	row := s.Store.Ent.Resource.Query().Where(resource.KindEQ(domainObserverKind)).OnlyX(ctx)
	if strings.Contains(row.String(), first.Secret) {
		t.Fatal("observer secret stored in plaintext")
	}
	cfg = makeConfig()
	if err := s.compileDomainObservers(ctx, g, &cfg); err != nil || cfg.Observers["sing-box"] != first {
		t.Fatal("unstable observer", err)
	}
	if err := s.checkPort(ctx, g, first.Port, 0); err == nil {
		t.Fatal("node claimed observer port")
	}
	if err := s.resolvePortConflict(ctx, g, first.Port); err != nil {
		t.Fatal(err)
	}
	_, saved, err := s.loadDomainObservers(ctx, s.Store.Ent, g.ID)
	if err != nil || saved["sing-box"].Port == first.Port || saved["sing-box"].Secret != first.Secret {
		t.Fatal("port recovery lost identity", err)
	}
}

func TestDomainDirectRuleCASAndAtomicCallback(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true}, 200)
	v := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "direct-domains@example.test", "password": "DomainOwner2026", "all_nodes": true}, 200)
	uid := int(v["id"].(float64))
	now := time.Now().UTC()
	data := observedDomainData{Domain: "domestic.example", Day: now.Format("2006-01-02"), Engine: "sing-box", ObservedAt: now, Bytes: 100}
	if err := putObservationResource(ctx, s.Store.Ent, domainDailyKind, data.Day+":fixture:domestic.example", int(g.id), data); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { subCheckBinary, subCheckRun = defaultSubCheckBinary, runSubCheck })
	subCheckBinary = func(*Server, context.Context, string) (string, string, error) { return "checker", "domains-test", nil }
	fail := true
	subCheckRun = func(context.Context, string, string, string, []byte) ([]byte, error) {
		if fail {
			return nil, errors.New("native check failed")
		}
		return nil, nil
	}
	path := "/api/v1/admin/domain-candidates/domestic.example/direct"
	mustJSON(t, admin, "POST", path, map[string]any{"user_id": uid, "revision": 0, "confirmed": true}, 422)
	if s.Store.Ent.Resource.Query().Where(resource.KindEQ(domainDecisionKind)).CountX(ctx) != 0 {
		t.Fatal("failed check committed decision")
	}
	fail = false
	mustJSON(t, admin, "POST", path, map[string]any{"user_id": uid, "revision": 0, "confirmed": true}, 409)
	mustJSON(t, admin, "POST", path, map[string]any{"user_id": uid, "revision": 1, "confirmed": true}, 200)
	u := s.Store.Ent.User.GetX(ctx, uid)
	base := loadPolicy(u)
	if len(base.Custom.Entries) != 1 || base.Custom.Entries[0].Target != "direct" {
		t.Fatal("rule not committed")
	}
	entries := append([]contracts.CustomEntry(nil), base.Custom.Entries...)
	entries = append(entries, contracts.CustomEntry{ID: "rollback", Kind: "domain_suffix", Value: "rollback.example", Target: "direct", Enabled: true})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/internal", nil).WithContext(ctx)
	s.writeCustomEntries(c, u, &base.Revision, entries, "", nil, func(context.Context, *ent.Client) error { return errors.New("decision write failed") })
	code := c.Writer.Status()
	if code != 500 || policyFingerprint(loadPolicy(s.Store.Ent.User.GetX(ctx, uid))) != policyFingerprint(base) {
		t.Fatal("callback did not roll back policy", code)
	}
}
