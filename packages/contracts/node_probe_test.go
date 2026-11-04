package contracts

import "testing"

func TestNodeTestClientDependencyGraph(t *testing.T) {
	shadow := map[string]any{"type": "shadowtls", "tag": "tls", "server": "example.com", "server_port": 443}
	ss := map[string]any{"type": "shadowsocks", "tag": "ss", "detour": "tls"}
	c := NodeTestClient{Target: "ss", Outbounds: []map[string]any{shadow, ss}}
	if err := c.Loopback(12345); err != nil {
		t.Fatal(err)
	}
	if ss["detour"] != "tls" || ss["server"] != nil || shadow["server"] != "127.0.0.1" || shadow["server_port"] != 12345 {
		t.Fatal("dependency graph corrupted")
	}
	peer := map[string]any{"address": "example.com", "port": 443, "public_key": "keep"}
	c = NodeTestClient{Target: "wg", Endpoints: []map[string]any{{"tag": "wg", "type": "wireguard", "peers": []any{peer}}}}
	if err := c.Loopback(12346); err != nil {
		t.Fatal(err)
	}
	if peer["address"] != "127.0.0.1" || peer["port"] != 12346 || peer["public_key"] != "keep" {
		t.Fatal("peer corrupted")
	}
	c = NodeTestClient{Target: "ss", Outbounds: []map[string]any{ss}}
	if c.Loopback(12345) == nil {
		t.Fatal("missing dependency accepted")
	}
}
