package center

import (
	"reflect"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

func TestOutboundExecutionPlanAndStableSealedPorts(t *testing.T) {
	s, _, fake := setupGate(t)
	ctx := t.Context()
	g := s.Store.Ent.Gate.GetX(ctx, int(fake.id))
	makeConfig := func() contracts.GateConfig {
		return contracts.GateConfig{Outbounds: []contracts.Outbound{{ID: 10, Tag: "external", Protocol: "anytls", Address: "exit.example.test", Port: 443, Password: "secret"}}}
	}
	cfg := makeConfig()
	if err := s.compileOutboundBridges(ctx, g, &cfg, map[string]bool{"xray": true}); err == nil || !strings.Contains(err.Error(), "更新 Gate") {
		t.Fatalf("old Gate accepted new capability: %v", err)
	}
	if err := s.Store.Tasks.SetCapabilities(ctx, g.ID, contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskCoreInstall: contracts.TaskVersion}, ConfigFeatures: map[string]int{contracts.ConfigOutboundBridges: 1}}); err != nil {
		t.Fatal(err)
	}
	need := map[string]bool{"xray": true}
	if err := s.compileOutboundBridges(ctx, g, &cfg, need); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Bridges) != 1 || cfg.Bridges[0].Kernel != "sing-box" || !need["sing-box"] {
		t.Fatalf("wrong execution plan: %+v", cfg.Bridges)
	}
	first := cfg.Bridges[0]
	row := s.Store.Ent.Resource.Query().Where(resource.KindEQ(outboundBridgeKind)).OnlyX(ctx)
	if row.Data["items"] == nil || strings.Contains(row.String(), first.Password) {
		t.Fatal("bridge credentials stored unsealed")
	}
	cfg = makeConfig()
	if err := s.compileOutboundBridges(ctx, g, &cfg, map[string]bool{"xray": true}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, cfg.Bridges[0]) {
		t.Fatal("recompile rotated bridge identity")
	}
	if err := s.checkPort(ctx, g, first.Port, 0); err == nil {
		t.Fatal("node may claim internal port")
	}
	if err := s.resolvePortConflict(ctx, g, first.Port); err != nil {
		t.Fatal(err)
	}
	_, saved, err := s.loadOutboundBridges(ctx, s.Store.Ent, g.ID)
	if err != nil || len(saved) != 1 || saved[0].Port == first.Port || saved[0].Password != first.Password {
		t.Fatal("port recovery lost authentication", err)
	}
	current := s.Store.Ent.Gate.GetX(ctx, g.ID)
	if current.ConfigRevision != g.ConfigRevision+1 {
		t.Fatal("port recovery did not advance revision")
	}
}
