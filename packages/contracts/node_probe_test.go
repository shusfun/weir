package contracts

import "testing"

func TestNodeTestClientLoopback(t *testing.T) {
	for _, kind := range []string{"vless", "ss", "wireguard"} {
		t.Run(kind, func(t *testing.T) {
			p := map[string]any{"name": "test", "type": kind, "server": "example.com", "port": 443, "servername": "sni.example.com", "plugin-opts": map[string]any{"host": "sni.example.com"}}
			c := NodeTestClient{Target: "test", Proxies: []map[string]any{p}}
			if err := c.Loopback(12345); err != nil {
				t.Fatal(err)
			}
			if p["server"] != "127.0.0.1" || p["port"] != 12345 || p["servername"] != "sni.example.com" {
				t.Fatal(p)
			}
			cfg := c.Config(12346)
			if cfg["allow-lan"] != false || cfg["bind-address"] != "127.0.0.1" {
				t.Fatal(cfg)
			}
		})
	}
	if (NodeTestClient{}).Loopback(12345) == nil {
		t.Fatal("缺少代理不能测试")
	}
}
