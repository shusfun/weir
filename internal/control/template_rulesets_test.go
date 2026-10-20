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

func remoteTemplateFixture(target string) map[string]any {
	data := templateFixture(target)
	set := map[string]any{"name": "cloudflare", "url": "https://example.org/cloudflare.json", "format": "source", "interval": 7200}
	if target == "clash" {
		set["format"], set["behavior"] = "yaml", "domain"
	}
	data["rule_sets"] = []any{set}
	data["rules"] = []any{map[string]any{"type": "RULE-SET", "value": "cloudflare", "policy": "REJECT"}, map[string]any{"type": "FINAL", "policy": "Choice"}}
	return data
}

func TestRemoteRuleSetRendering(t *testing.T) {
	for _, target := range []string{"clash", "singbox"} {
		t.Run(target, func(t *testing.T) {
			model, err := decodeOutputTemplate(remoteTemplateFixture(target))
			if err != nil {
				t.Fatal(err)
			}
			body, err := renderOutputTemplate([]string{"http://u:p@example.org:8080#HK"}, model)
			if err != nil {
				t.Fatal(err)
			}
			var cfg map[string]any
			if target == "clash" {
				if err = yaml.Unmarshal(body, &cfg); err != nil {
					t.Fatal(err)
				}
				p := cfg["rule-providers"].(map[string]any)["cloudflare"].(map[string]any)
				if p["path"] != "./rule-providers/cloudflare.yaml" || p["url"] != model.RuleSets[0].URL || cfg["rules"].([]any)[0] != "RULE-SET,cloudflare,REJECT" {
					t.Fatal("Clash rule provider lost mapping")
				}
			} else {
				if err = json.Unmarshal(body, &cfg); err != nil {
					t.Fatal(err)
				}
				route := cfg["route"].(map[string]any)
				set := route["rule_set"].([]any)[0].(map[string]any)
				if set["tag"] != "cloudflare" || set["update_interval"] != "7200s" || set["download_detour"] != "direct" || route["rules"].([]any)[0].(map[string]any)["rule_set"].([]any)[0] != "cloudflare" {
					t.Fatal("sing-box remote set lost mapping")
				}
				if binary := os.Getenv("MIGATE_SINGBOX_BINARY"); binary != "" {
					file := filepath.Join(t.TempDir(), "remote.json")
					if err = os.WriteFile(file, body, 0600); err != nil {
						t.Fatal(err)
					}
					if out, e := exec.Command(binary, "check", "-c", file).CombinedOutput(); e != nil {
						t.Fatalf("real core rejected remote set: %s", out)
					}
				}
			}
		})
	}
}

func TestRemoteRuleSetRejectsUnsafeAndAmbiguousDeclarations(t *testing.T) {
	for name, patch := range map[string]map[string]any{
		"path traversal": {"name": "../bad"}, "credentials": {"url": "https://secret:token@example.org/rules"}, "query token": {"url": "https://example.org/rules?token=secret"}, "fragment": {"url": "https://example.org/rules#bad"}, "cleartext": {"url": "http://example.org/rules"}, "unknown format": {"format": "javascript"}, "interval": {"interval": 1}, "singbox behavior": {"behavior": "domain"},
	} {
		t.Run(name, func(t *testing.T) {
			data := remoteTemplateFixture("singbox")
			set := data["rule_sets"].([]any)[0].(map[string]any)
			for k, v := range patch {
				set[k] = v
			}
			if _, err := decodeOutputTemplate(data); err == nil {
				t.Fatal("unsafe rule set accepted")
			}
		})
	}
	data := remoteTemplateFixture("singbox")
	sets := data["rule_sets"].([]any)
	data["rule_sets"] = append(sets, sets[0])
	if _, err := decodeOutputTemplate(data); err == nil {
		t.Fatal("duplicate rule set accepted")
	}
	data = remoteTemplateFixture("singbox")
	data["rule_sets"] = []any{}
	if _, err := decodeOutputTemplate(data); err == nil || !strings.Contains(err.Error(), "unknown rule set") {
		t.Fatal("dangling reference accepted")
	}
}
