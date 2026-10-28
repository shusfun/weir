package center

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/internal/subscription"
)

func TestNewPresetsAdvancedFieldsAndChosenUsers(t *testing.T) {
	_, admin, g := setupGate(t)
	gid := int(g.id)
	// 一个不限额度、一个有额度、一个只有部分节点的用户。
	free := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "free@example.test"}, 200)
	limited := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "limited@example.test", "quota": 10 << 30}, 200)
	picky := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "picky@example.test", "all_nodes": false, "node_ids": []int{}}, 200)
	ids := []int{int(free["id"].(float64)), int(limited["id"].(float64)), int(picky["id"].(float64))}

	// 高级字段：只能改方案列出的字段。
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "trojan-tls", "settings": map[string]any{"reality_private_key": "x"}, "skip_probe": true}, 400)
	hy := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "hysteria2", "settings": map[string]any{"hy2_up_mbps": 100, "hy2_obfs": ""}, "skip_probe": true, "user_ids": ids}, 200)
	if st := hy["settings"].(map[string]any); st["hy2_up_mbps"].(float64) != 100 || st["hy2_obfs"] != nil || st["hy2_obfs_password"] != nil {
		t.Fatalf("hysteria2 advanced = %v", st)
	}
	// 第 ④ 步：勾选的用户都拿到链接；只选了部分节点的用户被加上这个节点。
	shares := hy["shares"].([]any)
	if len(shares) != 3 {
		t.Fatalf("shares = %v", shares)
	}
	for _, sh := range shares {
		m := sh.(map[string]any)
		links := m["links"].([]any)
		if len(links) != 1 || !strings.HasPrefix(links[0].(map[string]any)["uri"].(string), "hysteria2://") {
			t.Fatalf("share for %v = %v", m["email"], m)
		}
	}

	byPreset := map[string]map[string]any{}
	for _, preset := range []string{"socks-local", "http-local", "shadowtls", "wireguard"} {
		byPreset[preset] = mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": preset, "skip_probe": true, "user_ids": ids[:2]}, 200)
	}
	uri := func(preset string, i int) string {
		sh := byPreset[preset]["shares"].([]any)[i].(map[string]any)
		if l := sh["links"].([]any); len(l) > 0 {
			return l[0].(map[string]any)["uri"].(string)
		}
		return "error: " + sh["error"].(string)
	}
	if u := uri("socks-local", 0); !strings.HasPrefix(u, "socks5://u") {
		t.Fatalf("socks = %s", u)
	}
	if u := uri("http-local", 0); !strings.HasPrefix(u, "http://u") {
		t.Fatalf("http = %s", u)
	}
	stls := uri("shadowtls", 0)
	if !strings.HasPrefix(stls, "ss://") || !strings.Contains(stls, "shadow-tls") {
		t.Fatalf("shadowtls = %s", stls)
	}
	wg := uri("wireguard", 0)
	if !strings.HasPrefix(wg, "wireguard://") || !strings.Contains(wg, "address=10.77.") {
		t.Fatalf("wireguard = %s", wg)
	}
	// WireGuard 没有按用户的流量统计：有额度的用户拿不到，用户详情写明原因。
	if u := uri("wireguard", 1); !strings.Contains(u, "不限额度") {
		t.Fatalf("limited user wireguard = %s", u)
	}
	withheld := mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(ids[1]), nil, 200)["withheld"].([]any)
	if len(withheld) != 1 || !strings.Contains(withheld[0].(map[string]any)["reason"].(string), "不限额度") {
		t.Fatalf("withheld = %v", withheld)
	}
	if w := mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(ids[0]), nil, 200)["withheld"].([]any); len(w) != 0 {
		t.Fatalf("unlimited user withheld = %v", w)
	}

	// 自己生成的链接能被解析回来，并转换成 Clash / sing-box；Surge 订阅不含 ShadowTLS。
	links := []string{uri("socks-local", 0), uri("http-local", 0), stls, wg}
	parsed, err := subscription.ParseLinks(links)
	if err != nil || len(parsed.Nodes) != 4 || len(parsed.Skipped) != 0 {
		t.Fatalf("parse own links: %v %+v", err, parsed.Skipped)
	}
	clash, _, err := subout.Render(links, "clash")
	if err != nil || !strings.Contains(string(clash), "plugin: shadow-tls") || !strings.Contains(string(clash), "type: wireguard") {
		t.Fatalf("clash = %v\n%s", err, clash)
	}
	sb, _, err := subout.Render(links, "singbox")
	if err != nil || !strings.Contains(string(sb), `"type": "shadowtls"`) || !strings.Contains(string(sb), `"detour"`) || !strings.Contains(string(sb), `"endpoints"`) {
		t.Fatalf("sing-box = %v\n%s", err, sb)
	}
	surge, _, err := subout.Render(links[:3], "surge")
	if err != nil || strings.Contains(string(surge), "shadow-tls") {
		t.Fatalf("surge = %v\n%s", err, surge)
	}

	// Gate 下发：WireGuard 只有不限额度用户的 peer；ShadowTLS 带 Shadowsocks 服务端密钥。
	cfg := g.config(0)
	wgID, stID := int64(byPreset["wireguard"]["id"].(float64)), int64(byPreset["shadowtls"]["id"].(float64))
	peers := 0
	for _, c := range cfg.Credentials {
		if c.InboundID == wgID {
			peers++
			if raw, err := base64.StdEncoding.DecodeString(c.Password); err != nil || len(raw) != 32 {
				t.Fatalf("wireguard credential must be a private key: %q", c.Password)
			}
		}
	}
	if peers != 1 {
		t.Fatalf("wireguard peers = %d", peers)
	}
	for _, in := range cfg.Inbounds {
		if in.ID == stID && (in.Settings["ss_password"] == nil || in.Settings["tls_sni"] == nil) {
			t.Fatalf("shadowtls inbound settings = %v", in.Settings)
		}
	}
}
