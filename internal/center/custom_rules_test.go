package center

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/subout"
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

// 保存、检查的三种结果、测试清单、rule-providers、与其他设置互不影响、权限。
func TestCustomRulesFlow(t *testing.T) {
	s, admin, g := setupGate(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true}, 200)
	created := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "cat@example.test", "password": "Passw0rd-cat", "all_nodes": true}, 200)
	uid := int(created["id"].(float64))
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "eve@example.test", "password": "Passw0rd-eve", "all_nodes": true}, 200)
	cat, eve := login(t, s, "cat@example.test", "Passw0rd-cat"), login(t, s, "eve@example.test", "Passw0rd-eve")
	tok := mustJSON(t, cat, "GET", "/api/v1/me/links", nil, 200)["items"].([]any)[0].(map[string]any)["token"].(string)
	clashUA := map[string]string{"User-Agent": "clash-verge/v2.0"}
	clash := func() string { return cat.do("GET", "/sub/"+tok, nil, clashUA).Body.String() }

	// 默认：Clash 也引用清单，内容是不会命中的占位
	if text := clash(); !strings.Contains(text, "RULE-SET,weir-custom,DIRECT") || !strings.Contains(text, "/sub/"+tok+"/rules") {
		t.Fatalf("default clash:\n%s", text)
	}
	if w := cat.do("GET", "/sub/"+tok+"/rules", nil, clashUA); w.Code != 200 || w.Body.String() != customEmptyRule+"\n" {
		t.Fatalf("empty rules = %d %q", w.Code, w.Body.String())
	}
	if w := cat.do("GET", "/sub/"+strings.Repeat("0", 48)+"/rules", nil, nil); w.Code != 404 {
		t.Fatalf("unknown token = %d", w.Code)
	}

	t.Cleanup(func() { subCheckBinary, subCheckRun = defaultSubCheckBinary, runSubCheck })
	// 未知：内核不可用时生效，并如实显示
	subCheckBinary = func(*Server, context.Context, string) (string, string, error) {
		return "", "", errors.New("内核尚未下载")
	}
	src := "netflix.com\nGEOSITE,x\n1.2.3.4"
	view := mustJSON(t, cat, "PUT", "/api/v1/me/proxy/custom", map[string]any{"mode": "bypass", "source": src}, 200)
	custom := view["custom"].(map[string]any)
	check := custom["check"].(map[string]any)
	if check["status"] != "unknown" || !strings.Contains(check["detail"].(string), "内核尚未下载") || custom["active_mode"] != "bypass" || custom["source"] != src {
		t.Fatalf("unknown check = %v", custom)
	}
	record := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("proxy.custom_rules")).FirstX(t.Context())
	traceDetail := record.Detail
	if traceDetail["trace_id"] == "" || traceDetail["request_id"] == "" || record.Actor != uid {
		t.Fatalf("custom rules audit trace = %+v detail=%v", record, traceDetail)
	}
	if w := cat.do("GET", "/sub/"+tok+"/rules", nil, clashUA); w.Body.String() != "DOMAIN-SUFFIX,netflix.com\nIP-CIDR,1.2.3.4/32,no-resolve\n" {
		t.Fatalf("rules = %q", w.Body.String())
	}
	if text := clash(); !strings.Contains(text, "RULE-SET,weir-custom,DIRECT") || strings.Contains(text, "DOMAIN-SUFFIX,netflix.com,DIRECT") {
		t.Fatalf("clash must use the rule set:\n%s", text)
	}
	// 其他格式内联，排在类别之前
	singbox := cat.do("GET", "/sub/"+tok, nil, map[string]string{"User-Agent": "SFA/1.12"}).Body.String()
	if i, j := strings.Index(singbox, `"netflix.com"`), strings.Index(singbox, `"openai.com"`); i < 0 || j < 0 || i > j {
		t.Fatalf("sing-box custom rule must come first:\n%s", singbox)
	}

	// 改类型开关与分流不会冲掉清单
	mustJSON(t, cat, "PUT", "/api/v1/me/proxy", map[string]any{"rules": map[string]string{"ai": "select"}}, 200)
	view = mustJSON(t, cat, "GET", "/api/v1/me/proxy", nil, 200)
	if c := view["custom"].(map[string]any); c["source"] != src || c["active_mode"] != "bypass" {
		t.Fatalf("custom lost after PUT /proxy: %v", c)
	}
	if _, leaked := view["policy"].(map[string]any)["custom"]; leaked {
		t.Fatalf("policy must not carry the list: %v", view["policy"])
	}

	// 测试清单：用没保存的内容计算，不写入
	testIt := func(target, mode, source string) map[string]any {
		return mustJSON(t, cat, "POST", "/api/v1/me/proxy/custom/test", map[string]any{"target": target, "mode": mode, "source": source}, 200)["match"].(map[string]any)
	}
	if m := testIt("https://WWW.Netflix.com/x", "bypass", "netflix.com"); m["label"] != subout.CustomLabel || m["kind"] != "domain_suffix" || m["value"] != "netflix.com" || m["policy"] != "DIRECT" {
		t.Fatalf("custom hit = %v", m)
	}
	if m := testIt("www.netflix.com", "only", "youtube.com"); m["label"] != "其余流量" || m["policy"] != "DIRECT" {
		t.Fatalf("only miss = %v", m)
	}
	if m := testIt("www.youtube.com", "only", "youtube.com"); m["policy"] != subout.GroupSelect {
		t.Fatalf("only hit = %v", m)
	}
	if m := testIt("www.netflix.com", "off", "netflix.com"); m["label"] != "流媒体" || m["kind"] != "domain_suffix" {
		t.Fatalf("category hit = %v", m)
	}
	if m := testIt("api.telegram.org", "off", ""); m["label"] != "Telegram" {
		t.Fatalf("telegram = %v", m)
	}
	if m := testIt("unmatched.example", "bypass", "1.2.3.0/24"); m["label"] != "其余流量" || len(m["maybe"].([]any)) < 2 {
		t.Fatalf("ip rules must be listed as maybe: %v", m)
	}
	if m := testIt("1.2.3.9", "bypass", "1.2.3.0/24"); m["kind"] != "ip_cidr" || m["value"] != "1.2.3.0/24" {
		t.Fatalf("ip hit = %v", m)
	}
	if m := testIt("192.168.1.1", "only", ""); m["label"] != "内网地址" {
		t.Fatalf("private = %v", m)
	}
	mustJSON(t, cat, "POST", "/api/v1/me/proxy/custom/test", map[string]any{"target": "不是地址", "mode": "off"}, 400)
	if c := mustJSON(t, cat, "GET", "/api/v1/me/proxy", nil, 200)["custom"].(map[string]any); c["source"] != src {
		t.Fatalf("test must not write: %v", c)
	}

	// 通过：生效；Clash 规则集改走节点选择，其余直连，类别不再输出
	subCheckBinary = func(*Server, context.Context, string) (string, string, error) { return "checker", "1.0.0", nil }
	subCheckRun = func(context.Context, string, string, string, []byte) ([]byte, error) { return nil, nil }
	view = mustJSON(t, cat, "PUT", "/api/v1/me/proxy/custom", map[string]any{"mode": "only", "source": "youtube.com"}, 200)
	if c := view["custom"].(map[string]any); c["check"].(map[string]any)["status"] != "passed" || c["active_mode"] != "only" {
		t.Fatalf("passed = %v", c)
	}
	text := clash()
	if !strings.Contains(text, "RULE-SET,weir-custom,节点选择") || !strings.Contains(text, "MATCH,DIRECT") || strings.Contains(text, "DOMAIN-SUFFIX,openai.com") {
		t.Fatalf("only clash:\n%s", text)
	}

	// 失败：原文保留、显示真实报错，客户端继续用上一次的规则
	subCheckRun = func(context.Context, string, string, string, []byte) ([]byte, error) {
		return []byte("bad rule from kernel"), errors.New("exit status 1")
	}
	if w := cat.do("PUT", "/api/v1/me/proxy/custom", map[string]any{"mode": "bypass", "source": "changed.example"}, nil); w.Code != 422 || !strings.Contains(w.Body.String(), "bad rule from kernel") {
		t.Fatalf("failed check = %d %s", w.Code, w.Body.String())
	}
	custom2 := mustJSON(t, cat, "GET", "/api/v1/me/proxy", nil, 200)["custom"].(map[string]any)
	if custom2["source"] != "changed.example" || custom2["mode"] != "bypass" || custom2["active_mode"] != "only" || custom2["active_count"].(float64) != 1 || !strings.Contains(custom2["check"].(map[string]any)["detail"].(string), "bad rule from kernel") {
		t.Fatalf("failed check view = %v", custom2)
	}
	if w := cat.do("GET", "/sub/"+tok+"/rules", nil, clashUA); w.Body.String() != "DOMAIN-SUFFIX,youtube.com\n" {
		t.Fatalf("failed check must keep the previous rules: %q", w.Body.String())
	}

	// 内容过大直接拒绝；不认识的方式拒绝
	mustJSON(t, cat, "POST", "/api/v1/me/proxy/custom/test", map[string]any{"target": "a.example", "source": strings.Repeat("x", customMaxSource+1)}, 400)
	mustJSON(t, cat, "POST", "/api/v1/me/proxy/custom/test", map[string]any{"target": "a.example", "mode": "nope"}, 400)

	// 权限：普通用户动不了别人的清单；管理员可以
	if w := eve.do("PUT", "/api/v1/admin/users/"+itoa(uid)+"/proxy/custom", map[string]any{"mode": "off"}, nil); w.Code != 403 {
		t.Fatalf("other user PUT = %d", w.Code)
	}
	if w := eve.do("POST", "/api/v1/admin/users/"+itoa(uid)+"/proxy/custom/test", map[string]any{"target": "a.example"}, nil); w.Code != 403 {
		t.Fatalf("other user test = %d", w.Code)
	}
	var evePol map[string]any
	json.Unmarshal(eve.do("GET", "/api/v1/me/proxy", nil, nil).Body.Bytes(), &evePol)
	if evePol["custom"].(map[string]any)["source"] != "" {
		t.Fatalf("eve sees a list: %v", evePol["custom"])
	}
}
