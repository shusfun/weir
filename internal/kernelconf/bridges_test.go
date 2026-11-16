package kernelconf

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func bridgeConfig(t *testing.T, helper string) contracts.GateConfig {
	t.Helper()
	o := contracts.Outbound{ID: 10, Tag: "exit", Protocol: "anytls", Address: "exit.example.test", Port: 443, Password: "exit-password", Settings: map[string]any{"tls": true, "sni": "exit.example.test"}}
	if helper == "mihomo" {
		o = contracts.Outbound{ID: 10, Tag: "exit", Protocol: "snell", ClientOnly: true, Client: map[string]any{"type": "snell", "server": "exit.example.test", "port": 443, "psk": "exit-password", "version": 3}}
	}
	return contracts.GateConfig{
		Ports:       contracts.PortPlan{XrayStats: 21001, SingboxStats: 21002},
		Drivers:     map[string]string{"xray": driverText(t, "xray-26.3"), "sing-box": driverText(t, "sing-box-1.12"), "mihomo": driverText(t, "mihomo-1")},
		Inbounds:    []contracts.Inbound{{ID: 1, Tag: "entry", Kernel: "xray", Protocol: "socks", Network: "tcp", Port: 24001}},
		Credentials: []contracts.Credential{cred(1, 1, "entry-user", "entry-password")},
		Outbounds:   []contracts.Outbound{o},
		Routes:      []contracts.Route{{ID: 1, InboundTag: "entry", OutboundTag: "exit"}},
		Bridges:     []contracts.OutboundBridge{{FromKernel: "xray", OutboundTag: "exit", Kernel: helper, Port: 25001, Username: "bridge-user", Password: "bridge-password-at-least-24-characters"}},
	}
}

func TestOutboundBridgesPreserveTargetAndAuthenticateLoopback(t *testing.T) {
	for _, helper := range []string{"sing-box", "mihomo"} {
		t.Run(helper, func(t *testing.T) {
			cfg := bridgeConfig(t, helper)
			out, err := Build(cfg, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out.Xray), `"127.0.0.1"`) || !strings.Contains(string(out.Xray), `"bridge-user"`) || strings.Contains(string(out.Xray), "exit.example.test") {
				t.Fatalf("entry does not use isolated bridge: %s", out.Xray)
			}
			body := out.Singbox
			if helper == "mihomo" {
				body = out.Mihomo
				if len(out.Singbox) > 0 {
					t.Fatal("unneeded sing-box started")
				}
			}
			if !strings.Contains(string(body), "exit.example.test") || !strings.Contains(string(body), "exit-password") || !strings.Contains(string(body), "bridge-user") {
				t.Fatalf("lost target or auth: %s", body)
			}
			if cfg.Outbounds[0].Tag != "exit" || cfg.Outbounds[0].Client["name"] != nil {
				t.Fatal("mutated canonical target")
			}
		})
	}
}

func TestOutboundBridgeRejectsUnsafeRenderedRoute(t *testing.T) {
	for _, helper := range []string{"sing-box", "mihomo"} {
		cfg := bridgeConfig(t, helper)
		if helper == "sing-box" {
			cfg.Drivers[helper] = strings.ReplaceAll(cfg.Drivers[helper], `${r.outbound}`, `direct`)
		} else {
			cfg.Drivers[helper] = strings.ReplaceAll(cfg.Drivers[helper], `${bridge.target}`, `DIRECT`)
		}
		if _, err := Build(cfg, Options{}); err == nil {
			t.Fatal("accepted helper route to direct")
		}
	}
}

func TestOutboundBridgeRejectsPortCollisionAndCycles(t *testing.T) {
	for _, mutate := range []func(*contracts.OutboundBridge){
		func(b *contracts.OutboundBridge) { b.Port = 24001 },
		func(b *contracts.OutboundBridge) { b.Port = 21001 },
		func(b *contracts.OutboundBridge) { b.FromKernel = "sing-box" },
		func(b *contracts.OutboundBridge) { b.Password = "" },
		func(b *contracts.OutboundBridge) { b.OutboundTag = "missing" },
	} {
		cfg := bridgeConfig(t, "sing-box")
		mutate(&cfg.Bridges[0])
		if _, err := Build(cfg, Options{}); err == nil {
			t.Fatal("accepted unsafe bridge")
		}
	}
}

func TestAuxiliaryDriverCannotChangeMihomoCredentials(t *testing.T) {
	cfg := bridgeConfig(t, "mihomo")
	cfg.Drivers["mihomo"] = strings.ReplaceAll(cfg.Drivers["mihomo"], `${bridge.password}`, `substituted-password-at-least-24-characters`)
	if _, err := Build(cfg, Options{}); err == nil {
		t.Fatal("accepted modified authentication")
	}
}

func TestSingboxBridgeRejectsMissingTarget(t *testing.T) {
	cfg := bridgeConfig(t, "sing-box")
	expanded, _, _ := expandGroups(cfg)
	views, err := bridgeViews(expanded)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Build(cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(out.Singbox, &doc); err != nil {
		t.Fatal(err)
	}
	doc["outbounds"] = []any{map[string]any{"type": "direct", "tag": "weir-bridge-1-target"}}
	body, _ := json.Marshal(doc)
	if err = validateBridgeListeners(body, views); err == nil {
		t.Fatal("accepted direct as helper target")
	}
}
