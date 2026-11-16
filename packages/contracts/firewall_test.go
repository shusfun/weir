package contracts

import (
	"slices"
	"testing"
)

func TestInboundFirewallSharesPortsAndTransport(t *testing.T) {
	plan := InboundFirewall([]Inbound{{Protocol: "vless", Port: 4433}, {Protocol: "vless", Port: 4433}, {Protocol: "hysteria2", Port: 4433}, {Protocol: "shadowsocks", Port: 8443}})
	want := []FirewallPort{{4433, "tcp"}, {4433, "udp"}, {8443, "tcp"}, {8443, "udp"}}
	if !slices.Equal(plan.Ports, want) || plan.Validate() != nil {
		t.Fatal(plan)
	}
	if InboundFirewall(nil).Ports == nil {
		t.Fatal("empty plan must explicitly remove managed ports")
	}
}

func TestFirewallPlanPortBounds(t *testing.T) {
	for _, port := range []int{1, 80, 443, 65535} {
		if err := (FirewallPlan{Version: 1, Ports: []FirewallPort{{port, "tcp"}}}).Validate(); err != nil {
			t.Fatalf("port %d: %v", port, err)
		}
	}
	for _, port := range []int{0, -1, 65536} {
		if err := (FirewallPlan{Version: 1, Ports: []FirewallPort{{port, "udp"}}}).Validate(); err == nil {
			t.Fatalf("accepted port %d", port)
		}
	}
}
