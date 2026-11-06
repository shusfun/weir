package center

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/internal/subscription"
	"gopkg.in/yaml.v3"
)

func TestSubscriptionNamesReachEveryFormatAndRefresh(t *testing.T) {
	s, admin, g := setupGate(t)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "name": "原备注", "skip_probe": true}, 200)
	// A standard SS cipher is representable by every supported client, including QuanX.
	if err := s.Store.Ent.Node.UpdateOneID(int(n["id"].(float64))).SetSettings(map[string]any{"ss_method": "aes-128-gcm", "ss_password": "test-only-password"}).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	// SS2022 credentials contain base64 padding which QuanX correctly rejects.
	// This conversion fixture uses the standard cipher above and a literal secret.
	if err := s.Store.Ent.Credential.Update().SetPassword("client-test-password").Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	links := mustJSON(t, admin, "GET", "/api/v1/me/links", nil, 200)["items"].([]any)
	token := links[0].(map[string]any)["token"].(string)
	for _, remark := range []string{"原备注", "新备注"} {
		mustJSON(t, admin, "PATCH", "/api/v1/admin/nodes/"+itoa(int(n["id"].(float64))), map[string]any{"name": remark}, 200)
		for _, target := range subout.Formats {
			t.Run(remark+"/"+target, func(t *testing.T) {
				w := admin.do("GET", "/sub/"+token+"?target="+target, nil, nil)
				if w.Code != 200 {
					t.Fatalf("%s: %d %s", target, w.Code, w.Body)
				}
				pulls := mustJSON(t, admin, "GET", "/api/v1/me/links/"+itoa(int(links[0].(map[string]any)["id"].(float64)))+"/pulls", nil, 200)["items"].([]any)
				if len(pulls) == 0 || pulls[0].(map[string]any)["trace_id"] != w.Header().Get("X-Trace-Id") {
					t.Fatal("subscription pull lost request trace")
				}
				body := w.Body.Bytes()
				if target == "singbox" || target == "surge" || target == "loon" || target == "quanx" {
					links, err := subscription.DecodeInventory(body)
					if err != nil {
						t.Fatalf("parse %s inventory: %v", target, err)
					}
					parsed, err := subscription.ParseLinks(links)
					if err != nil || len(parsed.Nodes) != 1 || len(parsed.Skipped) != 0 {
						t.Fatalf("parse %s nodes: %v", target, err)
					}
					p := parsed.Nodes[0]
					want := "[直连] " + remark
					if target != "singbox" {
						want = "直连 " + remark
					}
					if p.Remark != want || p.Address == "" || p.Port != int(n["port"].(float64)) || p.Password != "test-only-password:client-test-password" {
						t.Fatalf("%s name or connection parameters changed", target)
					}
				}
				switch target {
				case "base64":
					decoded, err := base64.StdEncoding.DecodeString(string(body))
					if err != nil {
						t.Fatal(err)
					}
					parsed, err := subscription.ParseBody(decoded)
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, p := range parsed {
						if p.Remark == "[直连] "+remark {
							found = true
							if p.Address == "" || p.Port != int(n["port"].(float64)) {
								t.Fatalf("connection changed: %+v", p)
							}
						}
					}
					if !found {
						t.Fatalf("name missing: %s", decoded)
					}
				case "clash":
					var doc struct {
						Proxies []struct {
							Name string `yaml:"name"`
						}
						Groups []struct {
							Proxies []string `yaml:"proxies"`
						} `yaml:"proxy-groups"`
					}
					if err := yaml.Unmarshal(body, &doc); err != nil {
						t.Fatal(err)
					}
					if len(doc.Proxies) == 0 {
						provider := admin.do("GET", "/sub/"+token+"/provider/ss2022", nil, nil)
						if provider.Code != 200 {
							t.Fatalf("provider=%d %s", provider.Code, provider.Body)
						}
						if err := yaml.Unmarshal(provider.Body.Bytes(), &doc); err != nil {
							t.Fatal(err)
						}
					}
					if len(doc.Proxies) != 1 || doc.Proxies[0].Name != "[直连] "+remark {
						t.Fatalf("clash names=%v", doc.Proxies)
					}
					allowed := map[string]bool{"DIRECT": true, "REJECT": true, "节点选择": true, "自动选择": true}
					for _, p := range doc.Proxies {
						allowed[p.Name] = true
					}
					// Provider-backed type groups use provider names, while inline references must not retain the old remark.
					for _, group := range doc.Groups {
						for _, ref := range group.Proxies {
							if strings.Contains(ref, "备注") && !allowed[ref] {
								t.Fatalf("stale reference=%s", ref)
							}
						}
					}
				case "singbox":
					var doc struct {
						Outbounds []struct {
							Tag       string   `json:"tag"`
							Outbounds []string `json:"outbounds"`
						}
					}
					if err := json.Unmarshal(body, &doc); err != nil {
						t.Fatal(err)
					}
					found := false
					tags := map[string]bool{}
					for _, o := range doc.Outbounds {
						tags[o.Tag] = true
						found = found || o.Tag == "[直连] "+remark
					}
					if !found {
						t.Fatalf("tag missing: %s", body)
					}
					for _, o := range doc.Outbounds {
						for _, ref := range o.Outbounds {
							if !tags[ref] {
								t.Fatalf("dangling tag=%s", ref)
							}
						}
					}
				default:
					if !strings.Contains(string(body), remark) {
						t.Fatalf("name missing: %s", body)
					}
					if target == "egern" {
						var doc struct {
							Proxies []map[string]struct {
								Name     string `yaml:"name"`
								Server   string `yaml:"server"`
								Port     int    `yaml:"port"`
								Password string `yaml:"password"`
							} `yaml:"proxies"`
							Groups []map[string]struct {
								Name     string   `yaml:"name"`
								Policies []string `yaml:"policies"`
							} `yaml:"policy_groups"`
						}
						if err := yaml.Unmarshal(body, &doc); err != nil {
							t.Fatal(err)
						}
						if len(doc.Proxies) != 1 {
							t.Fatal("Egern node count changed")
						}
						known := map[string]bool{"DIRECT": true, "REJECT": true}
						for _, proxy := range doc.Proxies {
							for _, p := range proxy {
								if p.Name != "[直连] "+remark || p.Server == "" || p.Port != int(n["port"].(float64)) || p.Password != "test-only-password:client-test-password" {
									t.Fatal("Egern name or connection parameters changed")
								}
								known[p.Name] = true
							}
						}
						for _, group := range doc.Groups {
							for _, p := range group {
								known[p.Name] = true
							}
						}
						for _, group := range doc.Groups {
							for _, p := range group {
								for _, ref := range p.Policies {
									if !known[ref] {
										t.Fatalf("Egern dangling reference=%q", ref)
									}
								}
							}
						}
					} else {
						// INI writers escape reserved brackets, but the configured remark must occur in both definition and policy reference.
						if strings.Count(string(body), remark) < 2 {
							t.Fatalf("definition or reference missing: %s", body)
						}
					}
				}
				if remark == "新备注" && strings.Contains(string(body), "原备注") {
					t.Fatal("cached old name")
				}
			})
		}
		info := mustJSON(t, admin, "GET", "/api/v1/sub-info/"+token, nil, 200)
		if name := info["nodes"].([]any)[0].(map[string]any)["name"]; name != "[直连] "+remark {
			t.Fatalf("preview=%v", name)
		}
	}
}
