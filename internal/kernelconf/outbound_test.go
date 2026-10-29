package kernelconf

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

// 作客户端连出去：协议看有没有 outbound.<协议> 模板，传输看 outbound_networks（继承后也要在）。
func TestOutboundAllowsByDriver(t *testing.T) {
	xray, err := ParseDriver(driverText(t, "xray-26.3"))
	if err != nil {
		t.Fatal(err)
	}
	sing, err := ParseDriver(driverText(t, "sing-box-1.12"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		d        Driver
		protocol string
		network  string
		ok       bool
	}{
		{xray, "vless", "xhttp", true}, {xray, "vless", "httpupgrade", true}, {xray, "shadowsocks", "", true},
		{xray, "hysteria2", "", false}, {xray, "anytls", "", false}, {xray, "vless", "kcp", false},
		{sing, "hysteria2", "", true}, {sing, "tuic", "", true}, {sing, "vless", "httpupgrade", true},
		{sing, "vless", "xhttp", false},
	} {
		if err := c.d.OutboundAllows(c.protocol, c.network); (err == nil) != c.ok {
			t.Fatalf("%s %s/%s = %v, want ok=%v", c.d.Name, c.protocol, c.network, err, c.ok)
		}
	}
}

// XHTTP 出口带上路径与模式（之前不生成 xhttpSettings，连接按默认路径走，和出口节点对不上）。
func TestXrayOutboundXHTTP(t *testing.T) {
	cfg := scenarios(t)["xray-noroute"]
	cfg.Drivers = map[string]string{"xray": driverText(t, "xray-26.3")}
	cfg.Outbounds = append(cfg.Outbounds, contracts.Outbound{ID: 77, Tag: "x", Protocol: "vless", Address: "203.0.113.7", Port: 443, Username: "11111111-1111-4111-8111-111111111111",
		Settings: map[string]any{"network": "xhttp", "security": "tls", "sni": "a.example.com", "path": "/up", "mode": "packet-up"}})
	out, err := Build(cfg, Options{CertDir: certs(t)})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	_ = json.Unmarshal(out.Xray, &doc)
	for _, o := range doc.Outbounds {
		if o["tag"] != "xray-out-77" {
			continue
		}
		st, _ := o["streamSettings"].(map[string]any)
		x, _ := st["xhttpSettings"].(map[string]any)
		if st["network"] != "xhttp" || x["path"] != "/up" || x["mode"] != "packet-up" {
			t.Fatalf("xhttp outbound = %v", st)
		}
		return
	}
	t.Fatal("outbound missing")
}

// 实时计数把内核标签换回配置里的出口标签：一组的成员按展开顺序，固定直连为 direct。
func TestOutboundNames(t *testing.T) {
	cfg := contracts.GateConfig{Outbounds: []contracts.Outbound{
		{ID: 5, Tag: "home", Protocol: "socks", Address: "203.0.113.5", Port: 1080},
		{ID: 6, Tag: "pool", Protocol: "group", Members: []contracts.Outbound{{Protocol: "trojan"}, {Protocol: "trojan"}}},
	}}
	names := OutboundNames(cfg)
	for kernelTag, want := range map[string]string{"xray-out-5": "home", "singbox-out-5": "home", "xray-out-6": "pool", "xray-out-2147483647": "direct"} {
		if names[kernelTag] != want {
			t.Fatalf("%s = %q, want %q", kernelTag, names[kernelTag], want)
		}
	}
	members := map[string]bool{}
	for _, v := range names {
		if strings.HasPrefix(v, "pool-m") {
			members[v] = true
		}
	}
	if len(members) != 2 || !hasValue(names, "pool-m1") || !hasValue(names, "pool-m2") {
		t.Fatalf("members = %v", names)
	}
}

func hasValue(m map[string]string, v string) bool {
	for _, x := range m {
		if x == v {
			return true
		}
	}
	return false
}
