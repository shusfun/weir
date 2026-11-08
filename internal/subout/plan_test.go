package subout

import (
	"encoding/json"
	"github.com/shusfun/weir/packages/contracts"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testPlan() (*Plan, []Item) {
	items := []Item{
		{Link: "trojan://pass1@203.0.113.1:443?security=tls&sni=example.com#HK", Ref: "node:1", Auto: true, Direct: true},
		{Link: "trojan://pass2@203.0.113.2:443?security=tls&sni=example.com#JP", Ref: "chain:2"},
		{Link: "trojan://pass3@203.0.113.3:443?security=tls&sni=example.com#US", Ref: "node:3", Auto: true, Direct: true},
	}
	p := &Plan{Groups: []contracts.ProxyGroup{{ID: 1, Name: "主备", Kind: "fallback", Members: []string{"chain:2", "node:3"}, IntervalSeconds: 60, ToleranceMS: 50}}, Rules: []Rule{{Kind: "domain_suffix", Values: []string{"cn", "qq.com"}, Target: "direct"}, {Kind: "geoip", Values: []string{"cn"}, Target: "direct"}}, IntervalSec: 1800}
	return p, items
}
func TestPlanLayout(t *testing.T) {
	p, items := testPlan()
	l := p.layout([]string{"HK", "JP", "US"}, items)
	if len(l.Groups) != 3 || l.Groups[0].Name != "PROXY" || l.Groups[0].Kind != "select" || !reflect.DeepEqual(l.Groups[0].Members, []string{"自动选择", "主备", "HK"}) {
		t.Fatalf("布局=%+v", l.Groups)
	}
	if !reflect.DeepEqual(l.Groups[1].Members, []string{"HK", "US"}) || !l.Groups[1].Hidden || l.Groups[1].IntervalSec != 60 || l.Groups[1].ToleranceMS != 50 {
		t.Fatalf("自动选择=%+v", l.Groups[1])
	}
	if !l.Groups[2].Hidden || !reflect.DeepEqual(l.Groups[2].MemberRefs, []string{"chain:2", "node:3"}) {
		t.Fatal(l.Groups[2])
	}
	for i := range items {
		items[i].Auto = false
	}
	if got := p.layout([]string{"HK", "JP", "US"}, items).Groups[1].Members; !reflect.DeepEqual(got, []string{"HK", "US"}) {
		t.Fatal("全部 down 时必须只退回直连", got)
	}
	for i := range items {
		items[i].Direct = false
	}
	l = p.layout([]string{"HK", "JP", "US"}, items)
	if l.Groups[1].Ref == "auto" {
		t.Fatal("无直连时不能输出自动组")
	}
}

func TestEmptyPlanPreviewHasArrayMembers(t *testing.T) {
	groups, _ := (&Plan{}).Layout(nil, nil)
	b, err := json.Marshal(groups)
	if err != nil || !strings.Contains(string(b), `"members":[]`) || !strings.Contains(string(b), `"member_refs":[]`) {
		t.Fatalf("空布局应保留可供页面读取的成员数组：%s，%v", b, err)
	}
	if _, _, err := RenderPlan(nil, "mihomo", &Plan{}); err == nil {
		t.Fatal("空布局预览不能让空订阅成为有效配置")
	}
}
func TestHiddenLinesRestorePreference(t *testing.T) {
	p, items := testPlan()
	p.HiddenLines = []string{"node:3"}
	p.Groups = nil
	l := p.layout([]string{"HK", "JP", "US"}, items)
	if strings.Contains(strings.Join(l.Groups[0].Members, "|"), "US") {
		t.Fatal("关闭偏好丢失")
	}
	p.Groups = []contracts.ProxyGroup{{ID: 1, Name: "组", Kind: "select", Members: []string{"node:1"}}}
	l = p.layout([]string{"HK", "JP", "US"}, items)
	if reflect.DeepEqual(l.Groups[0].Members, []string{"自动选择", "组", "HK", "JP"}) {
		t.Fatal("组成员重复单列")
	}
}
func TestRenderPlanFormats(t *testing.T) {
	p, items := testPlan()
	for _, target := range []string{"clash", "mihomo", "singbox", "surge", "loon", "quanx", "egern", "base64"} {
		t.Run(target, func(t *testing.T) {
			b, _, err := RenderPlan(items, target, p)
			if err != nil {
				t.Fatal(err)
			}
			text := string(b)
			switch target {
			case "clash", "mihomo":
				var v map[string]any
				if yaml.Unmarshal(b, &v) != nil || !strings.Contains(text, "hidden: true") || !strings.Contains(text, "MATCH,PROXY") {
					t.Fatal(text)
				}
			case "singbox":
				var v map[string]any
				if json.Unmarshal(b, &v) != nil || !strings.Contains(text, "geoip-cn") || strings.Contains(text, `"type": "fallback"`) {
					t.Fatal(text)
				}
			case "surge":
				if !strings.Contains(text, "hidden=true") || !strings.Contains(text, "主备 = fallback") {
					t.Fatal(text)
				}
			case "loon":
				if strings.Contains(text, "hidden=") || !strings.Contains(text, "主备 = fallback") {
					t.Fatal(text)
				}
			case "quanx":
				if !strings.Contains(text, "available=主备") || strings.Contains(text, "hidden=") {
					t.Fatal(text)
				}
			case "egern":
				if !strings.Contains(text, "hidden: true") || !strings.Contains(text, "fallback:") {
					t.Fatal(text)
				}
			}
			if dir := os.Getenv("KERNEL_CHECK_DIR"); dir != "" && (target == "clash" || target == "singbox") {
				if err = os.WriteFile(filepath.Join(dir, "plan-"+target+".json"), b, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	for i := range items {
		items[i].Auto = false
		items[i].Direct = false
	}
	p.Groups = nil
	p.HiddenLines = []string{"node:1", "chain:2", "node:3"}
	for _, target := range []string{"clash", "singbox", "surge", "loon", "quanx", "egern"} {
		if _, _, err := RenderPlan(items, target, p); err == nil || !strings.Contains(err.Error(), "PROXY") {
			t.Fatal(target, "空 PROXY 未报错", err)
		}
	}
}
func TestClashProviders(t *testing.T) {
	p, items := testPlan()
	p.Providers = func(key string) string { return "https://center.example/sub/token/provider/" + key }
	b, _, err := RenderPlan(items, "mihomo", p)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"p-auto", "p-single", "p-group-1"} {
		if !strings.Contains(string(b), key) {
			t.Fatal("provider不稳定", key)
		}
	}
	for _, tt := range []struct {
		key   string
		count int
	}{{"auto", 2}, {"single", 1}, {"group-1", 2}} {
		b, err := ClashProvider(items, p, tt.key)
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Proxies []map[string]any `yaml:"proxies"`
		}
		if yaml.Unmarshal(b, &v) != nil || len(v.Proxies) != tt.count {
			t.Fatal(tt.key, string(b))
		}
	}
	if _, err = ClashProvider(items, p, "group-999"); err == nil {
		t.Fatal("不存在的provider不能输出凭据")
	}
}
func TestPlanMatch(t *testing.T) {
	p, items := testPlan()
	p.Custom = []Rule{{ID: "local", Kind: "ip_cidr", Values: []string{"192.168.1.0/24"}, Target: "proxy"}, {ID: "site", Kind: "domain", Values: []string{"qq.com"}, Target: "proxy"}}
	for _, target := range []string{"qq.com", "192.168.1.2"} {
		m := p.Match(target, []string{"HK", "JP", "US"}, items)
		if m.Policy != "PROXY" || m.EntryID == "" {
			t.Fatal("用户清单必须先于内置规则", m)
		}
	}
	m := p.Match("10.0.0.1", []string{"HK", "JP", "US"}, items)
	if m.Policy != "DIRECT" {
		t.Fatal(m)
	}
}
func TestPlanNameCollision(t *testing.T) {
	p, items := testPlan()
	items[0].Link = strings.Replace(items[0].Link, "#HK", "#PROXY", 1)
	b, _, err := RenderPlan(items, "clash", p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "PROXY (2)") {
		t.Fatal(string(b))
	}
}
