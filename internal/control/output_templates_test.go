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

func templateFixture(target string) map[string]any {
	return map[string]any{"name": "test", "target": target,
		"groups": []any{map[string]any{"name": "Auto", "type": "url-test", "members": []string{"$nodes"}, "filter": "HK"}, map[string]any{"name": "Choice", "type": "select", "members": []string{"Auto", "$nodes", "DIRECT"}}},
		"rules":  []any{map[string]any{"type": "DOMAIN-SUFFIX", "value": "example.org", "policy": "REJECT"}, map[string]any{"type": "IP-CIDR", "value": "192.168.0.0/16", "policy": "DIRECT"}, map[string]any{"type": "FINAL", "policy": "Choice"}}, "dns": []string{"1.1.1.1"}}
}

func TestOutputTemplateRendering(t *testing.T) {
	for _, target := range []string{"clash", "singbox"} {
		t.Run(target, func(t *testing.T) {
			data := templateFixture(target)
			model, err := decodeOutputTemplate(data)
			if err != nil {
				t.Fatal(err)
			}
			links := []string{"http://u:p@127.0.0.1:8080#HK", "http://u:p@127.0.0.1:8081#Choice"}
			b, err := renderOutputTemplate(links, model)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "$nodes") {
				t.Fatal("unexpanded node placeholder")
			}
			var cfg map[string]any
			if target == "clash" {
				if err = yaml.Unmarshal(b, &cfg); err != nil {
					t.Fatal(err)
				}
				proxies := cfg["proxies"].([]any)
				groups := cfg["proxy-groups"].([]any)
				if proxies[1].(map[string]any)["name"] == "Choice" || len(groups[0].(map[string]any)["proxies"].([]any)) != 1 {
					t.Fatal("group/name/filter isolation failed")
				}
				if cfg["rules"].([]any)[2] != "MATCH,Choice" {
					t.Fatal("final policy missing")
				}
			} else {
				if err = json.Unmarshal(b, &cfg); err != nil {
					t.Fatal(err)
				}
				route := cfg["route"].(map[string]any)
				if route["final"] != "Choice" || route["rules"].([]any)[0].(map[string]any)["action"] != "reject" {
					t.Fatal("route action missing")
				}
				if binary := os.Getenv("MIGATE_SINGBOX_BINARY"); binary != "" {
					file := filepath.Join(t.TempDir(), "template.json")
					os.WriteFile(file, b, 0600)
					if output, e := exec.Command(binary, "check", "-c", file).CombinedOutput(); e != nil {
						t.Fatalf("real core rejected template: %s", output)
					}
				}
			}
		})
	}
}

func TestOutputTemplateRejectsUnsafeOrAmbiguousModels(t *testing.T) {
	for _, change := range []func(map[string]any){
		func(d map[string]any) { d["proxies"] = []any{map[string]any{"password": "outside"}} },
		func(d map[string]any) { d["dns"] = []string{"https://unvalidated.example/dns-query"} },
		func(d map[string]any) { d["groups"].([]any)[0].(map[string]any)["members"] = []string{"Choice"} },
		func(d map[string]any) { d["groups"].([]any)[0].(map[string]any)["members"] = []string{"outside"} },
		func(d map[string]any) { d["groups"].([]any)[0].(map[string]any)["filter"] = "[" },
		func(d map[string]any) { d["rules"].([]any)[0].(map[string]any)["value"] = "example.org\nMATCH,DIRECT" },
		func(d map[string]any) { d["rules"].([]any)[0].(map[string]any)["type"] = "RULE-SET" },
		func(d map[string]any) { d["rules"].([]any)[0].(map[string]any)["policy"] = "outside" },
	} {
		d := templateFixture("singbox")
		change(d)
		if _, err := decodeOutputTemplate(d); err == nil {
			t.Fatal("unsafe template accepted")
		}
	}
	model, _ := decodeOutputTemplate(templateFixture("singbox"))
	if _, err := renderOutputTemplate([]string{"http://example.com:80#US"}, model); err == nil {
		t.Fatal("empty filtered group accepted")
	}
}
