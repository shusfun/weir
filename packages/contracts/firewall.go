package contracts

import (
	"fmt"
	"slices"
)

const ConfigFirewall = "firewall"

type FirewallPort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// A non-nil empty plan removes only the rules owned by Weir.
type FirewallPlan struct {
	Version int            `json:"version"`
	Ports   []FirewallPort `json:"ports"`
}

func (p FirewallPlan) Validate() error {
	if p.Version != 1 || len(p.Ports) > 4096 {
		return fmt.Errorf("unsupported firewall plan")
	}
	for _, v := range p.Ports {
		if v.Port < 1 || v.Port > 65535 || v.Protocol != "tcp" && v.Protocol != "udp" {
			return fmt.Errorf("invalid firewall port")
		}
	}
	return nil
}

func InboundFirewall(inbounds []Inbound) *FirewallPlan {
	p := &FirewallPlan{Version: 1, Ports: []FirewallPort{}}
	for _, in := range inbounds {
		protocols := []string{"tcp"}
		switch in.Protocol {
		case "hysteria2", "tuic", "wireguard":
			protocols = []string{"udp"}
		case "shadowsocks":
			protocols = []string{"tcp", "udp"}
			if in.Settings["tcp_only"] == true {
				protocols = []string{"tcp"}
			}
		}
		for _, protocol := range protocols {
			v := FirewallPort{Port: in.Port, Protocol: protocol}
			if !slices.Contains(p.Ports, v) {
				p.Ports = append(p.Ports, v)
			}
		}
	}
	slices.SortFunc(p.Ports, func(a, b FirewallPort) int {
		if a.Port != b.Port {
			return a.Port - b.Port
		}
		if a.Protocol < b.Protocol {
			return -1
		}
		if a.Protocol > b.Protocol {
			return 1
		}
		return 0
	})
	return p
}
