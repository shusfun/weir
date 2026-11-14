package subout

import (
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func TestUDPExportCapability(t *testing.T) {
	yes, no := true, false
	for _, provider := range []bool{false, true} {
		for _, planEnabled := range []bool{false, true} {
			p, items := testPlan()
			items[0].UDP, items[1].UDP = &yes, &no
			if !planEnabled {
				p = nil
			}
			var body []byte
			var err error
			if provider {
				body, err = ClashProvider(items, p, AllProvider)
			} else {
				body, _, err = RenderPlan(items, "clash", p)
			}
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Proxies []map[string]any `yaml:"proxies"`
			}
			if err = yaml.Unmarshal(body, &doc); err != nil {
				t.Fatal(err)
			}
			if len(doc.Proxies) != 3 || doc.Proxies[0]["udp"] != true || doc.Proxies[1]["udp"] != false {
				t.Fatalf("capabilities lost: %s", body)
			}
			if _, known := doc.Proxies[2]["udp"]; known {
				t.Fatal("unknown capability claimed as known")
			}
		}
	}
	for _, target := range []string{"singbox", "surge", "loon", "quanx", "base64", "egern"} {
		for _, withPlan := range []bool{false, true} {
			p, items := testPlan()
			items[1].UDP = &no
			if !withPlan {
				p = nil
			}
			body, _, report, err := RenderPlanDetailed(items, target, p)
			if err != nil {
				t.Fatalf("%s: %v", target, err)
			}
			switch target {
			case "base64", "egern":
				if len(report.Warnings) != 1 {
					t.Fatalf("%s missing limitation warning", target)
				}
			case "singbox":
				if !strings.Contains(string(body), `"network": "tcp"`) {
					t.Fatalf("missing TCP-only outbound: %s", body)
				}
			default:
				if !strings.Contains(string(body), "udp-relay=false") {
					t.Fatalf("%s missing UDP restriction", target)
				}
			}
		}
	}
}
