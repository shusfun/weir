package subout

import (
	"reflect"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
	"gopkg.in/yaml.v3"
)

func TestAutomaticGroupAllowsManualSelection(t *testing.T) {
	for _, kind := range []string{"url-test", "fallback"} {
		t.Run(kind, func(t *testing.T) {
			p, items := testPlan()
			p.Groups[0].Kind = kind
			p.Groups[0].IntervalSeconds, p.Groups[0].ToleranceMS = 120, 75
			groups, _ := p.Layout([]string{"HK", "JP", "US"}, items)
			if len(groups) != 4 {
				t.Fatalf("需要可见选择组和内部自动组：%+v", groups)
			}
			selection, automatic := groups[2], groups[3]
			if selection.Ref != "group:1" || selection.Kind != "select" || selection.Hidden || !reflect.DeepEqual(selection.Members, []string{automatic.Name, "JP", "US"}) || !reflect.DeepEqual(selection.MemberRefs, []string{automatic.Ref, "chain:2", "node:3"}) {
				t.Fatalf("必须默认自动且可选择全部节点：%+v", selection)
			}
			if automatic.Kind != kind || !automatic.Hidden || automatic.IntervalSeconds != 120 || automatic.ToleranceMS != 75 || !reflect.DeepEqual(automatic.MemberRefs, []string{"chain:2", "node:3"}) {
				t.Fatalf("自动策略或顺序丢失：%+v", automatic)
			}
			p.Providers = func(key string) string { return "https://center.example/provider/" + key }
			body, _, err := RenderPlan(items, "mihomo", p)
			if err != nil {
				t.Fatal(err)
			}
			var cfg struct {
				Groups []struct {
					Name    string   `yaml:"name"`
					Proxies []string `yaml:"proxies"`
					Use     []string `yaml:"use"`
				} `yaml:"proxy-groups"`
				Providers map[string]struct {
					Health struct {
						Enable bool `yaml:"enable"`
					} `yaml:"health-check"`
				} `yaml:"proxy-providers"`
			}
			if err := yaml.Unmarshal(body, &cfg); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.Groups[2].Proxies, []string{automatic.Name}) || !reflect.DeepEqual(cfg.Groups[2].Use, []string{"p-group-1"}) || !reflect.DeepEqual(cfg.Groups[3].Use, []string{"p-group-1"}) || !cfg.Providers["p-group-1"].Health.Enable {
				t.Fatalf("外层和自动组应共用原 provider 并启用健康检查：%s", body)
			}
		})
	}
}

func TestAutomaticGroupNamesAreUnique(t *testing.T) {
	p, items := testPlan()
	p.Groups[0].Kind = "url-test"
	p.Groups = append(p.Groups, contracts.ProxyGroup{ID: 2, Name: "主备 · 自动选择", Kind: "select", Members: []string{"node:1"}})
	groups, _ := p.Layout([]string{"主备 · 自动选择 (2)", "JP", "US"}, items)
	seen := map[string]bool{"主备 · 自动选择 (2)": true, "JP": true, "US": true}
	for _, g := range groups {
		if seen[g.Name] {
			t.Fatalf("组名与用户组或节点冲突：%s", g.Name)
		}
		seen[g.Name] = true
	}
}
