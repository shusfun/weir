package kernelconf

import (
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestSingboxRejectTagMatchesInbound(t *testing.T) {
	tag := rejectInboundTag("sing-box", contracts.Inbound{ID: 7, Protocol: "shadowsocks", Tag: "node-7"})
	if tag != "inbound-7-shadowsocks" {
		t.Fatalf("sing-box reject tag = %s", tag)
	}
	if rejectInboundTag("xray", contracts.Inbound{ID: 7, Protocol: "vless", Tag: "node-7"}) != "inbound-7-vless" {
		t.Fatal("xray reject tag must match the rendered inbound tag, not node-7")
	}
}

func TestRejectUDPStopsShadowsocksListen(t *testing.T) {
	sb := string(rejectUDP([]byte(`{"inbounds":[{"type":"shadowsocks","tag":"inbound-7-shadowsocks"}],"route":{"rules":[]}}`), "sing-box", []string{"inbound-7-shadowsocks"}))
	if !strings.Contains(sb, `"network":"tcp"`) || !strings.Contains(sb, `"action":"reject"`) {
		t.Fatalf("sing-box shadowsocks still listens for UDP: %s", sb)
	}
	x := string(rejectUDP([]byte(`{"inbounds":[{"protocol":"shadowsocks","tag":"inbound-7-shadowsocks","settings":{}}],"routing":{"rules":[]},"outbounds":[]}`), "xray", []string{"inbound-7-shadowsocks"}))
	if !strings.Contains(x, `"network":"tcp"`) || !strings.Contains(x, `"protocol":"blackhole"`) {
		t.Fatalf("xray shadowsocks still listens for UDP: %s", x)
	}
}

func TestRejectUDPFailsFastInsteadOfDropping(t *testing.T) {
	in := []byte(`{"inbounds":[{"tag":"in"}],"routing":{"rules":[]},"outbounds":[{"tag":"direct","protocol":"freedom"}]}`)
	out := string(rejectUDP(in, "xray", []string{"in"}))
	if !strings.Contains(out, `"outboundTag":"udp-reject"`) || !strings.Contains(out, `"protocol":"blackhole"`) || strings.Contains(out, `"port":1`) {
		t.Fatalf("xray udp reject = %s", out)
	}
	sb := string(rejectUDP(in, "sing-box", []string{"in"}))
	if !strings.Contains(sb, `"action":"reject"`) || !strings.Contains(sb, `"network":"udp"`) {
		t.Fatalf("sing-box udp reject = %s", sb)
	}
}
