package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/packages/contracts"
)

func TestInboundEnabledPreservationAndMeter(t *testing.T) {
	a, err := New(Config{DataDir: filepath.Join(t.TempDir(), "data")})
	if err != nil {
		t.Fatal(err)
	}
	defer a.DB.Close()
	p := map[string]any{"id": 1, "port": 24446, "protocol": "vless", "enabled": false}
	if err := a.mutate(contracts.Task{Operation: "inbound.upsert", Revision: 1, Payload: p}); err != nil {
		t.Fatal(err)
	}
	if a.State.Inbounds[0].Enabled {
		t.Fatal("explicit disable ignored")
	}
	delete(p, "enabled")
	p["remark"] = "still-disabled"
	if err := a.mutate(contracts.Task{Operation: "inbound.upsert", Revision: 2, Payload: p}); err != nil || a.State.Inbounds[0].Enabled {
		t.Fatal("omitted edit reenabled inbound")
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	b, err := New(a.Config)
	if err != nil {
		t.Fatal(err)
	}
	defer b.DB.Close()
	if b.State.Inbounds[0].Enabled {
		t.Fatal("disabled state lost on restart")
	}
	a.State.Inbounds = append(a.State.Inbounds, db.Inbound{ID: 2, Core: db.CoreXray, Enabled: true})
	a.State.Grants = []LocalGrant{{GrantID: 1, InboundID: 1, Enabled: true, DesiredEnabled: true, Allocated: 1000}, {GrantID: 2, InboundID: 2, Enabled: true, DesiredEnabled: true, Allocated: 1000}}
	a.stats = meterFixture{}
	if !a.collectLocked(context.Background()) || a.State.Grants[0].Enabled || !a.State.Grants[1].Enabled {
		t.Fatal("inbound state not scoped to own grants")
	}
	p["enabled"] = true
	if err := a.mutate(contracts.Task{Operation: "inbound.upsert", Revision: 3, Payload: p}); err != nil {
		t.Fatal(err)
	}
	a.collectLocked(context.Background())
	if !a.State.Grants[0].Enabled {
		t.Fatal("reenable did not restore own grant")
	}
	p["enabled"] = "false"
	if a.mutate(contracts.Task{Operation: "inbound.upsert", Revision: 4, Payload: p}) == nil {
		t.Fatal("invalid bool accepted")
	}
}
