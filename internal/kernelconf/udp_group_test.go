package kernelconf

import (
	"strings"
	"testing"
)

func TestGroupUDPUsesOnlySupportingMembers(t *testing.T) {
	cfg := groupConfig(t, []any{"balancer", "observatory"}, []any{"urltest"})
	cfg.Outbounds[len(cfg.Outbounds)-1].Members[0].Settings["udp"] = true
	cfg.Outbounds[len(cfg.Outbounds)-1].Members[1].Settings["udp"] = false
	out, err := Build(cfg, Options{CertDir: certDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	x := string(out.Xray)
	if !strings.Contains(x, "xray-out-30-udp") || !strings.Contains(x, `"network": "udp"`) {
		t.Fatalf("xray udp selector missing: %s", x)
	}
	sb := string(out.Singbox)
	if !strings.Contains(sb, "singbox-out-30-udp") || !strings.Contains(sb, `"network": "udp"`) {
		t.Fatalf("sing-box udp selector missing: %s", sb)
	}
}
