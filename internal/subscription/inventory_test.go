package subscription

import "testing"

func TestStructuredInventoryRefusesSilentFieldLoss(t *testing.T) {
	for _, content := range []string{
		`{"outbounds":[{"type":"http","server":"example.com","server_port":80,"username":"u","password":"p","detour":"unknown"}]}`,
		`{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"id","password":"discarded"}]}`,
		`{"outbounds":[{"type":"http","server":"example.com","server_port":443,"tls":{"enabled":true,"server_name":42}}]}`,
		`{"outbounds":[{"type":"unknown","server":"example.com","server_port":443}]}`,
		`{"outbounds":[{"type":"http","server":"example.com","server_port":80,"password":42}]}`,
		"proxies:\n - {type: ss, server: example.com, port: 8388, cipher: aes-128-gcm, password: secret, plugin: unknown}",
		"proxies:\n - {type: http, server: example.com, port: 80}\n---\nproxies: []",
		"not-a-supported-line\nhttp://example.com:80",
		"proxies:\n - {type: http, server: example.com, port: 80, ws-opts: {path: /discarded}}",
	} {
		if _, err := DecodeInventory([]byte(content)); err == nil {
			t.Fatal("unsupported structured field silently imported")
		}
	}
}
