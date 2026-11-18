package subout

import (
	"testing"

	"github.com/shusfun/weir/packages/contracts"
	"gopkg.in/yaml.v3"
)

func TestRawClashEmptyFallback(t *testing.T) {
	_, items := testPlan()
	for _, target := range []string{"clash", "mihomo"} {
		body, _, err := RenderPlan(items, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Groups []map[string]any `yaml:"proxy-groups"`
		}
		if err := yaml.Unmarshal(body, &cfg); err != nil {
			t.Fatal(err)
		}
		if len(cfg.Groups) != 1 || cfg.Groups[0]["empty-fallback"] != "REJECT" {
			t.Fatalf("raw subscription group can escape: %+v", cfg.Groups)
		}
	}
}

func TestProbePolicyAndEmptyGroups(t *testing.T) {
	for _, provider := range []bool{false, true} {
		for _, target := range []string{"clash", "mihomo"} {
			p, items := testPlan()
			p.Groups[0].IntervalSeconds = 123
			p.TestURL = SubscriptionProbeURL
			if provider {
				p.Providers = func(key string) string { return "https://example.test/provider/" + key }
			}
			body, _, err := RenderPlan(items, target, p)
			if err != nil {
				t.Fatal(err)
			}
			var cfg struct {
				Groups    []map[string]any `yaml:"proxy-groups"`
				Providers map[string]struct {
					Health map[string]any `yaml:"health-check"`
				} `yaml:"proxy-providers"`
			}
			if err := yaml.Unmarshal(body, &cfg); err != nil {
				t.Fatal(err)
			}
			check := func(h map[string]any) {
				t.Helper()
				if h["url"] != "https://www.gstatic.com/generate_204" || h["expected-status"] != "204" || h["timeout"] != 5000 || h["lazy"] != false {
					t.Fatalf("unsafe probe: %+v", h)
				}
			}
			for _, g := range cfg.Groups {
				if g["empty-fallback"] != "REJECT" {
					t.Fatalf("empty group can escape: %+v", g)
				}
				if g["type"] == "select" {
					continue
				}
				check(g)
				want := 123
				if g["name"] == GroupAuto {
					want = contracts.DefaultGroupIntervalSeconds
				}
				if g["interval"] != want {
					t.Fatalf("interval not preserved: %+v", g)
				}
				if provider {
					key := g["use"].([]any)[0].(string)
					h := cfg.Providers[key].Health
					check(h)
					if h["interval"] != g["interval"] {
						t.Fatal("group/provider intervals disagree")
					}
				}
			}
		}
	}
}
