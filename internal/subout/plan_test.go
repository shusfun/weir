package subout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func testPlan() (*Plan, []Item) {
	items := []Item{
		{Link: "vless://11111111-1111-4111-8111-111111111111@203.0.113.1:443?type=tcp&security=reality&pbk=Q7m3S0fKOdP5q3kQb6r2ZQqYQ3x9R7v0o2JtJb1s6lA&sid=ab12&sni=www.microsoft.com&fp=chrome&flow=xtls-rprx-vision&encryption=none#HK%20REALITY", Type: "vless-reality", Auto: true},
		{Link: "trojan://pass1@203.0.113.2:443?type=tcp&security=tls&sni=203.0.113.2#JP%20Trojan", Type: "trojan-tls", Auto: false},
		{Link: "trojan://pass2@203.0.113.3:443?type=tcp&security=tls&sni=203.0.113.3#US%20Trojan", Type: "trojan-tls", Auto: true},
	}
	plan := &Plan{
		Types: []TypeGroup{{"vless-reality", "VLESS REALITY"}, {"trojan-tls", "Trojan"}, {"hysteria2", "Hysteria2"}},
		Rules: []Rule{
			{Kind: "domain_suffix", Values: []string{"openai.com", "claude.ai"}, Target: "auto"},
			{Kind: "domain_suffix", Values: []string{"netflix.com"}, Target: "trojan-tls"},
			{Kind: "domain_suffix", Values: []string{"example.cn"}, Target: "hysteria2"}, // 没有成员的类型改走节点选择
			{Kind: "domain_suffix", Values: []string{"qq.com"}, Target: "direct"},
			{Kind: "geoip", Values: []string{"cn"}, Target: "direct"},
		},
		Final: "select", IntervalSec: 1800,
	}
	return plan, items
}

func TestPlanLayout(t *testing.T) {
	plan, items := testPlan()
	names := []string{"HK REALITY", "JP Trojan", "US Trojan"}
	lay := plan.layout(names, items)
	var got []string
	for _, g := range lay.Groups {
		got = append(got, g.Name+":"+g.Kind+":"+strings.Join(g.Members, "|"))
	}
	want := []string{
		"节点选择:select:自动选择|VLESS REALITY|Trojan|HK REALITY|JP Trojan|US Trojan|DIRECT",
		"自动选择:url-test:HK REALITY|US Trojan",
		"VLESS REALITY:url-test:HK REALITY",
		"Trojan:url-test:JP Trojan|US Trojan",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("groups:\n%s", strings.Join(got, "\n"))
	}
	pol := map[string]string{}
	for _, r := range lay.Rules {
		pol[r.Values[0]] = r.Policy
	}
	if pol["openai.com"] != "自动选择" || pol["netflix.com"] != "Trojan" || pol["example.cn"] != "节点选择" || pol["qq.com"] != "DIRECT" || pol["cn"] != "DIRECT" || lay.Final != "节点选择" {
		t.Fatalf("rules: %+v final %s", pol, lay.Final)
	}
}

