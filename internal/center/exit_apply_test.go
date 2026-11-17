package center

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/internal/store/ent/exitbinding"
	"github.com/shusfun/weir/packages/contracts"
)

func TestExitSaveAndProbeDoNotBindUntilUse(t *testing.T) {
	s, admin, g := setupGate(t)
	ex := mustJSON(t, admin, "POST", "/api/v1/admin/exits", map[string]any{"kind": "socks5", "address": "203.0.113.9", "port": 1080, "password": "secret"}, 200)
	id := int(ex["id"].(float64))
	if n := s.Store.Ent.ExitBinding.Query().CountX(t.Context()); n != 0 {
		t.Fatalf("saving an exit created %d bindings", n)
	}
	oldMeasure, oldBin := measureExitProxy, deployedProbeBinary
	t.Cleanup(func() { measureExitProxy, deployedProbeBinary = oldMeasure, oldBin })
	measureExitProxy = func(context.Context, nodeprobe.MeasureOptions) (nodeprobe.MeasureResult, error) {
		return nodeprobe.MeasureResult{LatencyMS: 12}, nil
	}
	deployedProbeBinary = func(*Server, context.Context, string) (string, kernelconf.Driver, error) {
		return "mihomo", kernelconf.Driver{}, nil
	}
	probed := mustJSON(t, admin, "POST", "/api/v1/admin/exits/"+itoa(id)+"/probe", map[string]any{"location": "center"}, 200)
	if probed["location"] != "center" || probed["ok"] != true {
		t.Fatalf("center probe = %v", probed)
	}
	var failProbe atomic.Bool
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionChainProbe: func(contracts.Action) contracts.ActionResult {
			if failProbe.Load() {
				return contracts.ActionResult{OK: false, Error: "exit_probe_connect_failed"}
			}
			return contracts.ActionResult{OK: true, Result: map[string]any{"latency_ms": 9, "exit_ip": "198.51.100.9", "udp": true}}
		},
	})
	gateProbe := mustJSON(t, admin, "POST", "/api/v1/admin/exits/"+itoa(id)+"/probe", map[string]any{"gate_id": int(g.id)}, 200)
	if gateProbe["location"] != "gate" || int(gateProbe["gate_id"].(float64)) != int(g.id) {
		t.Fatalf("gate probe = %v", gateProbe)
	}
	if n := s.Store.Ent.ExitBinding.Query().CountX(t.Context()); n != 0 {
		t.Fatalf("probe created %d bindings", n)
	}
	node := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	mustJSON(t, admin, "PATCH", "/api/v1/admin/nodes/"+itoa(int(node["id"].(float64))), map[string]any{"exit_id": id}, 200)
	before := s.Store.Ent.ExitBinding.Query().Where(exitbinding.ExitIDEQ(id), exitbinding.GateIDEQ(int(g.id))).OnlyX(t.Context())
	if before.Address != "203.0.113.9" || before.Status != exitbinding.StatusApplied {
		t.Fatalf("applied binding = %+v", before)
	}
	failProbe.Store(true)
	updated := mustJSON(t, admin, "PATCH", "/api/v1/admin/exits/"+itoa(id), map[string]any{"address": "203.0.113.10"}, 200)
	if updated["address"] != "203.0.113.10" {
		t.Fatalf("desired address = %v", updated["address"])
	}
	after := s.Store.Ent.ExitBinding.Query().Where(exitbinding.ExitIDEQ(id), exitbinding.GateIDEQ(int(g.id))).OnlyX(t.Context())
	if after.Address != "203.0.113.9" || after.Status != exitbinding.StatusFailed {
		t.Fatalf("failed VPS replaced applied config: %+v", after)
	}
	apps, _ := updated["applications"].([]any)
	if len(apps) != 1 || apps[0].(map[string]any)["status"] != "kept" {
		t.Fatalf("applications = %v", updated["applications"])
	}
}
