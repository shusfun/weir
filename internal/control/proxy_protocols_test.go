package control

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/imzyb/MiGate/internal/subscription"
	"gopkg.in/yaml.v3"
)

func TestProxyProtocolCredentialsAndTLS(t *testing.T) {
	for _, test := range []struct {
		link, kind, user, password string
		tls                        bool
	}{
		{"http://127.0.0.1:8080#plain", "http", "", "", false},
		{"https://us%40er:pa%3Ass@127.0.0.1:8443?sni=proxy.example#secure", "http", "us@er", "pa:ss", true},
		{"socks5://us%40er:pa%3Ass@127.0.0.1:1080#socks", "socks", "us@er", "pa:ss", false},
		{"anytls://pa%3Ass@127.0.0.1:443?sni=proxy.example#any", "anytls", "", "pa:ss", true},
	} {
		out, err := singboxOutput([]string{test.link})
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]any
		if err = json.Unmarshal(out, &cfg); err != nil {
			t.Fatal(err)
		}
		p := cfg["outbounds"].([]any)[1].(map[string]any)
		if p["type"] != test.kind || p["password"] != test.password || test.kind != "anytls" && p["username"] != test.user || (p["tls"] != nil) != test.tls {
			t.Fatal("proxy credentials/TLS lost", test.kind)
		}
		clash, err := clashOutput([]string{test.link})
		if err != nil {
			t.Fatal(err)
		}
		var c map[string]any
		if err = yaml.Unmarshal(clash, &c); err != nil {
			t.Fatal(err)
		}
		if c["proxies"].([]any)[0].(map[string]any)["password"] != test.password {
			t.Fatal("clash password lost")
		}
		if binary := os.Getenv("MIGATE_SINGBOX_BINARY"); binary != "" {
			path := filepath.Join(t.TempDir(), "client.json")
			if err = os.WriteFile(path, out, 0600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(binary, "check", "-c", path).CombinedOutput(); err != nil {
				t.Fatalf("actual sing-box config check: %s %v", output, err)
			}
		}
	}
}

func TestUnsupportedTLSAndMisplacedFieldsRejected(t *testing.T) {
	for _, link := range []string{
		"socks5-tls://u:p@example.com:1080?sni=proxy.example",
		"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388?security=tls",
		"vless://id@example.com:443?security=unknown",
		"vless://id@example.com:443?type=tcp&serviceName=dropped",
		"trojan://secret@example.com:443?security=tls&flow=xtls-rprx-vision",
		"vless://id@example.com:443?security=tls&type=grpc&flow=xtls-rprx-vision",
		"vless://id@example.com:443?flow=xtls-rprx-vision",
		"vless://id@example.com:443?security=tls&flow=unknown",
	} {
		if _, err := singboxOutput([]string{link}); err == nil {
			t.Fatal("unsupported security/field silently converted")
		}
	}
	if _, err := subscription.ParseLink("http://example.com:80?x=%zz"); err == nil {
		t.Fatal("malformed query silently dropped")
	}
	n, err := subscription.ParseLink("https://proxy.example")
	if err != nil || n.Port != 443 {
		t.Fatal("HTTPS default port missing")
	}
	if _, err := clashOutput([]string{"socks5-tls://u:p@example.com:1080?sni=proxy.example"}); err != nil {
		t.Fatal("Clash SOCKS TLS should retain TLS", err)
	}
}
