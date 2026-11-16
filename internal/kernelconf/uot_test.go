package kernelconf

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestUoTUsesNativeSingboxAndTCPOnlyReceiver(t *testing.T) {
	cfg := bridgeConfig(t, "sing-box")
	cfg.Outbounds[0] = contracts.Outbound{ID: 10, Tag: "exit", Protocol: "shadowsocks", Address: "exit.example.test", Port: 24445, Username: "2022-blake3-aes-128-gcm", Password: "AAAAAAAAAAAAAAAAAAAAAA==:BBBBBBBBBBBBBBBBBBBBBA==", Settings: map[string]any{"udp_over_tcp": true}}
	out, err := Build(cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out.Xray), "exit.example.test") || !strings.Contains(string(out.Singbox), `"udp_over_tcp"`) {
		t.Fatal("UoT bypassed native helper")
	}
	cfg.Bridges = nil
	if _, err = Build(cfg, Options{}); err == nil {
		t.Fatal("Xray silently discarded UoT")
	}
	cfg.Inbounds = []contracts.Inbound{{ID: 2, Tag: "receiver", Kernel: "sing-box", Protocol: "shadowsocks", Network: "tcp", Port: 24445, Settings: map[string]any{"ss_method": "2022-blake3-aes-128-gcm", "ss_password": "AAAAAAAAAAAAAAAAAAAAAA==", "tcp_only": true}}}
	cfg.Credentials = []contracts.Credential{{ID: 2, InboundID: 2, StatsKey: "internal-exit", UserKey: "internal", Internal: true, Password: "BBBBBBBBBBBBBBBBBBBBBA=="}}
	cfg.Outbounds = nil
	cfg.Routes = nil
	out, err = Build(cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Inbounds []struct {
			Type    string `json:"type"`
			Network string `json:"network"`
		}
	}
	if err = json.Unmarshal(out.Singbox, &doc); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, in := range doc.Inbounds {
		if in.Type == "shadowsocks" {
			found = true
			if in.Network != "tcp" {
				t.Fatal("receiver exposed native UDP")
			}
		}
	}
	if !found {
		t.Fatal("receiver absent")
	}
	firewall := contracts.InboundFirewall(cfg.Inbounds)
	if len(firewall.Ports) != 1 || firewall.Ports[0].Protocol != "tcp" {
		t.Fatal("outer UDP unnecessarily opened")
	}
	cfg.Drivers["sing-box"] = strings.ReplaceAll(cfg.Drivers["sing-box"], "udp-over-tcp", "no-uot")
	if _, err = Build(cfg, Options{}); err == nil {
		t.Fatal("unsupported driver accepted internal transport")
	}
}
