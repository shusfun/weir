package center

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

func TestDailyUsageRetriesEpochAndInternalHop(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := context.Background()
	u := s.Store.Ent.User.Create().SetEmail("daily@example.test").SetPasswordHash("x").SetUnlimited(true).SaveX(ctx)
	exit, err := s.directExit(ctx, s.Store.Ent, int(g.id))
	if err != nil {
		t.Fatal(err)
	}
	n := s.Store.Ent.Node.Create().SetGateID(int(g.id)).SetName("daily").SetKernel("xray").SetProtocol("vless").SetPort(31001).SetExitID(exit.ID).SaveX(ctx)
	c := s.Store.Ent.Credential.Create().SetNodeID(n.ID).SetUserID(u.ID).SetStatsKey("daily-user").SaveX(ctx)
	s.Store.Ent.Credential.Create().SetNodeID(n.ID).SetUserID(u.ID).SetInternal(true).SetStatsKey("daily-hop").SaveX(ctx)
	report := contracts.MeterReport{Protocol: 2, Epoch: "daily-1", Seq: 1, Status: "fresh", Usage: []contracts.MeterSample{{StatsKey: c.StatsKey, Up: 300, Down: 700}, {StatsKey: "daily-hop", Up: 300, Down: 700}}}
	for i := 0; i < 2; i++ {
		if code, b := g.call("POST", "/api/v1/agent/meter", report); code != 200 {
			t.Fatalf("meter %d %s", code, b)
		}
	}
	report.Seq = 2
	report.Usage[0].Down = 900
	if code, b := g.call("POST", "/api/v1/agent/meter", report); code != 200 {
		t.Fatalf("meter %d %s", code, b)
	}
	report.Epoch, report.Seq, report.Usage[0].Up, report.Usage[0].Down = "daily-2", 1, 10, 40
	if code, b := g.call("POST", "/api/v1/agent/meter", report); code != 200 {
		t.Fatalf("meter %d %s", code, b)
	}
	rows := s.Store.Ent.Resource.Query().Where(resource.KindEQ(dailyUsageKind)).AllX(ctx)
	if len(rows) != 1 {
		t.Fatalf("daily rows: %d", len(rows))
	}
	var row dailyUsage
	if err := decodeResource(rows[0], &row); err != nil {
		t.Fatal(err)
	}
	if row.Bytes != 1250 || s.Store.Ent.User.GetX(ctx, u.ID).Used != 1250 {
		t.Fatalf("duplicate/internal counted: %+v", row)
	}
	view := mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(u.ID)+"/usage", nil, 200)
	items := view["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["bytes"] != float64(1250) {
		t.Fatalf("ranking: %v", view)
	}
	mustJSON(t, admin, "GET", "/api/v1/admin/nodes/"+itoa(n.ID)+"/usage", nil, 200)
	// The logged-in admin's self endpoint cannot leak another user's rows.
	self := mustJSON(t, admin, "GET", "/api/v1/me/usage", nil, 200)
	if len(self["items"].([]any)) != 0 {
		t.Fatalf("self leaked: %v", self)
	}
}

func TestNetworkPeriodClampAndCounterReboot(t *testing.T) {
	for _, at := range []string{"2028-02-28T23:00:00Z", "2028-02-29T01:00:00Z", "2027-02-28T01:00:00Z", "2028-03-01T01:00:00Z", "2028-01-01T01:00:00Z"} {
		now, _ := time.Parse(time.RFC3339, at)
		start, next := networkPeriod(now, 31)
		if now.Before(start) || !now.Before(next) || !start.Before(next) {
			t.Fatalf("bad period: %s %s %s", now, start, next)
		}
	}
	now := time.Date(2028, 2, 29, 1, 0, 0, 0, time.UTC)
	rx, tx := uint64(1000), uint64(2000)
	h := &contracts.HostSample{ObservedAt: now, BootID: "a", NetRxBytes: &rx, NetTxBytes: &tx}
	v, err := advanceNetworkUsage(gateNetworkUsage{}, h, 31)
	if err != nil {
		t.Fatal(err)
	}
	rx, tx = 1100, 2400
	h.ObservedAt = now.Add(time.Minute)
	v, err = advanceNetworkUsage(v, h, 31)
	if err != nil || v.TotalRX != 100 || v.TotalTX != 400 {
		t.Fatalf("delta: %+v %v", v, err)
	}
	copy, err := advanceNetworkUsage(v, h, 31)
	if err != nil || copy != v {
		t.Fatal("duplicate changed counters")
	}
	rx, tx = 10, 30
	h.BootID = "b"
	h.ObservedAt = now.Add(2 * time.Minute)
	v, err = advanceNetworkUsage(v, h, 31)
	if err != nil || v.TotalRX != 110 || v.TotalTX != 430 {
		t.Fatalf("reboot: %+v %v", v, err)
	}
	if networkBytes(v, "sum") != 540 || networkBytes(v, "max") != 430 || networkBytes(v, "outbound") != 430 {
		t.Fatal("billing mode")
	}
	cal := uint64(10000)
	settings := gateTrafficSettings{Mode: "sum", Calibration: &cal, CalibrationBase: 540, CalibrationPeriod: v.Start}
	v.TotalTX += 100
	if billedNetworkBytes(settings, v) != 10100 {
		t.Fatal("calibration")
	}
	h.ObservedAt = time.Date(2028, 3, 31, 1, 0, 0, 0, time.UTC)
	v, err = advanceNetworkUsage(v, h, 31)
	if err != nil || networkBytes(v, "sum") != 0 || billedNetworkBytes(settings, v) != 0 || !v.Partial {
		t.Fatalf("reset: %+v %v", v, err)
	}
}

