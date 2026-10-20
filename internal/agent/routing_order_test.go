package agent

import (
	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/packages/contracts"
	"path/filepath"
	"testing"
)

func TestRoutingOrderAtomicValidationAndPersistence(t *testing.T) {
	a, err := New(Config{DeviceID: 8, DataDir: filepath.Join(t.TempDir(), "state")})
	if err != nil {
		t.Fatal(err)
	}
	defer a.DB.Close()
	a.State.Routing = []db.RoutingRule{{ID: 11, Sort: 0, Domain: "one.example", OutboundID: 5, Enabled: true}, {ID: 12, Sort: 0, Domain: "two.example", OutboundID: 6, Enabled: true}}
	makeTask := func(rules []contracts.RoutePosition, rev int64) contracts.Task {
		return contracts.Task{Operation: "routing.reorder", Revision: rev, Payload: map[string]any{"rules": rules}}
	}
	for _, invalid := range [][]contracts.RoutePosition{{{ID: 11, Sort: 0}}, {{ID: 11, Sort: 0}, {ID: 11, Sort: 0}}, {{ID: 11, Sort: 0}, {ID: 99, Sort: 0}}, {{ID: 11, Sort: 1}, {ID: 12, Sort: 0}}} {
		if err = a.mutate(makeTask(invalid, 5)); err == nil {
			t.Fatal("invalid/stale order accepted")
		}
		if a.State.Routing[0].Sort != 0 || a.State.Routing[1].Sort != 0 {
			t.Fatal("invalid reorder partially mutated rules")
		}
	}
	task := makeTask([]contracts.RoutePosition{{ID: 12, Sort: 0}, {ID: 11, Sort: 0}}, 5)
	if err = a.mutate(task); err != nil {
		t.Fatal(err)
	}
	if a.State.Routing[0].Sort != 20 || a.State.Routing[1].Sort != 10 || a.State.Routing[0].Domain != "one.example" || a.State.Routing[1].OutboundID != 6 {
		t.Fatal("reorder changed attributes or wrong priority")
	}
	if err = a.mutate(task); err != nil {
		t.Fatal("idempotent reorder rejected", err)
	}
	if err = a.persist(); err != nil {
		t.Fatal(err)
	}
	restored, err := New(a.Config)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.DB.Close()
	if restored.State.Routing[0].Sort != 20 || restored.State.Versions["routing:12"] != 5 {
		t.Fatal("priority/version did not survive load")
	}
	if err = a.mutate(makeTask([]contracts.RoutePosition{{ID: 11, Sort: 20}, {ID: 12, Sort: 10}}, 4)); err == nil {
		t.Fatal("stale revision accepted")
	}
	if err = a.mutate(contracts.Task{Operation: "routing.upsert", Revision: 4, Payload: map[string]any{"id": 11, "outbound_id": 5, "sort": 0}}); err == nil {
		t.Fatal("old upsert reset new priority")
	}
}
