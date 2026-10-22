package kernelconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imzyb/MiGate/packages/contracts"
)

func sampleConfig(t *testing.T) contracts.GateConfig {
	t.Helper()
	past := time.Now().Add(-time.Hour)
	return contracts.GateConfig{
		Revision: 7,
		Ports:    contracts.PortPlan{XrayStats: 21001, SingboxStats: 21002},
		Inbounds: []contracts.Inbound{{
			ID: 1, Tag: "hk-reality", Kernel: "xray", Protocol: "vless", Port: 24443, Network: "tcp", Security: "reality",
			Settings: map[string]any{"reality_dest": "www.microsoft.com:443", "reality_server_names": "www.microsoft.com", "reality_private_key": "kKB1Vv7m0wN3hOq1oQv0b9nN5mF3V6Q2l1c8pZ0yGXE", "reality_short_id": "a1b2c3d4"},
		}},
		Outbounds: []contracts.Outbound{{ID: 9, Tag: "home", Protocol: "socks", Address: "203.0.113.10", Port: 1080, Username: "u", Password: "p"}},
		Credentials: []contracts.Credential{
			{ID: 11, InboundID: 1, StatsKey: "c11", UserKey: "u1", UUID: "11111111-1111-4111-8111-111111111111"},
			{ID: 12, InboundID: 1, StatsKey: "c12", UserKey: "u2", UUID: "22222222-2222-4222-8222-222222222222"},
			{ID: 13, InboundID: 1, StatsKey: "c13", UserKey: "u3", UUID: "33333333-3333-4333-8333-333333333333", ExpiresAt: &past},
			{ID: 14, InboundID: 1, StatsKey: "c14", UserKey: "u2", UUID: "44444444-4444-4444-8444-444444444444", Internal: true},
		},
		Routes: []contracts.Route{
			{ID: 2, InboundTag: "hk-reality", StatsKey: "c11", OutboundTag: "home", Priority: 1},
			{ID: 3, StatsKey: "c12", OutboundTag: "direct", Priority: 2},
		},
	}
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, b)
	}
	return doc
}

func TestBuildXrayFiltersCredentialsAndRoutes(t *testing.T) {
	cfg := sampleConfig(t)
	out, err := Build(cfg, Options{CertDir: t.TempDir(), Exhausted: map[string]bool{"u2": true}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Xray == nil || out.Singbox != nil {
		t.Fatalf("xray=%v singbox=%v", out.Xray != nil, out.Singbox != nil)
	}
	text := string(out.Xray)
	// u1 正常；u2 额度用尽被剔除；u3 已到期被剔除；c14 是内部凭据不受额度影响。
	for key, want := range map[string]bool{"11111111-1111-4111-8111-111111111111": true, "22222222-2222-4222-8222-222222222222": false, "33333333-3333-4333-8333-333333333333": false, "44444444-4444-4444-8444-444444444444": true} {
		if strings.Contains(text, key) != want {
			t.Fatalf("credential %s present=%v, want %v", key, !want, want)
		}
	}
	doc := decode(t, out.Xray)
	apiPort := 0
	for _, in := range doc["inbounds"].([]any) {
		m := in.(map[string]any)
		if m["tag"] == "api" {
			apiPort = int(m["port"].(float64))
		}
	}
	if apiPort != 21001 {
		t.Fatalf("api port = %d", apiPort)
	}
	if !strings.Contains(text, "203.0.113.10") || !strings.Contains(text, `"c11"`) {
		t.Fatalf("route to home outbound for c11 missing:\n%s", text)
	}
	if strings.Contains(text, `"c12"`) {
		t.Fatal("route for exhausted credential must be skipped")
	}
}

func TestBuildRejectsMissingReferences(t *testing.T) {
	cfg := sampleConfig(t)
	cfg.Routes = append(cfg.Routes, contracts.Route{ID: 4, OutboundTag: "nowhere"})
	if _, err := Build(cfg, Options{}); err == nil || !strings.Contains(err.Error(), "missing outbound") {
		t.Fatalf("err = %v", err)
	}
	cfg = sampleConfig(t)
	cfg.Routes = []contracts.Route{{ID: 5, StatsKey: "ghost", OutboundTag: "direct"}}
	if _, err := Build(cfg, Options{}); err == nil {
		t.Fatal("route to unknown credential accepted")
	}
	cfg = sampleConfig(t)
	cfg.Ports.XrayStats = 0
	if _, err := Build(cfg, Options{}); err == nil {
		t.Fatal("missing stats port accepted")
	}
}

func TestProfileValidatesAndStrips(t *testing.T) {
	cfg := sampleConfig(t)
	cfg.Profiles = map[string]string{"xray": "kernel: xray\nmin_version: 26.3.27\nprotocols: [vless, vmess, trojan, shadowsocks]\nnetworks: [tcp, ws, grpc, xhttp]\nsecurities: [tls, reality]\nstrip: [policy.levels.*.statsUserDownlink]\n"}
	out, err := Build(cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out.Xray), "statsUserDownlink") {
		t.Fatal("strip path not applied")
	}
	if !strings.Contains(string(out.Xray), "statsUserUplink") {
		t.Fatal("strip removed too much")
	}
	cfg.Profiles["xray"] = "kernel: xray\nmin_version: 26.3.27\nprotocols: [trojan]\n"
	if _, err = Build(cfg, Options{}); err == nil || !strings.Contains(err.Error(), "不支持协议 vless") {
		t.Fatalf("err = %v", err)
	}
	cfg.Profiles["xray"] = "kernel: xray\nmin_version: 1\nprotocols: [vless]\nunknown_field: 1\n"
	if _, err = Build(cfg, Options{}); err == nil {
		t.Fatal("unknown profile field accepted")
	}
}

func TestBuildSingboxHysteria2WithManagedCert(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"5.crt", "5.key"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := contracts.GateConfig{
		Ports:       contracts.PortPlan{XrayStats: 21001, SingboxStats: 21002},
		Inbounds:    []contracts.Inbound{{ID: 2, Tag: "hy2", Kernel: "sing-box", Protocol: "hysteria2", Port: 24444, Security: "tls", CertID: 5, Settings: map[string]any{"tls_sni": "hy.example.test"}}},
		Credentials: []contracts.Credential{{ID: 21, InboundID: 2, StatsKey: "c21", UserKey: "u1", Password: "secret-21"}},
	}
	out, err := Build(cfg, Options{CertDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if out.Xray != nil || out.Singbox == nil {
		t.Fatalf("xray=%v singbox=%v", out.Xray != nil, out.Singbox != nil)
	}
	doc := decode(t, out.Singbox)
	api := doc["experimental"].(map[string]any)["v2ray_api"].(map[string]any)
	if api["listen"] != "127.0.0.1:21002" {
		t.Fatalf("stats listen = %v", api["listen"])
	}
	inbound := doc["inbounds"].([]any)[0].(map[string]any)
	if got := inbound["tls"].(map[string]any)["certificate_path"]; got != filepath.Join(dir, "5.crt") {
		t.Fatalf("certificate_path = %v", got)
	}
	// 该入站没有有效凭据时整个 sing-box 配置不生成（sing-box 不接受空用户列表）。
	cfg.Credentials[0].ExpiresAt = func() *time.Time { v := time.Now().Add(-time.Minute); return &v }()
	if out, err = Build(cfg, Options{CertDir: dir}); err != nil || out.Singbox != nil {
		t.Fatalf("expired-only inbound: singbox=%v err=%v", out.Singbox != nil, err)
	}
}
