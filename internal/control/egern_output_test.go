package control

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEgernUpstreamNodeComparison(t *testing.T) {
	if os.Getenv("MIGATE_UPSTREAM_TESTS") != "1" {
		t.Skip("requires local upstream Node oracle")
	}
	links := []string{
		"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#HK",
		"trojan://secret@example.com:443?sni=example.org#HK",
		"trojan://secret@example.com:443?sni=example.org&type=ws&host=cdn.example.com&path=/ws#HK",
		"trojan://secret@example.com:443?sni=example.org&type=grpc&serviceName=service#HK",
		"vless://uuid@example.com:443?security=tls&sni=example.org#HK",
		"vless://uuid@example.com:443?security=tls&sni=example.org&type=ws&host=cdn.example.com&path=/ws#HK",
		"anytls://secret@example.com:443?sni=example.org#HK",
		"hysteria2://secret@example.com:443?sni=example.org#HK",
		"tuic://uuid:secret@example.com:443?sni=example.org&alpn=h3#HK",
	}
	for _, link := range links {
		out, err := egernOutput([]string{link})
		if err != nil {
			t.Fatal(err)
		}
		clash, err := clashOutput([]string{link})
		if err != nil {
			t.Fatal(err)
		}
		var cfg, actual map[string]any
		yaml.Unmarshal(clash, &cfg)
		yaml.Unmarshal(out, &actual)
		proxy := cfg["proxies"].([]any)[0]
		input, _ := json.Marshal(map[string]any{"target": "egern", "proxy": proxy})
		cmd := exec.Command("node", filepath.Join("..", "..", "third_party", "misub", "reference", "compare-ini.mjs"))
		cmd.Stdin = bytes.NewReader(input)
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("upstream Egern oracle: %s", b)
		}
		var expected, got any
		json.Unmarshal(b, &expected)
		b, _ = json.Marshal(actual["proxies"].([]any)[0])
		json.Unmarshal(b, &got)
		if !reflect.DeepEqual(expected, got) {
			t.Fatalf("Egern fields differ: expected=%v actual=%v", expected, got)
		}
	}
	t.Logf("%d Egern mappings compared with pinned upstream", len(links))
}

func TestEgernRejectsLossAndKeepsCredentials(t *testing.T) {
	for _, link := range []string{"https://u:p@example.com:443", "socks5://u:p@example.com:1080", "vless://uuid@example.com:443?security=reality&pbk=key&sid=abcd&fp=chrome", "hysteria2://secret@example.com:443?spki=pin"} {
		if _, err := egernOutput([]string{link}); err == nil {
			t.Fatal("Egern credential/TLS field loss accepted")
		}
	}
	out, err := egernOutput([]string{"trojan://%20actual%20secret%20@example.com:443#PROXY"})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	yaml.Unmarshal(out, &cfg)
	proxy := cfg["proxies"].([]any)[0].(map[string]any)["trojan"].(map[string]any)
	if proxy["name"] == "PROXY" || proxy["password"] != " actual secret " {
		t.Fatal("credentials trimmed or group name shadowed")
	}
}
