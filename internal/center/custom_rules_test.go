package center

import (
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestOptimizeCustomRules(t *testing.T) {
	res := optimizeCustomRules(strings.Join([]string{
		"# 注释", "payload:", "",
		"netflix.com", ".netflix.com", "+.Netflix.COM", "*.netflix.com", "www.netflix.com",
		"https://api.youtube.com:443/watch?v=1",
		"- DOMAIN-SUFFIX,google.com,PROXY",
		"DOMAIN,mail.google.com,DIRECT",
		"DOMAIN,exact.example.org",
		"DOMAIN-KEYWORD,OpenAI,Proxy",
		"chat.openai.example",
		"IP-CIDR,1.2.3.0/25,DIRECT,no-resolve",
		"IP-CIDR,1.2.3.128/25",
		"1.2.3.4",
		"IP-CIDR6,2001:db8::/32",
		"8.8.8.8",
		"中文.cn",
		"a.example，b.example、c.example",
		"||ads.example^",
		"0.0.0.0 tracker.example",
		"GEOSITE,netflix",
		"PROCESS-NAME,foo.exe,DIRECT",
		"乱码",
		"DOMAIN-SUFFIX,com",
		"0.0.0.0/0",
	}, "\n"))
	got := strings.Join(customLines(res.Rules), " ")
	for _, want := range []string{"DOMAIN-SUFFIX,xn--fiq228c.cn", "DOMAIN-KEYWORD,openai", "DOMAIN-SUFFIX,com", "IP-CIDR,0.0.0.0/0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	// 后缀 com、0.0.0.0/0 覆盖了同类的其他规则：单独验证精简时不带它们
	if len(res.Warnings) != 2 {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	if len(res.Unused) != 3 || res.Unused[0].Text != "GEOSITE,netflix" || res.Unused[0].Line != 24 || !strings.Contains(res.Unused[2].Reason, "乱码") {
		t.Fatalf("unused = %+v", res.Unused)
	}

	res = optimizeCustomRules(strings.Join([]string{
		"netflix.com", ".netflix.com", "www.netflix.com", "DOMAIN,netflix.com",
		"https://api.youtube.com:443/watch?v=1", "DOMAIN-KEYWORD,openai", "chat.openai.example",
		"IP-CIDR,1.2.3.0/25", "1.2.3.128/25", "1.2.3.4", "IP-CIDR6,2001:db8::/32",
		"a.example，b.example、c.example", "||ads.example^", "0.0.0.0 tracker.example",
	}, "\n"))
	got = strings.Join(customLines(res.Rules), " ")
	want := "DOMAIN-SUFFIX,netflix.com DOMAIN-SUFFIX,api.youtube.com DOMAIN-KEYWORD,openai DOMAIN-SUFFIX,a.example DOMAIN-SUFFIX,b.example DOMAIN-SUFFIX,c.example DOMAIN-SUFFIX,ads.example DOMAIN-SUFFIX,tracker.example IP-CIDR,1.2.3.0/24 IP-CIDR,2001:db8::/32"
	if got != want {
		t.Fatalf("rules:\n got %s\nwant %s", got, want)
	}
	if res.Total != 14 || res.Reduced != 16-10 || len(res.Unused) != 0 || len(res.Warnings) != 0 || res.Counts["domain_suffix"] != 7 || res.Counts["ip_cidr"] != 2 || res.Counts["domain_keyword"] != 1 {
		t.Fatalf("result = %+v", res)
	}

	// 什么都认不出也不是错误
	if res = optimizeCustomRules("???\nhello world"); len(res.Rules) != 0 || len(res.Unused) != 2 {
		t.Fatalf("garbage = %+v", res)
	}
}

func TestLegacyCustomRulesSubscription(t *testing.T) {
	s, admin, g := setupGate(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true}, 200)
	u := s.Store.Ent.User.GetX(t.Context(), 1)
	old := contracts.ProxyPolicy{Custom: contracts.CustomRules{Mode: "only", SourceMode: "bypass", Source: "failed.example", Lines: []string{"DOMAIN-SUFFIX,active.example"}, Check: &contracts.CustomCheck{Status: "failed", Detail: "bad candidate"}}}
	u.Update().SetProxyPolicy(policyMap(old)).ExecX(t.Context())
	view := mustJSON(t, admin, "GET", "/api/v1/me/proxy", nil, 200)
	custom := view["custom"].(map[string]any)
	if custom["pending"] == nil || custom["legacy_only"] != true || custom["entries"].([]any)[0].(map[string]any)["value"] != "active.example" {
		t.Fatalf("migration: %v", custom)
	}
	token := mustJSON(t, admin, "GET", "/api/v1/me/links", nil, 200)["items"].([]any)[0].(map[string]any)["token"].(string)
	body := admin.do("GET", "/sub/"+token, nil, map[string]string{"User-Agent": "clash.meta"}).Body.String()
	if !strings.Contains(body, "DOMAIN-SUFFIX,active.example,节点选择") || !strings.Contains(body, "MATCH,DIRECT") || strings.Contains(body, "RULE-SET,weir-custom") || strings.Contains(body, "failed.example") {
		t.Fatalf("subscription: %s", body)
	}
	if rules := admin.do("GET", "/sub/"+token+"/rules", nil, nil).Body.String(); rules != "DOMAIN-SUFFIX,active.example\n" {
		t.Fatalf("legacy snapshot: %q", rules)
	}
	mustJSON(t, admin, "PUT", "/api/v1/me/proxy/custom", map[string]any{"mode": "bypass", "source": "unversioned.example"}, 400)
}