// TestRenderPlanFormats 每种格式都能按计划生成；KERNEL_CHECK_DIR 设置时把 Clash 与 sing-box 的结果写出来，
// 由 e2e 环境里的真实内核检查（见 deploy/e2e.py）。
func TestRenderPlanFormats(t *testing.T) {
	plan, items := testPlan()
	dir := os.Getenv("KERNEL_CHECK_DIR")
	for _, target := range []string{"clash", "singbox", "loon", "quanx", "egern", "base64"} {
		use := items
		if target == "quanx" || target == "egern" {
			use = items[1:] // QuanX、Egern 表达不了 REALITY 的指纹
		}
		body, _, err := RenderPlan(use, target, plan)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		text := string(body)
		switch target {
		case "clash":
			var cfg map[string]any
			if err := yaml.Unmarshal(body, &cfg); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(text, "DOMAIN-SUFFIX,netflix.com,Trojan") || !strings.Contains(text, "MATCH,节点选择") || !strings.Contains(text, "GEOIP,CN,DIRECT") {
				t.Fatalf("clash rules:\n%s", text)
			}
		case "singbox":
			var cfg map[string]any
			if err := json.Unmarshal(body, &cfg); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(text, `"type": "urltest"`) || strings.Contains(text, "geoip") {
				t.Fatalf("singbox:\n%s", text)
			}
		case "loon":
			if !strings.Contains(text, "Trojan = url-test, JP Trojan, US Trojan") || !strings.Contains(text, "FINAL, 节点选择") {
				t.Fatalf("loon:\n%s", text)
			}
		case "quanx":
			if !strings.Contains(text, "url-latency-benchmark=Trojan, JP Trojan, US Trojan") || !strings.Contains(text, "host-suffix, qq.com, direct") {
				t.Fatalf("quanx:\n%s", text)
			}
		case "egern":
			if !strings.Contains(text, "auto_test:") {
				t.Fatalf("egern:\n%s", text)
			}
		}
		if dir != "" && (target == "clash" || target == "singbox") {
			ext := map[string]string{"clash": "yaml", "singbox": "json"}[target]
			if err := os.WriteFile(filepath.Join(dir, "plan."+ext), body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func customPlanForTest() (*Plan, []Item) {
	plan, items := testPlan()
	plan.CustomTarget = "direct"
	plan.Custom = []Rule{
		{Kind: "domain", Values: []string{"exact.example"}},
		{Kind: "domain_suffix", Values: []string{"custom.example"}},
		{Kind: "domain_keyword", Values: []string{"customword"}},
		{Kind: "ip_cidr", Values: []string{"198.51.100.0/24", "2001:db8::/32"}},
	}
	return plan, items
}

// 自定义清单在每种格式里都排在类别之前，四种规则都有对应写法；KERNEL_CHECK_DIR 设置时写出来给真实内核检查。
func TestRenderCustomRules(t *testing.T) {
	plan, items := customPlanForTest()
	dir := os.Getenv("KERNEL_CHECK_DIR")
	for target, want := range map[string][]string{
		"clash":   {"DOMAIN,exact.example,DIRECT", "DOMAIN-SUFFIX,custom.example,DIRECT", "DOMAIN-KEYWORD,customword,DIRECT", "IP-CIDR,198.51.100.0/24,DIRECT,no-resolve", "IP-CIDR,2001:db8::/32,DIRECT,no-resolve"},
		"singbox": {`"domain": [`, `"custom.example"`, `"domain_keyword": [`, `"198.51.100.0/24"`},
		"loon":    {"DOMAIN, exact.example, DIRECT", "DOMAIN-SUFFIX, custom.example, DIRECT", "DOMAIN-KEYWORD, customword, DIRECT", "IP-CIDR, 198.51.100.0/24, DIRECT, no-resolve", "IP-CIDR6, 2001:db8::/32, DIRECT, no-resolve"},
		"quanx":   {"host, exact.example, direct", "host-suffix, custom.example, direct", "host-keyword, customword, direct", "ip-cidr, 198.51.100.0/24, direct", "ip6-cidr, 2001:db8::/32, direct"},
		"egern":   {"domain:", "domain_suffix:", "domain_keyword:", "198.51.100.0/24"},
	} {
		use := items
		if target == "quanx" || target == "egern" {
			use = items[1:]
		}
		body, _, err := RenderPlan(use, target, plan)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		text := string(body)
		for _, w := range want {
			if !strings.Contains(text, w) {
				t.Fatalf("%s missing %q:\n%s", target, w, text)
			}
		}
		if i, j := strings.Index(text, "custom.example"), strings.Index(text, "openai.com"); i < 0 || j < 0 || i > j {
			t.Fatalf("%s: custom rules must come first:\n%s", target, text)
		}
		if dir != "" && (target == "clash" || target == "singbox") {
			ext := map[string]string{"clash": "yaml", "singbox": "json"}[target]
			if err := os.WriteFile(filepath.Join(dir, "plan-custom."+ext), body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Clash 设了 RuleSet：清单不内联，只引用 rule-providers，紧跟内网规则；走向由 CustomTarget 决定。
func TestClashCustomRuleSet(t *testing.T) {
	plan, items := customPlanForTest()
	plan.CustomTarget = "select"
	plan.RuleSet = func() string { return "https://center.example/sub/tok/rules" }
	body, _, err := RenderPlan(items, "clash", plan)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Rules     []string       `yaml:"rules"`
		Providers map[string]any `yaml:"rule-providers"`
	}
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	p, _ := cfg.Providers[CustomRuleSetName].(map[string]any)
	if p["url"] != "https://center.example/sub/tok/rules" || p["behavior"] != "classical" || p["format"] != "text" || p["interval"] != CustomRuleSetSec {
		t.Fatalf("provider = %v", cfg.Providers)
	}
	if cfg.Rules[len(privateCIDRs)] != "RULE-SET,"+CustomRuleSetName+",节点选择" || strings.Contains(string(body), "custom.example") {
		t.Fatalf("rules:\n%s", body)
	}
}

// Match 的顺序与渲染一致：内网 → 清单 → 类别 → 其余流量；域名不解析，排在命中之前的 IP 规则列为可能先命中。
func TestPlanMatch(t *testing.T) {
	plan, items := customPlanForTest()
	names := []string{"HK REALITY", "JP Trojan", "US Trojan"}
	for _, c := range []struct{ target, label, kind, value, policy string }{
		{"10.1.2.3", "内网地址", "ip_cidr", "10.0.0.0/8", "DIRECT"},
		{"exact.example", CustomLabel, "domain", "exact.example", "DIRECT"},
		{"sub.exact.example", "其余流量", "final", "", GroupSelect},
		{"a.custom.example", CustomLabel, "domain_suffix", "custom.example", "DIRECT"},
		{"notcustom.example", "其余流量", "final", "", GroupSelect},
		{"www.customword.net", CustomLabel, "domain_keyword", "customword", "DIRECT"},
		{"198.51.100.7", CustomLabel, "ip_cidr", "198.51.100.0/24", "DIRECT"},
		{"2001:db8::1", CustomLabel, "ip_cidr", "2001:db8::/32", "DIRECT"},
		{"chat.openai.com", "", "domain_suffix", "openai.com", GroupAuto},
	} {
		m := plan.Match(c.target, names, items)
		if m.Label != c.label || m.Kind != c.kind || m.Value != c.value || m.Policy != c.policy {
			t.Fatalf("%s = %+v", c.target, m)
		}
	}
	// 清单排在前面：域名先命中清单时，后面类别里的 GeoIP 不算；没命中时列出来
	if m := plan.Match("a.custom.example", names, items); len(m.Maybe) != 0 {
		t.Fatalf("maybe = %+v", m.Maybe)
	}
	if m := plan.Match("chat.openai.com", names, items); len(m.Maybe) != 1 || m.Maybe[0].Kind != "ip_cidr" {
		t.Fatalf("maybe = %+v", m.Maybe)
	}
	if m := plan.Match("unmatched.example", names, items); len(m.Maybe) != 2 || m.Maybe[1].Kind != "geoip" {
		t.Fatalf("maybe = %+v", m.Maybe)
	}
}

func TestClashProviders(t *testing.T) {
	plan, items := testPlan()
	plan.Providers = func(key string) string { return "https://center.example/sub/tok/provider/" + key }
	body, _, err := RenderPlan(items, "clash", plan)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "\nproxies:") || !strings.Contains(text, "proxy-providers:") || !strings.Contains(text, "/provider/auto") || !strings.Contains(text, "/provider/trojan-tls") {
		t.Fatalf("providers:\n%s", text)
	}
	if strings.Contains(text, "/provider/hysteria2") {
		t.Fatal("empty type must not get a provider")
	}
	auto, err := ClashProvider(items, plan, AutoProvider)
	if err != nil || !strings.Contains(string(auto), "HK REALITY") || !strings.Contains(string(auto), "US Trojan") || strings.Contains(string(auto), "JP Trojan") {
		t.Fatalf("auto provider: %v\n%s", err, auto)
	}
	typ, err := ClashProvider(items, plan, "trojan-tls")
	if err != nil || strings.Contains(string(typ), "HK REALITY") || !strings.Contains(string(typ), "JP Trojan") {
		t.Fatalf("type provider: %v\n%s", err, typ)
	}
}

// 节点名与组名相同时节点改名，组名保持不变。
func TestPlanNameCollision(t *testing.T) {
	plan, items := testPlan()
	items[0].Link = strings.Replace(items[0].Link, "#HK%20REALITY", "#Trojan", 1)
	body, _, err := RenderPlan(items, "clash", plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Trojan (2)") {
		t.Fatalf("collision not renamed:\n%s", body)
	}
}
