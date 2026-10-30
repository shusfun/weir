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
