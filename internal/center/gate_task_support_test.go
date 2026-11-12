package center

import (
	"context"
	"testing"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/packages/contracts"
)

func TestTaskCapabilitiesOverrideLegacyHeartbeat(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := context.Background()
	g := &ent.Gate{ID: 971, Host: map[string]any{"node_test_version": 3}}
	if ok, err := s.gateSupportsTask(ctx, g, contracts.ActionPanelDiscover, 2); err != nil || !ok {
		t.Fatalf("newer legacy worker rejected: %v %v", ok, err)
	}
	caps := contracts.CapabilityDocument{Protocol: 2, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.ActionNodeTest: contracts.TaskVersion}}
	if err := s.Store.Tasks.SetCapabilities(ctx, g.ID, caps); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.gateSupportsTask(ctx, g, contracts.ActionPanelDiscover, 2); err != nil || ok {
		t.Fatalf("missing capability bypassed: %v %v", ok, err)
	}
	if ok, err := s.gateSupportsTask(ctx, g, contracts.ActionNodeTest, 3); err != nil || !ok {
		t.Fatalf("declared task rejected: %v %v", ok, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.gateSupportsTask(cancelled, g, contracts.ActionNodeTest, 3); err == nil {
		t.Fatal("database failure was hidden as unsupported")
	}
}
