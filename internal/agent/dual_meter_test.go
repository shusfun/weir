package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/internal/xray"
)

type meterFixture struct {
	xray.StatsClient
	values map[string]*xray.ClientStats
	err    error
}

func (m meterFixture) QueryAllStats(context.Context) (map[string]*xray.ClientStats, error) {
	return m.values, m.err
}

func TestDualMeterFailureIsScopedToCore(t *testing.T) {
	a := &Agent{State: State{Inbounds: []db.Inbound{{ID: 1, Core: db.CoreXray, Enabled: true}, {ID: 2, Core: db.CoreSingbox, Enabled: true}}, Grants: []LocalGrant{{GrantID: 1, InboundID: 1, Enabled: true, DesiredEnabled: true, Allocated: 1000}, {GrantID: 2, InboundID: 2, Enabled: true, DesiredEnabled: true, Allocated: 1000}}}}
	a.stats = meterFixture{values: map[string]*xray.ClientStats{"grant-1": {Downlink: 100}}}
	a.singStats = meterFixture{err: errors.New("unavailable")}
	if !a.collectLocked(context.Background()) || !a.State.Grants[0].Enabled || a.State.Grants[1].Enabled || a.State.Grants[0].Used != 100 || a.TrafficStatus != "unknown" {
		t.Fatal("sing-box failure crossed core boundary or failed open")
	}
	a.singStats = meterFixture{values: map[string]*xray.ClientStats{"grant-2": {Downlink: 200}}}
	a.stats = meterFixture{err: errors.New("unavailable")}
	if !a.collectLocked(context.Background()) || a.State.Grants[0].Enabled || !a.State.Grants[1].Enabled || a.State.Grants[1].Used != 200 {
		t.Fatal("xray failure crossed core boundary or failed open")
	}
}

// Executed only inside the disposable actual systemd/core test environment.
func TestLiveSingboxMeter(t *testing.T) {
	if os.Getenv("MIGATE_DUAL_LIVE") != "1" {
		t.Skip("requires isolated running kernels")
	}
	ctx := context.Background()
	client, err := xray.NewGRPCStatsClientWithEngineAndService(ctx, "127.0.0.1:19817", "singbox", "v2ray.core.app.stats.command.StatsService")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	data, err := os.ReadFile("/var/tmp/dual-state.json")
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{stats: meterFixture{values: map[string]*xray.ClientStats{}}, singStats: client}
	if err = json.Unmarshal(data, &a.State); err != nil {
		t.Fatal(err)
	}
	a.collectLocked(ctx)
	if len(a.State.Grants) != 1 || a.State.Grants[0].Used < 1048576 || !a.State.Grants[0].Enabled || a.TrafficStatus != "fresh" {
		values, queryErr := client.QueryTrafficStats(ctx)
		t.Logf("isolated counters: %+v error=%v", values, queryErr)
		t.Fatalf("actual per-user counters not charged: status=%s count=%d used=%d enabled=%v", a.TrafficStatus, len(a.State.Grants), a.State.Grants[0].Used, a.State.Grants[0].Enabled)
	}
	used := a.State.Grants[0].Used
	a.collectLocked(ctx)
	if a.State.Grants[0].Used != used {
		t.Fatal("same counters charged twice")
	}
}

func TestLiveApplyDiagnostic(t *testing.T) {
	if os.Getenv("MIGATE_DUAL_LIVE") != "1" {
		t.Skip("isolated kernels only")
	}
	data, err := os.ReadFile("/var/tmp/dual-failed-state.json")
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err = json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if err = applyState(context.Background(), state); err != nil {
		t.Fatal(err)
	}
}
