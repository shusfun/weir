package kernelconf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func groupConfig(t *testing.T, xrayFeatures, singFeatures []any) contracts.GateConfig {
	cfg := sampleConfig(t)
	cfg.Inbounds = append(cfg.Inbounds, contracts.Inbound{ID: 2, Tag: "hy2", Kernel: "sing-box", Protocol: "hysteria2", Port: 24444, Security: "tls", CertID: 5, Settings: map[string]any{"tls_sni": "hy.example.test"}})
	cfg.Credentials = append(cfg.Credentials, contracts.Credential{ID: 21, InboundID: 2, StatsKey: "c21", UserKey: "u1", Password: "pw21"})
	cfg.Outbounds = append(cfg.Outbounds, contracts.Outbound{ID: 30, Tag: "hk-pool", Protocol: "group", Members: []contracts.Outbound{
		{Protocol: "trojan", Address: "198.51.100.10", Port: 443, Password: "a", Settings: map[string]any{"security": "tls", "tls": true, "sni": "a.example.com"}},
		{Protocol: "trojan", Address: "198.51.100.11", Port: 443, Password: "b", Settings: map[string]any{"security": "tls", "tls": true, "sni": "b.example.com"}},
	}})
	cfg.Routes = append(cfg.Routes, contracts.Route{ID: 4, Domain: "example.org", OutboundTag: "hk-pool", Priority: 3})
	feature := func(text string, list []any) string {
		return withDriver(t, text, func(m map[string]any) { m["features"] = list })
	}
	cfg.Drivers["xray"] = feature(cfg.Drivers["xray"], xrayFeatures)
	cfg.Drivers["sing-box"] = feature(cfg.Drivers["sing-box"], singFeatures)
	return cfg
}

// certDir 准备 sing-box TLS 入站要用的证书文件（内容不校验）。
func certDir(t *testing.T) string {
	dir := t.TempDir()
	for _, name := range []string{"5.crt", "5.key"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestGroupBecomesBalancerAndURLTest(t *testing.T) {
	out, err := Build(groupConfig(t, []any{"balancer", "observatory"}, []any{"urltest"}), Options{CertDir: certDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	x := decode(t, out.Xray)
	routing := x["routing"].(map[string]any)
	bal := routing["balancers"].([]any)[0].(map[string]any)
	if bal["tag"] != "xray-out-30" || len(bal["selector"].([]any)) != 2 || bal["strategy"].(map[string]any)["type"] != "leastPing" {
		t.Fatalf("balancer = %v", bal)
	}
	obs := x["observatory"].(map[string]any)
	if len(obs["subjectSelector"].([]any)) != 2 || obs["probeUrl"] != contracts.GroupProbeURL {
		t.Fatalf("observatory = %v", obs)
	}
	ruled := false
	for _, r := range routing["rules"].([]any) {
		if m := r.(map[string]any); m["balancerTag"] == "xray-out-30" && m["outboundTag"] == nil {
			ruled = true
		}
	}
	if !ruled {
		t.Fatalf("rule must target the balancer: %v", routing["rules"])
	}
	for _, o := range x["outbounds"].([]any) {
		if o.(map[string]any)["tag"] == "xray-out-30" {
			t.Fatal("the placeholder must be replaced by the balancer")
		}
	}
	sb := string(out.Singbox)
	m1, m2 := `"singbox-out-1073741824"`, `"singbox-out-1073741825"`
	if !strings.Contains(sb, `"type": "urltest"`) || !strings.Contains(sb, m1) || !strings.Contains(sb, m2) {
		t.Fatalf("sing-box = %s", sb)
	}
	doc := decode(t, out.Singbox)
	for _, o := range doc["outbounds"].([]any) {
		if m := o.(map[string]any); m["tag"] == "singbox-out-30" && m["type"] != "urltest" {
			t.Fatalf("sing-box placeholder must be replaced: %v", m)
		}
	}
}

func TestGroupNeedsKernelFeatures(t *testing.T) {
	// 驱动没写 features：不支持，报错，不悄悄退回单个节点。
	if _, err := Build(groupConfig(t, nil, []any{"urltest"}), Options{CertDir: certDir(t)}); err == nil || !strings.Contains(err.Error(), "不支持出口自动切换") {
		t.Fatalf("xray without features = %v", err)
	}
	if _, err := Build(groupConfig(t, []any{"balancer", "observatory"}, []any{}), Options{CertDir: certDir(t)}); err == nil || !strings.Contains(err.Error(), "urltest") {
		t.Fatalf("sing-box without urltest = %v", err)
	}
	cfg := groupConfig(t, []any{"balancer", "observatory"}, []any{"urltest"})
	cfg.Outbounds[len(cfg.Outbounds)-1].Members = make([]contracts.Outbound, contracts.MaxGroupMembers+1)
	if _, err := Build(cfg, Options{CertDir: certDir(t)}); err == nil {
		t.Fatal("more than 50 members must be rejected")
	}
}
