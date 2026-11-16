package center

import (
	"testing"

	"github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/packages/contracts"
)

func TestUDPPathIncludesRoutingAndUserChains(t *testing.T) {
	s, admin, gate := setupGate(t)
	ctx := t.Context()
	gid := int(gate.id)
	direct, err := s.directExit(ctx, s.Store.Ent, gid)
	if err != nil {
		t.Fatal(err)
	}
	n := s.Store.Ent.Node.Create().SetGateID(gid).SetExitID(direct.ID).SetName("UDP paths").SetKernel("sing-box").SetProtocol("shadowsocks").SetPort(31101).SaveX(ctx)
	http := s.Store.Ent.Exit.Create().SetGateID(gid).SetTag("tcp-only").SetKind(exit.KindHTTP).SetAddress("203.0.113.20").SetPort(8080).SaveX(ctx)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "udp-path@example.test"}, 200)
	uid := int(u["id"].(float64))
	r := s.Store.Ent.Route.Create().SetGateID(gid).SetNodeID(n.ID).SetUserID(uid).SetExitID(http.ID).SetDomain("tcp.example.test").SaveX(ctx)
	if _, known := s.nodeUDP(ctx, n); known {
		t.Fatal("mixed selective routes claimed a uniform UDP capability")
	}
	if _, known := s.nodeRouteUDP(ctx, n, &uid, false, map[int]bool{}); known {
		t.Fatal("subscriber's selective route was ignored")
	}
	internal := 0
	if yes, known := s.nodeRouteUDP(ctx, n, &internal, false, map[int]bool{}); !yes || !known {
		t.Fatal("user-specific route affected internal transport")
	}
	r.Update().SetEnabled(false).ExecX(ctx)
	n = n.Update().SetExitID(http.ID).SaveX(ctx)
	if yes, known := s.nodeUDP(ctx, n); yes || !known {
		t.Fatal("HTTP default must reject UDP")
	}
	s.Store.Ent.Chain.Create().SetName("UDP chain").SetUserID(uid).SetEntryNodeID(n.ID).SetExitID(direct.ID).SaveX(ctx)
	if _, known := s.nodeUDP(ctx, n); known {
		t.Fatal("TCP default must not disable the user's UDP chain")
	}
	if yes, known := s.nodeRouteUDP(ctx, n, &uid, false, map[int]bool{}); yes || !known {
		t.Fatal("chain credential changed the ordinary subscription path")
	}
	g := s.Store.Ent.Gate.GetX(ctx, gid)
	cfg, err := s.compileGateConfig(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range cfg.Inbounds {
		if in.ID == int64(n.ID) && in.Settings["reject_udp"] == true {
			t.Fatal("node-wide rejection broke a working chain")
		}
	}
}

func TestUDPMembersSkipUnknownAndHTTP(t *testing.T) {
	members := []contracts.Outbound{{Protocol: "freedom"}, {Protocol: "http"}, {Protocol: "socks5"}}
	got := udpMembers(members)
	if len(got) != 1 || got[0].Protocol != "freedom" {
		t.Fatalf("udp members = %+v", got)
	}
}

func TestUDPMembersUsesMeasuredSOCKS(t *testing.T) {
	yes, no := true, false
	members := []contracts.Outbound{
		{Protocol: "socks", Settings: map[string]any{"udp": yes}},
		{Protocol: "socks", Settings: map[string]any{"udp": no}},
		{Protocol: "socks"},
	}
	got := udpMembers(members)
	if len(got) != 1 || got[0].Settings["udp"] != true {
		t.Fatalf("measured socks members = %+v", got)
	}
	if allUDPKnown(members) {
		t.Fatal("unmeasured socks must stay unknown")
	}
}

func TestGroupExitUDP(t *testing.T) {
	supported, known := groupExitUDP(contracts.Outbound{Protocol: "group", Settings: map[string]any{"udp_members": 2}})
	if !supported || !known {
		t.Fatal("members that support UDP must be known supported")
	}
	supported, known = groupExitUDP(contracts.Outbound{Protocol: "group", Settings: map[string]any{"udp_supported": false, "udp_members": 0}})
	if supported || !known {
		t.Fatal("all-unsupported group must be known unsupported")
	}
	supported, known = groupExitUDP(contracts.Outbound{Protocol: "group", Settings: map[string]any{"udp_members": 0}})
	if supported || known {
		t.Fatal("unmeasured group must stay unknown")
	}
}

func TestUDPKind(t *testing.T) {
	yes := true
	no := false
	cases := []struct {
		kind      string
		measured  *bool
		supported bool
		known     bool
	}{
		{"freedom", nil, true, true},
		{"http", nil, false, true},
		{"https", nil, false, true},
		{"blackhole", nil, false, true},
		{"socks5", nil, false, false},
		{"socks5", &yes, true, true},
		{"socks5", &no, false, true},
	}
	for _, c := range cases {
		supported, known := udpKind(c.kind, c.measured)
		if supported != c.supported || known != c.known {
			t.Fatalf("%s = %v %v", c.kind, supported, known)
		}
	}
}
