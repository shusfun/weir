package center

import (
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

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
