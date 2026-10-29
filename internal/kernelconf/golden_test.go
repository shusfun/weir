package kernelconf

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

// 新旧对照：同一份 GateConfig，旧生成器（本包）与按内置驱动生成的结果必须完全一致。

func driverText(t *testing.T, name string) string {
	dir := filepath.Join("..", "kernels", "drivers")
	src := func(n string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(dir, n+".yaml"))
		return string(b), err == nil
	}
	text, _ := src(name)
	_, full, err := Resolve(text, src)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return full
}

// wgKey 是固定的 WireGuard 私钥（任意 32 字节都是有效的 X25519 私钥），保证输出可复现。
func wgKey(seed string) string {
	sum := sha256.Sum256([]byte("wg-" + seed))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func certs(t *testing.T) string {
	dir := t.TempDir()
	for _, id := range []string{"5", "6", "7"} {
		for _, ext := range []string{".crt", ".key"} {
			if err := os.WriteFile(filepath.Join(dir, id+ext), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func cred(id, inbound int64, uuid, pw string) contracts.Credential {
	return contracts.Credential{ID: id, InboundID: inbound, StatsKey: "c" + itoa(id), UserKey: "u" + itoa(id), UUID: uuid, Password: pw}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func scenarios(t *testing.T) map[string]contracts.GateConfig {
	ports := contracts.PortPlan{XrayStats: 21001, SingboxStats: 21002}
	xrayIn := []contracts.Inbound{
		{ID: 1, Tag: "reality", Kernel: "xray", Protocol: "vless", Port: 24001, Network: "tcp", Security: "reality", Settings: map[string]any{"reality_dest": "www.microsoft.com:443", "reality_server_names": "www.microsoft.com, microsoft.com", "reality_private_key": "kKB1Vv7m0wN3hOq1oQv0b9nN5mF3V6Q2l1c8pZ0yGXE", "reality_short_id": "a1b2c3d4", "tls_fingerprint": "chrome"}},
		{ID: 2, Tag: "reality-xhttp", Kernel: "xray", Protocol: "vless", Port: 24002, Network: "xhttp", Security: "reality", Settings: map[string]any{"xhttp_path": "/x", "xhttp_mode": "packet-up"}},
		{ID: 3, Tag: "vless-ws", Kernel: "xray", Protocol: "vless", Port: 24003, Network: "ws", Security: "tls", CertID: 5, Settings: map[string]any{"ws_path": "/abc", "ws_host": "h.example.com", "tls_alpn": "http/1.1", "tls_fingerprint": "firefox", "tls_sni": "s.example.com"}},
		{ID: 4, Tag: "vmess-ws", Kernel: "xray", Protocol: "vmess", Port: 24004, Network: "ws", Security: "tls", CertID: 6},
		{ID: 5, Tag: "trojan", Kernel: "xray", Protocol: "trojan", Port: 24005, Network: "tcp", Security: "tls", CertID: 7, Settings: map[string]any{"tls_alpn": "h2, http/1.1"}},
		{ID: 6, Tag: "grpc", Kernel: "xray", Protocol: "vless", Port: 24006, Network: "grpc", Security: "none"},
		{ID: 7, Tag: "grpc-named", Kernel: "xray", Protocol: "trojan", Port: 24007, Network: "grpc", Security: "", Settings: map[string]any{"grpc_service_name": "svc"}},
		{ID: 8, Tag: "httpupgrade", Kernel: "xray", Protocol: "vmess", Port: 24008, Network: "httpupgrade", Settings: map[string]any{"ws_host": "a.com,b.com"}},
		{ID: 9, Tag: "ss", Kernel: "xray", Protocol: "shadowsocks", Port: 24009, Network: "tcp"},
		{ID: 10, Tag: "socks", Kernel: "xray", Protocol: "socks", Port: 24010, Network: "tcp"},
		{ID: 11, Tag: "http", Kernel: "xray", Protocol: "http", Port: 24011},
		{ID: 12, Tag: "reality-default", Kernel: "xray", Protocol: "vless", Port: 24012, Network: "TCP", Security: "REALITY"},
	}
	var xrayCreds []contracts.Credential
	for _, in := range xrayIn {
		xrayCreds = append(xrayCreds, cred(in.ID*10+1, in.ID, "11111111-1111-4111-8111-11111111111"+itoa(in.ID%10), "pw-"+itoa(in.ID)), cred(in.ID*10+2, in.ID, "u"+itoa(in.ID), "pw2-"+itoa(in.ID)))
	}
	outs := []contracts.Outbound{
		{ID: 40, Tag: "socks-auth", Protocol: "socks", Address: "203.0.113.1", Port: 1080, Username: " user ", Password: "pw"},
		{ID: 41, Tag: "socks-plain", Protocol: "socks5", Address: "203.0.113.2", Port: 1081},
		{ID: 42, Tag: "http", Protocol: "http", Address: "203.0.113.3", Port: 8080, Username: "u"},
		{ID: 43, Tag: "https", Protocol: "https", Address: "203.0.113.4", Port: 443, Username: "u", Password: "p", Settings: map[string]any{"sni": "p.example.com"}},
		{ID: 44, Tag: "vmess", Protocol: "vmess", Address: "198.51.100.1", Port: 443, Username: "33333333-3333-4333-8333-333333333333", Settings: map[string]any{"network": "ws", "path": "/v", "host": "v.example.com", "security": "tls", "sni": "v.example.com", "fp": "chrome", "method": "aes-128-gcm"}},
		{ID: 45, Tag: "vless-reality", Protocol: "vless", Address: "198.51.100.2", Port: 443, Username: "44444444-4444-4444-8444-444444444444", Settings: map[string]any{"reality": true, "sni": "www.apple.com", "pbk": "PUBKEY", "sid": "ab", "fp": "safari", "flow": "xtls-rprx-vision"}},
		{ID: 46, Tag: "trojan-grpc", Protocol: "trojan", Address: "198.51.100.3", Port: 443, Password: "tp", Settings: map[string]any{"network": "grpc", "service_name": "g", "tls": true, "alpn": []any{"h2"}, "allow_insecure": true, "trusted_certificate": "-----BEGIN-----\nAAA\n-----END-----"}},
		{ID: 47, Tag: "trojan-ws", Protocol: "trojan", Address: "198.51.100.4", Port: 443, Password: " tw ", Settings: map[string]any{"network": "websocket", "path": "/w"}},
		{ID: 48, Tag: "ss", Protocol: "shadowsocks", Address: "198.51.100.5", Port: 8388, Username: "chacha20-ietf-poly1305", Password: "sspw"},
		{ID: 49, Tag: "block", Protocol: "block"},
		{ID: 50, Tag: "dns", Protocol: "dns"},
	}
	singOuts := append(append([]contracts.Outbound{}, outs...),
		contracts.Outbound{ID: 60, Tag: "hy2", Protocol: "hysteria2", Address: "192.0.2.1", Port: 443, Password: "hp", Settings: map[string]any{"sni": "hy.example.com", "obfs": "salamander", "obfs_password": "op"}},
		contracts.Outbound{ID: 61, Tag: "tuic", Protocol: "tuic", Address: "192.0.2.2", Port: 443, Username: "55555555-5555-4555-8555-555555555555", Password: "tp", Settings: map[string]any{"congestion_control": "bbr", "zero_rtt": true, "alpn": []any{"h3"}}},
		contracts.Outbound{ID: 62, Tag: "stls", Protocol: "shadowtls", Address: "192.0.2.3", Port: 443, Password: "sp", Settings: map[string]any{"sni": "www.microsoft.com"}},
		contracts.Outbound{ID: 63, Tag: "anytls", Protocol: "anytls", Address: "192.0.2.4", Port: 443, Password: "ap"},
	)
	routes := []contracts.Route{
		{ID: 1, InboundTag: "reality", StatsKey: "c11", OutboundTag: "socks-auth", Priority: 1},
		{ID: 2, StatsKey: "c102", OutboundTag: "http", Priority: 2},
		{ID: 3, Domain: "a.com, b.com\nc.com", OutboundTag: "vmess", Priority: 3},
		{ID: 4, IP: "10.0.0.0/8,1.1.1.1", OutboundTag: "block", Priority: 4},
		{ID: 5, InboundTag: "trojan", OutboundTag: "direct", Priority: 5},
		{ID: 6, Domain: "x.com", OutboundTag: "trojan-grpc", Priority: 0},
	}
	sbIn := []contracts.Inbound{
		{ID: 20, Tag: "hy2", Kernel: "sing-box", Protocol: "hysteria2", Port: 25001, Security: "tls", CertID: 5, Settings: map[string]any{"tls_sni": "hy.example.com", "hy2_obfs": "salamander", "hy2_obfs_password": "obfs", "hy2_up_mbps": 100, "hy2_down_mbps": 200}},
		{ID: 21, Tag: "hy2-plain", Kernel: "sing-box", Protocol: "hysteria2", Port: 25002, Security: "tls", CertID: 6},
		{ID: 22, Tag: "tuic", Kernel: "sing-box", Protocol: "tuic", Port: 25003, Security: "tls", CertID: 7, Settings: map[string]any{"tuic_congestion_control": "cubic", "tuic_zero_rtt": true, "tls_alpn": "h3,h3-29", "tls_sni": "t.example.com"}},
		{ID: 23, Tag: "tuic-default", Kernel: "sing-box", Protocol: "tuic", Port: 25004, Security: "tls", CertID: 5},
		{ID: 24, Tag: "ss", Kernel: "sing-box", Protocol: "shadowsocks", Port: 25005, Settings: map[string]any{"ss_method": "2022-blake3-aes-128-gcm", "ss_password": "c2VydmVyLWtleS0xNi1iMDE="}},
		{ID: 25, Tag: "stls", Kernel: "sing-box", Protocol: "shadowtls", Port: 25006, Settings: map[string]any{"tls_sni": "www.microsoft.com", "ss_password": "c2VydmVyLWtleS0xNi1iMDE=", "shadowtls_version": 3}},
		{ID: 26, Tag: "stls-old", Kernel: "sing-box", Protocol: "shadowtls", Port: 25007, Settings: map[string]any{"shadowtls_version": 2}},
		{ID: 27, Tag: "wg", Kernel: "sing-box", Protocol: "wireguard", Port: 25008, Network: "udp", Settings: map[string]any{"wg_private_key": wgKey("server"), "wg_mtu": 1380}},
		{ID: 28, Tag: "wg-nokey", Kernel: "sing-box", Protocol: "wireguard", Port: 25009},
	}
	var sbCreds []contracts.Credential
	for _, in := range sbIn {
		pw := "pw-" + itoa(in.ID)
		if in.Protocol == "wireguard" {
			pw = wgKey(itoa(in.ID))
		}
		sbCreds = append(sbCreds, cred(in.ID*10+1, in.ID, "not-a-uuid-"+itoa(in.ID), pw))
	}
	sbCreds = append(sbCreds, cred(271*1+1000, 27, "", "not-base64!"))
	sbRoutes := []contracts.Route{
		{ID: 11, InboundTag: "hy2", OutboundTag: "hy2", Priority: 1},
		{ID: 12, StatsKey: "c221", OutboundTag: "tuic", Priority: 2},
		{ID: 13, Domain: "s.com", IP: "8.8.8.8", OutboundTag: "socks-auth", Priority: 3},
	}
	group := contracts.Outbound{ID: 70, Tag: "pool", Protocol: "group", Members: []contracts.Outbound{
		{Protocol: "trojan", Address: "198.51.100.10", Port: 443, Password: "a", Settings: map[string]any{"security": "tls", "tls": true, "sni": "a.example.com"}},
		{Protocol: "vless", Address: "198.51.100.11", Port: 443, Username: "66666666-6666-4666-8666-666666666666", Settings: map[string]any{"reality": true, "pbk": "K", "sid": "1"}},
	}}
	mixedIn := append(append([]contracts.Inbound{}, xrayIn[0], xrayIn[4]), sbIn[0], sbIn[4])
	var mixedCreds []contracts.Credential
	for _, c := range append(append([]contracts.Credential{}, xrayCreds...), sbCreds...) {
		for _, in := range mixedIn {
			if c.InboundID == in.ID {
				mixedCreds = append(mixedCreds, c)
			}
		}
	}
	return map[string]contracts.GateConfig{
		"xray-all":        {Ports: ports, Inbounds: xrayIn, Credentials: xrayCreds, Outbounds: outs, Routes: routes},
		"xray-noroute":    {Ports: ports, Inbounds: xrayIn[:3], Credentials: xrayCreds[:6]},
		"singbox-all":     {Ports: ports, Inbounds: sbIn, Credentials: sbCreds, Outbounds: singOuts, Routes: sbRoutes},
		"singbox-noroute": {Ports: ports, Inbounds: sbIn[4:6], Credentials: sbCreds[4:6]},
		"mixed-group": {Ports: ports, Inbounds: mixedIn, Credentials: mixedCreds, Outbounds: append(append([]contracts.Outbound{}, outs[:3]...), group),
			Routes: []contracts.Route{{ID: 1, Domain: "g.com", OutboundTag: "pool", Priority: 1}, {ID: 2, InboundTag: "reality", OutboundTag: "socks-auth", Priority: 2}, {ID: 3, StatsKey: "c201", OutboundTag: "pool", Priority: 3}}},
	}
}

// 对照文件来自改为驱动之前的生成器（同一批场景的输出）；证书目录写成 /weir-test-certs。
func golden(t *testing.T, name, kernel string, got []byte, dir string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+"-"+kernel+".json")
	want, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if got != nil {
			t.Fatalf("%s: unexpected %s config", name, kernel)
		}
		return
	}
	if got == nil {
		t.Fatalf("%s: missing %s config", name, kernel)
	}
	var x, y any
	_ = json.Unmarshal(want, &x)
	_ = json.Unmarshal([]byte(strings.ReplaceAll(string(got), dir, "/weir-test-certs")), &y)
	if !reflect.DeepEqual(x, y) {
		out := filepath.Join(os.TempDir(), "weir-"+name+"-"+kernel+".json")
		_ = os.WriteFile(out, got, 0o644)
		t.Fatalf("%s/%s differs from %s; got %s", name, kernel, path, out)
	}
}

func TestDriversMatchGolden(t *testing.T) {
	dir := certs(t)
	for _, xray := range []string{"xray-1.8", "xray-26.3"} {
		for name, cfg := range scenarios(t) {
			cfg.Drivers = map[string]string{"xray": driverText(t, xray), "sing-box": driverText(t, "sing-box-1.12")}
			got, err := Build(cfg, Options{CertDir: dir})
			if err != nil {
				t.Fatalf("%s %s: %v", xray, name, err)
			}
			golden(t, xray+"-"+name, "xray", got.Xray, dir)
			golden(t, xray+"-"+name, "sing-box", got.Singbox, dir)
		}
	}
	// 两边都拒绝的情况：拒绝原因对应。
	cfg := scenarios(t)["xray-noroute"]
	cfg.Inbounds = append(cfg.Inbounds, contracts.Inbound{ID: 99, Tag: "h2", Kernel: "xray", Protocol: "vmess", Port: 24099, Network: "h2"})
	cfg.Drivers = map[string]string{"xray": driverText(t, "xray-26.3")}
	if _, err := Build(cfg, Options{CertDir: dir}); err == nil || !strings.Contains(err.Error(), "不支持传输 h2") {
		t.Fatalf("unsupported transport: %v", err)
	}
	cfg = scenarios(t)["xray-all"]
	cfg.Outbounds = append(cfg.Outbounds, contracts.Outbound{ID: 98, Tag: "nopw", Protocol: "trojan", Address: "203.0.113.9", Port: 443})
	cfg.Drivers = map[string]string{"xray": driverText(t, "xray-26.3")}
	if _, err := Build(cfg, Options{CertDir: dir}); err == nil || !strings.Contains(err.Error(), "requires password") {
		t.Fatalf("trojan without password: %v", err)
	}
	if !strings.Contains(driverText(t, "xray-26.10"), "MLKEM") {
		t.Fatal("xray-26.10 must carry the compat note")
	}
}

// 继承的驱动写错字段时，错误里的行号要对应原文，而不是展开后重新编码的全文。
func TestResolveReportsOriginalLine(t *testing.T) {
	dir := filepath.Join("..", "kernels", "drivers")
	src := func(n string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(dir, n+".yaml"))
		return string(b), err == nil
	}
	text := "kernel: xray\nname: xray-test\nextends: xray-1.8\nmin_version: 26.3.27\nbogus_field: 1\n"
	_, _, err := Resolve(text, src)
	if err == nil || !strings.Contains(err.Error(), "line 5") || !strings.Contains(err.Error(), "bogus_field") {
		t.Fatalf("want error at line 5 naming bogus_field, got %v", err)
	}
}
