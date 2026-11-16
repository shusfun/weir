package center

import (
	"context"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

func TestRuleMeterIsolatedLabelsReplayAndBilling(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := context.Background()
	gate := s.Store.Ent.Gate.GetX(ctx, int(g.id))
	exit, err := s.directExit(ctx, s.Store.Ent, gate.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := s.Store.Ent.Node.Create().SetName("meter").SetGateID(gate.ID).SetExitID(exit.ID).SetKernel("xray").SetProtocol("vless").SetPort(31002).SaveX(ctx)
	u := s.Store.Ent.User.Create().SetEmail("rules@example.test").SetPasswordHash("x").SetUnlimited(true).SaveX(ctx)
	r1 := s.Store.Ent.Route.Create().SetGateID(gate.ID).SetExitID(exit.ID).SetDomain("domain:first.test").SetPriority(7).SaveX(ctx)
	r2 := s.Store.Ent.Route.Create().SetGateID(gate.ID).SetExitID(exit.ID).SetDomain("domain:second.test").SetPriority(9).SaveX(ctx)
	cfg := contracts.GateConfig{Meta: map[string]string{}}
	if err = s.compileRoutes(ctx, gate, &cfg, []*ent.Node{n}, nil); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Routes) != 2 || cfg.Routes[0].OutboundTag == cfg.Routes[1].OutboundTag || cfg.Routes[0].Priority != adminRouteBase+7 || cfg.Routes[1].Priority != adminRouteBase+9 {
		t.Fatalf("routing changed: %+v", cfg.Routes)
	}
	for i, rule := range []*ent.Route{r1, r2} {
		found := false
		for _, o := range cfg.Outbounds {
			if o.RuleID == int64(rule.ID) && o.Tag == cfg.Routes[i].OutboundTag && o.Protocol == "freedom" {
				found = true
			}
		}
		if !found {
			t.Fatal("rule did not retain actual direct exit")
		}
	}
	path := "/api/v1/admin/gates/" + itoa(gate.ID) + "/routes"
	view := mustJSON(t, admin, "GET", path, nil, 200)
	if view["items"].([]any)[0].(map[string]any)["usage"].(map[string]any)["bytes"] != nil {
		t.Fatal("unknown became zero")
	}
	now := time.Now().UTC()
	rep := contracts.MeterReport{Protocol: 2, Epoch: "rules-a", Seq: 1, Status: "fresh", Rules: []contracts.RuleMeterSample{
		{RuleID: int64(r1.ID), Engine: "xray", Up: 10, Down: 90, ObservedAt: now, Status: "fresh"},
		{RuleID: int64(r2.ID), Engine: "xray", Up: 20, Down: 180, ObservedAt: now, Status: "fresh"},
	}}
	for i := 0; i < 2; i++ {
		if code, b := g.call("POST", "/api/v1/agent/meter", rep); code != 200 {
			t.Fatalf("meter %d %s", code, b)
		}
	}
	if s.Store.Ent.User.GetX(ctx, u.ID).Used != 0 {
		t.Fatal("rule observation billed user")
	}
	check := func(want1, want2 float64) {
		view := mustJSON(t, admin, "GET", path, nil, 200)
		items := view["items"].([]any)
		if items[0].(map[string]any)["usage"].(map[string]any)["bytes"] != want1 || items[1].(map[string]any)["usage"].(map[string]any)["bytes"] != want2 {
			t.Fatalf("shared traffic or replay: %v", view)
		}
	}
	check(100, 200)
	rep.Seq++
	rep.Partial = true
	rep.Rules[0].Down = 140
	if code, b := g.call("POST", "/api/v1/agent/meter", rep); code != 200 {
		t.Fatalf("delta %d %s", code, b)
	}
	check(150, 200)
	// A rejected counter rollback must roll back the entire sequence as well.
	rep.Seq++
	rep.Rules[0].Down = 1
	if code, _ := g.call("POST", "/api/v1/agent/meter", rep); code != 500 {
		t.Fatalf("regression %d", code)
	}
	if got := s.Store.Ent.Gate.GetX(ctx, gate.ID).MeterSeq; got != 2 {
		t.Fatal("rejected batch acknowledged")
	}
	check(150, 200)
	rep.Epoch = "rules-b"
	rep.Seq = 1
	rep.Partial = false
	rep.Rules[0].Up = 2
	rep.Rules[0].Down = 8
	rep.Rules[1].Up = 0
	rep.Rules[1].Down = 0
	if code, b := g.call("POST", "/api/v1/agent/meter", rep); code != 200 {
		t.Fatalf("epoch %d %s", code, b)
	}
	check(160, 200)
	// A foreign Gate cannot mutate either rule, even knowing the rule IDs.
	other := pairFakeGate(t, admin)
	rep.Epoch = "other"
	rep.Rules[0].Down = 10000
	if code, b := other.call("POST", "/api/v1/agent/meter", rep); code != 200 {
		t.Fatalf("other %d %s", code, b)
	}
	check(160, 200)
	if s.Store.Ent.Resource.Query().Where(resource.KindEQ(ruleDailyKind)).CountX(ctx) != 2 {
		t.Fatal("foreign rule rows created")
	}
}
