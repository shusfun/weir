package center

import (
	"strings"
	"sync"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

const ss2022Link = "ss://2022-blake3-aes-128-gcm:AAAAAAAAAAAAAAAAAAAAAA%3D%3D@203.0.113.20:8388#SS-HK"

// 单条分享链接作出口：能表达的协议都转成出口；订阅地址、跳过证书校验、证书指纹一律明确拒绝。
func TestParseExitLink(t *testing.T) {
	for raw, want := range map[string]string{
		ss2022Link: "shadowsocks",
		"hysteria2://pw@203.0.113.21:443?sni=a.example.com&obfs=salamander&obfs-password=x#HY":                                               "hysteria2",
		"vless://11111111-1111-4111-8111-111111111111@203.0.113.22:443?type=xhttp&security=tls&sni=b.example.com&path=%2Fx&mode=packet-up#X": "vless",
		"socks5://u:p@203.0.113.23:1080": "socks",
		"https://u:p@203.0.113.24:8443":  "https",
	} {
		o, _, err := parseExitLink(raw)
		if err != nil || o.Protocol != want {
			t.Fatalf("%s = %+v %v", raw, o, err)
		}
		if want == "vless" && (o.Settings["path"] != "/x" || o.Settings["mode"] != "packet-up") {
			t.Fatalf("xhttp settings lost: %v", o.Settings)
		}
		if want == "shadowsocks" && (o.Username != "2022-blake3-aes-128-gcm" || o.Password == "") {
			t.Fatalf("ss method/password = %+v", o)
		}
	}
	for raw, why := range map[string]string{
		"https://sub.example.com/api/v1/client/subscribe?token=x":        "订阅地址",
		"hysteria2://pw@203.0.113.21:443?sni=a.example.com&insecure=1":   "跳过证书校验",
		"hysteria2://pw@203.0.113.21:443?sni=a.example.com&pinSHA256=ab": "证书指纹",
		"not a link": "无法识别",
	} {
		if _, _, err := parseExitLink(raw); err == nil || !strings.Contains(err.Error(), why) {
			t.Fatalf("%s: err = %v, want %s", raw, err, why)
		}
	}
}

// 用户自建链式代理的出口：HTTP / HTTPS 代理、单条分享链接都能用，按入口内核检查，实测结果（实际出口 IP）记在链上。
func TestUserChainExitKinds(t *testing.T) {
	s, admin, g := setupGate(t)
	gid := int(g.id)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "skip_probe": true}, 200)
	entry := int(n["id"].(float64))
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "exit@example.test", "allow_chain": true, "all_nodes": true}, 200)
	user := login(t, s, "exit@example.test", u["password"].(string))
	var mu sync.Mutex
	var seen []map[string]any
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionChainProbe: func(a contracts.Action) contracts.ActionResult {
			mu.Lock()
			seen = append(seen, a.Payload)
			mu.Unlock()
			return contracts.ActionResult{OK: true, Result: map[string]any{"mode": "proxy", "latency_ms": 42, "exit_ip": "198.51.100.77"}}
		},
	})
	post := func(exit map[string]any, want int) map[string]any {
		return mustJSON(t, user, "POST", "/api/v1/me/chains", map[string]any{"name": "出口", "entry_node_id": entry, "exit": exit}, want)
	}
	ch := post(map[string]any{"kind": "https", "address": "203.0.113.30", "port": 8443, "username": "u", "password": "p"}, 200)
	if p, _ := ch["probe"].(map[string]any); p["exit_ip"] != "198.51.100.77" || p["mode"] != "proxy" {
		t.Fatalf("probe = %v", ch["probe"])
	}
	post(map[string]any{"kind": "link", "link": ss2022Link}, 200)
	// Xray 入口连不了 Hysteria2：保存前就说明原因，不建一个下发失败的链。
	if w := user.do("POST", "/api/v1/me/chains", map[string]any{"name": "x", "entry_node_id": entry, "exit": map[string]any{"kind": "link", "link": "hysteria2://pw@203.0.113.21:443?sni=a.example.com#HY"}}, nil); w.Code != 422 || !strings.Contains(w.Body.String(), "exit_unsupported") {
		t.Fatalf("hy2 on xray = %d %s", w.Code, w.Body.String())
	}
	if w := user.do("POST", "/api/v1/me/chains", map[string]any{"name": "x", "entry_node_id": entry, "exit": map[string]any{"kind": "link", "link": "https://sub.example.com/link/abc?sub=1"}}, nil); w.Code != 422 || !strings.Contains(w.Body.String(), "订阅地址") {
		t.Fatalf("subscription url = %d %s", w.Code, w.Body.String())
	}
	if w := user.do("POST", "/api/v1/me/chains", map[string]any{"name": "x", "entry_node_id": entry, "exit": map[string]any{"kind": "http", "address": "192.168.1.2", "port": 3128}}, nil); w.Code != 422 || !strings.Contains(w.Body.String(), "exit_not_public") {
		t.Fatalf("private http = %d %s", w.Code, w.Body.String())
	}
	mu.Lock()
	first := seen[0]
	mu.Unlock()
	if first["client"] == nil || !strings.HasSuffix(first["target"].(string), "/api/v1/ip") && first["target"] != "https://www.gstatic.com/generate_204" {
		t.Fatalf("probe payload = %v", first)
	}
	byKind := map[string]contracts.Outbound{}
	for _, ob := range g.config(0).Outbounds {
		if strings.HasPrefix(ob.Tag, "chain-") {
			byKind[ob.Protocol] = ob
		}
	}
	if o := byKind["https"]; o.Address != "203.0.113.30" || o.Password != "p" || o.Settings["security"] != "tls" {
		t.Fatalf("https exit = %+v", o)
	}
	if o := byKind["shadowsocks"]; o.Address != "203.0.113.20" || o.Username != "2022-blake3-aes-128-gcm" {
		t.Fatalf("link exit = %+v", o)
	}
}

