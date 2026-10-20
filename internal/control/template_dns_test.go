package control

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTemplateEncryptedDNS(t *testing.T) {
	for _, target := range []string{"clash", "singbox"} {
		t.Run(target, func(t *testing.T) {
			data := templateFixture(target)
			data["dns"] = []string{"https://dns.google:443/dns-query", "tls://1.1.1.1:853", "https://[2606:4700:4700::1111]/dns-query", "8.8.8.8"}
			data["dns_bootstrap"] = []string{"9.9.9.9"}
			model, err := decodeOutputTemplate(data)
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := renderOutputTemplate([]string{"http://example.com:80#HK"}, model)
			if err != nil {
				t.Fatal(err)
			}
			var cfg map[string]any
			if target == "clash" {
				yaml.Unmarshal(rendered, &cfg)
				dns := cfg["dns"].(map[string]any)
				if dns["nameserver"].([]any)[0] != "https://dns.google:443/dns-query" || dns["default-nameserver"].([]any)[0] != "9.9.9.9" {
					t.Fatal("Clash DNS transport or bootstrap lost")
				}
			} else {
				json.Unmarshal(rendered, &cfg)
				dns := cfg["dns"].(map[string]any)
				servers := dns["servers"].([]any)
				first := servers[0].(map[string]any)
				if len(servers) != 5 || first["type"] != "https" || first["path"] != "/dns-query" || first["server"] != "dns.google" || first["domain_resolver"] != "dns-bootstrap-0" || first["server_port"] != float64(443) {
					t.Fatal("sing-box encrypted resolver invalid")
				}
				if strings.Contains(string(rendered), `"insecure"`) {
					t.Fatal("TLS verification weakened")
				}
				if binary := os.Getenv("MIGATE_SINGBOX_BINARY"); binary != "" {
					p := filepath.Join(t.TempDir(), "dns.json")
					os.WriteFile(p, rendered, 0600)
					if output, err := exec.Command(binary, "check", "-c", p).CombinedOutput(); err != nil {
						t.Fatalf("real sing-box rejects encrypted DNS: %s", output)
					}
				}
			}
		})
	}
}

func TestTemplateDNSRejectsAmbiguousAddresses(t *testing.T) {
	for _, address := range []string{"http://1.1.1.1/dns-query", "https://u:p@1.1.1.1/dns-query", "https://1.1.1.1/dns-query?token=private", "https://1.1.1.1/dns-query#DIRECT", "tls://1.1.1.1/invalid", "https://1.1.1.1", "https://1.1.1.1:0/dns-query", "tls://1.1.1.1:", "tls://bad_host", "tls://[fe80::1%25eth0]", "https://dns.google/dns-query"} {
		if err := validateTemplateDNS([]string{address}, nil); err == nil {
			t.Fatalf("accepted invalid / unbootstrapped DNS %q", address)
		}
	}
	if validateTemplateDNS([]string{"https://dns.google/dns-query"}, []string{"dns.google"}) == nil {
		t.Fatal("hostname bootstrap accepted")
	}
	if validateTemplateDNS(nil, []string{"1.1.1.1"}) == nil {
		t.Fatal("unused bootstrap accepted")
	}
}
