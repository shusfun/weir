package kernelconf

import (
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestRuleMeterMapsMembersAndOnlyConsumer(t *testing.T) {
	cfg := contracts.GateConfig{
		Inbounds:  []contracts.Inbound{{ID: 1, Tag: "entry", Kernel: "xray"}, {ID: 2, Tag: "other", Kernel: "sing-box"}},
		Outbounds: []contracts.Outbound{{ID: 1 << 40, RuleID: 42, Tag: "rule-42", Protocol: "group", Members: []contracts.Outbound{{Protocol: "socks"}, {Protocol: "socks"}}}},
		Routes:    []contracts.Route{{InboundTag: "entry", OutboundTag: "rule-42"}},
	}
	m := RuleOutboundNames(cfg)
	if len(m) != 1 || len(m["xray"]) != 2 || len(m["sing-box"]) != 0 {
		t.Fatalf("helper or group counted twice: %v", m)
	}
	for _, id := range m["xray"] {
		if id != 42 {
			t.Fatal("lost member attribution")
		}
	}
}