func TestResetDueUsageKeepsCurrentPeriod(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	dueAt := time.Date(2026, 1, 31, 8, 0, 0, 0, time.UTC)
	due := s.Store.Ent.User.Create().SetEmail("due@example.test").SetPasswordHash("x").SetMonthlyReset(true).SetUsed(100).SetQuota(1000).SetUsageResetAt(dueAt).SaveX(ctx)
	current := s.Store.Ent.User.Create().SetEmail("current@example.test").SetPasswordHash("x").SetMonthlyReset(true).SetUsed(40).SetQuota(1000).SetUsageResetAt(time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)).SaveX(ctx)
	if err := s.ResetDueUsage(ctx, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if got := s.Store.Ent.User.GetX(ctx, due.ID); got.Used != 0 || got.UsageResetAt.IsZero() {
		t.Fatal("due usage remained", got.Used, got.UsageResetAt)
	}
	if got := s.Store.Ent.User.GetX(ctx, current.ID); got.Used != 40 {
		t.Fatal("current period was cleared", got.Used)
	}
}

func TestGateTrafficConfigureRevisionAndObservation(t *testing.T) {
	s, admin, g := setupGate(t)
	path := "/api/v1/admin/gates/" + itoa(int(g.id)) + "/traffic"
	initial := mustJSON(t, admin, "GET", path, nil, 200)
	if initial["used"] != nil {
		t.Fatal("missing data became zero")
	}
	body := map[string]any{"quota": 1000, "mode": "sum", "reset_day": 1, "revision": initial["revision"]}
	if w := admin.do("PUT", path, body, nil); w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body.String())
	}
	if w := admin.do("PUT", path, body, nil); w.Code != 409 {
		t.Fatalf("stale revision %d", w.Code)
	}
	now := time.Now().UTC().Add(-time.Second)
	rx, tx := uint64(1000), uint64(2000)
	h := &contracts.HostSample{ObservedAt: now, BootID: "a", NetRxBytes: &rx, NetTxBytes: &tx}
	if code, b := g.sendHeartbeat(contracts.Heartbeat{Version: "test", Arch: "amd64", Host: h}); code != 200 {
		t.Fatalf("sample: %d %s", code, b)
	}
	h.ObservedAt = now.Add(time.Second)
	rx, tx = 1300, 2600
	if code, b := g.sendHeartbeat(contracts.Heartbeat{Version: "test", Arch: "amd64", Host: h}); code != 200 {
		t.Fatalf("sample: %d %s", code, b)
	}
	view := mustJSON(t, admin, "GET", path, nil, 200)
	if view["used"] != float64(900) || view["alert"] != "warning" {
		t.Fatalf("usage: %v", view)
	}
	body["revision"], body["meter_revision"], body["calibration"] = view["revision"], view["meter_revision"], 970
	calibrated := mustJSON(t, admin, "PUT", path, body, 200)
	if calibrated["used"] != float64(970) || calibrated["alert"] != "critical" {
		t.Fatalf("calibration %v", calibrated)
	}
	settings, usage, err := readGateTraffic(context.Background(), s.Store.Ent, int(g.id))
	if err != nil {
		t.Fatal(err)
	}
	if settings.Calibration == nil || *settings.Calibration != 970 {
		t.Fatal("calibration lost")
	}
	stale := gateTrafficView(settings, usage, time.Now().Add(3*time.Minute))
	raw, _ := json.Marshal(stale["state"])
	var state objectState
	_ = json.Unmarshal(raw, &state)
	if state.Status != "stale" {
		t.Fatalf("stale observation: %+v", state)
	}
}
