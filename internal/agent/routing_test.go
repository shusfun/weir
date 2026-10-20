package agent

import (
	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/packages/contracts"
	"testing"
)

func TestRoutingEnableStateAndDependencies(t *testing.T) {
	a := &Agent{State: State{Outbounds: []db.Outbound{{ID: 1, Tag: "deny", Protocol: "blackhole", Enabled: true}}, Routing: []db.RoutingRule{{ID: 2, OutboundID: 1, Enabled: true}}}}
	mutate := func(op string, rev int64, payload map[string]any) error {
		return a.mutate(contracts.Task{Operation: op, Revision: rev, Payload: payload})
	}
	out := map[string]any{"id": 1, "tag": "deny", "protocol": "blackhole", "enabled": false}
	if err := mutate("outbound.upsert", 1, out); err == nil || !a.State.Outbounds[0].Enabled {
		t.Fatal("active route lost its outbound")
	}
	route := map[string]any{"id": 2, "outbound_id": 1, "enabled": false}
	if err := mutate("routing.upsert", 2, route); err != nil || a.State.Routing[0].Enabled {
		t.Fatal("route disable failed", err)
	}
	if err := mutate("outbound.upsert", 3, out); err != nil || a.State.Outbounds[0].Enabled {
		t.Fatal("unused outbound disable failed", err)
	}
	delete(out, "enabled")
	if err := mutate("outbound.upsert", 4, out); err != nil || a.State.Outbounds[0].Enabled {
		t.Fatal("legacy edit re-enabled outbound", err)
	}
	delete(route, "enabled")
	if err := mutate("routing.upsert", 5, route); err != nil || a.State.Routing[0].Enabled {
		t.Fatal("legacy edit re-enabled route", err)
	}
	route["enabled"] = true
	if err := mutate("routing.upsert", 6, route); err == nil || a.State.Routing[0].Enabled {
		t.Fatal("route enabled with disabled target")
	}
	out["enabled"] = true
	if err := mutate("outbound.upsert", 7, out); err != nil {
		t.Fatal(err)
	}
	if err := mutate("routing.upsert", 8, route); err != nil || !a.State.Routing[0].Enabled {
		t.Fatal("route re-enable failed", err)
	}
	out["enabled"] = "false"
	if err := mutate("outbound.upsert", 9, out); err == nil || !a.State.Outbounds[0].Enabled {
		t.Fatal("invalid type changed state")
	}
}

func TestRoutingResourceVersionsAndReferences(t *testing.T) {
	a := &Agent{}
	task := contracts.Task{Operation: "outbound.upsert", Revision: 2, Payload: map[string]any{"id": 1, "tag": "deny-test", "protocol": "blackhole"}}
	if err := a.mutate(task); err != nil {
		t.Fatal(err)
	}
	if err := a.mutate(task); err != nil || len(a.State.Outbounds) != 1 {
		t.Fatal("outbound duplicated", err)
	}
	task.Revision = 1
	if err := a.mutate(task); err == nil {
		t.Fatal("stale outbound accepted")
	}
	task = contracts.Task{Operation: "routing.upsert", Revision: 3, Payload: map[string]any{"id": 2, "outbound_id": 99}}
	if err := a.mutate(task); err == nil {
		t.Fatal("missing outbound accepted")
	}
	task.Payload["outbound_id"] = 1
	if err := a.mutate(task); err != nil {
		t.Fatal(err)
	}
	if err := a.mutate(task); err != nil || len(a.State.Routing) != 1 {
		t.Fatal("route duplicated", err)
	}
	del := contracts.Task{Operation: "outbound.delete", Revision: 4, Payload: map[string]any{"id": 1}}
	if err := a.mutate(del); err == nil {
		t.Fatal("referenced outbound deleted")
	}
	if err := a.mutate(contracts.Task{Operation: "routing.delete", Revision: 4, Payload: map[string]any{"id": 2}}); err != nil {
		t.Fatal(err)
	}
	if err := a.mutate(del); err != nil || len(a.State.Outbounds) != 0 {
		t.Fatal(err)
	}
}