// 没有公网地址时不建节点（建出来也生成不了链接）；节点视图列出要放行的端口并对照本机防火墙。
func TestNodeNeedsPublicHostAndShowsPorts(t *testing.T) {
	_, admin, g := setupGate(t)
	gid := itoa(int(g.id))
	mustJSON(t, admin, "PATCH", "/api/v1/admin/gates/"+gid, map[string]any{"public_host": ""}, 200)
	if w := admin.do("POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, nil); w.Code != 422 || !strings.Contains(w.Body.String(), "gate_no_public_host") {
		t.Fatalf("no public host = %d %s", w.Code, w.Body.String())
	}
	mustJSON(t, admin, "PATCH", "/api/v1/admin/gates/"+gid, map[string]any{"public_host": "203.0.113.40"}, 200)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	port := int(n["port"].(float64))
	g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Version: "test", Arch: "amd64", Firewall: &contracts.FirewallStatus{Tool: "ufw", Active: true, Rules: []string{itoa(port) + "/tcp"}}})
	n = mustJSON(t, admin, "GET", "/api/v1/admin/nodes/"+itoa(int(n["id"].(float64))), nil, 200)
	got := map[string]string{}
	for _, p := range n["ports"].([]any) {
		m := p.(map[string]any)
		got[m["proto"].(string)] = m["firewall"].(string)
	}
	if got["tcp"] != "open" || got["udp"] != "blocked" {
		t.Fatalf("ports = %v", n["ports"])
	}
	g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Version: "test", Arch: "amd64"})
	n = mustJSON(t, admin, "GET", "/api/v1/admin/nodes/"+itoa(int(n["id"].(float64))), nil, 200)
	if p := n["ports"].([]any)[0].(map[string]any); p["firewall"] != "unknown" {
		t.Fatalf("missing firewall report must be unknown: %v", p)
	}
}
