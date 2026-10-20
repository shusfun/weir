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

func TestAutomaticTemplateGroups(t *testing.T) {
	links := []string{"http://u:p@example.org:8080#HK-one", "socks5://u:p@example.org:1080#Japan-two", "http://u:p@example.org:8081#HK-three", "http://u:p@example.org:8082#%24nodes"}
	for _, target := range []string{"clash", "singbox"} {
		for _, mode := range []string{"region", "protocol"} {
			t.Run(target+"/"+mode, func(t *testing.T) {
				data := map[string]any{"name": "auto", "target": target, "auto_groups": mode, "groups": []any{map[string]any{"name": "PROXY", "type": "select", "members": []string{"$auto", "DIRECT"}}, map[string]any{"name": "香港 节点", "type": "select", "members": []string{"$nodes"}}}, "rules": []any{map[string]any{"type": "FINAL", "policy": "PROXY"}}}
				model, err := decodeOutputTemplate(data)
				if err != nil {
					t.Fatal(err)
				}
				body, err := renderOutputTemplate(links, model)
				if err != nil {
					t.Fatal(err)
				}
				var cfg map[string]any
				var groups []any
				memberKey, nameKey := "proxies", "name"
				if target == "clash" {
					yaml.Unmarshal(body, &cfg)
					groups = cfg["proxy-groups"].([]any)
				} else {
					json.Unmarshal(body, &cfg)
					groups = cfg["outbounds"].([]any)
					groups = groups[:len(groups)-len(links)-1]
					memberKey, nameKey = "outbounds", "tag"
				}
				expected := 5
				if mode == "protocol" {
					expected = 4
				}
				if len(groups) != expected {
					t.Fatalf("unexpected group count %d", len(groups))
				}
				if strings.Contains(string(body), `"$auto"`) || strings.Contains(string(body), `"$nodes"`) {
					t.Fatal("placeholder not expanded")
				}
				all := map[string]int{}
				for _, raw := range groups[2:] {
					g := raw.(map[string]any)
					for _, n := range g[memberKey].([]any) {
						all[n.(string)]++
					}
					if g[nameKey] == "香港 节点" {
						t.Fatal("automatic group overwrote existing group")
					}
				}
				if len(all) != len(links) {
					t.Fatal("automatic grouping lost or introduced nodes")
				}
				for _, count := range all {
					if count != 1 {
						t.Fatal("node assigned to multiple automatic buckets")
					}
				}
				main := groups[0].(map[string]any)[memberKey].([]any)
				if len(main) != expected-1 {
					t.Fatal("selector lost automatic group references")
				}
				// Rendering must not mutate the stored template member slice.
				if model.Groups[0].Members[0] != "$auto" {
					t.Fatal("render mutated saved template")
				}
				if target == "singbox" {
					if binary := os.Getenv("MIGATE_SINGBOX_BINARY"); binary != "" {
						file := filepath.Join(t.TempDir(), "auto.json")
						os.WriteFile(file, body, 0600)
						if out, e := exec.Command(binary, "check", "-c", file).CombinedOutput(); e != nil {
							t.Fatalf("real core rejected auto groups: %s", out)
						}
					}
				}
			})
		}
	}
}

func TestAutomaticGroupValidation(t *testing.T) {
	for _, mode := range []string{"", "javascript"} {
		data := templateFixture("singbox")
		data["auto_groups"] = mode
		data["groups"].([]any)[1].(map[string]any)["members"] = []string{"$auto"}
		if _, err := decodeOutputTemplate(data); err == nil {
			t.Fatal("unsupported automatic placeholder accepted")
		}
	}
	data := templateFixture("singbox")
	data["auto_groups"] = "region"
	data["groups"].([]any)[0].(map[string]any)["members"] = []string{"$auto"}
	if _, err := decodeOutputTemplate(data); err == nil {
		t.Fatal("automatic groups accepted in url-test parent")
	}
}
