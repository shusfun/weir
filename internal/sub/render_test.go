package sub

import (
	"strings"
	"testing"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

func strOrNull(out string, ok bool) any {
	if !ok {
		return js.Null
	}
	return out
}

func nodeListFixture() string {
	return "# 注释行\n" + strings.Join(sampleLinks, "\n") + "\n\n" + sampleLinks[0] + "\n"
}

func generatorOptionSets(t *testing.T) []string {
	return []string{
		`{}`,
		`{"ruleLevel": "base"}`,
		`{"ruleLevel": "full", "enableUdp": true, "enableTfo": true, "skipCertVerify": true, "managedConfigUrl": "https://sub.example.com/s?token=x", "interval": 3600}`,
		`{"ruleLevel": "relay", "isMeta": true}`,
		`{"ruleLevel": "relay", "userAgent": "Stash/2.4", "managedConfigUrl": "https://sub.example.com/s"}`,
		`{"ruleLevel": "std", "userAgent": "Hiddify/2.0 ClashMeta"}`,
		`{"ruleLevel": "xyz", "customDnsOverride": "nameserver:\n  - https://dns.google/dns-query\n  - 10.0.0.1\ndefault-nameserver: [114.114.114.114]\nfake-ip-range: 28.0.0.1/8\n", "dnsMode": "polluted"}`,
		`{"customDnsOverride": "dns:\n  enable: true\n  nameserver: [1.1.1.1]\n", "addFlagEmoji": false}`,
		`{"subscriptionDnsPolicies": [{"domains": "+.corp.example", "nameservers": ["10.1.1.1"]}, {"policy": {"geosite:apple": ["223.5.5.5"]}}], "supportX25519mlkem768": true}`,
	}
}

func TestBuiltinGeneratorParity(t *testing.T) {
	nodeList := nodeListFixture()
	targets := []string{"clash", "surge", "surge&ver=4", "loon", "quanx", "singbox", "sing-box", "egern", "base64"}
	var cases []oracleCase
	for _, target := range targets {
		for i, raw := range generatorOptionSets(t) {
			target, raw := target, raw
			name := "transformBuiltinSubscription/" + target + "/" + js.NumberToString(float64(i))
			cases = append(cases, oracleCase{Name: name, Mod: "modules/subscription/transformer-factory.js", Fn: "transformBuiltinSubscription",
				Args: []any{nodeList, target, mustJSON(t, raw)},
				Go: func() (any, error) {
					out, ok, err := TransformBuiltinSubscription(nodeList, target, mustJSON(t, raw).(*js.Object))
					if err != nil {
						return nil, err
					}
					return strOrNull(out, ok), nil
				}})
		}
	}
	for _, target := range []string{"clash", "surge", "loon", "quanx", "singbox", "egern"} {
		target := target
		cases = append(cases, oracleCase{Name: "transformBuiltinSubscription/empty/" + target, Mod: "modules/subscription/transformer-factory.js",
			Fn: "transformBuiltinSubscription", Args: []any{"# 没有节点\n", target, js.NewObject()},
			Go: func() (any, error) {
				out, ok, err := TransformBuiltinSubscription("# 没有节点\n", target, js.NewObject())
				return strOrNull(out, ok), err
			}})
	}
	cases = append(cases, oracleCase{Name: "generateProxiesOnly", Mod: "modules/subscription/builtin-clash-generator.js", Fn: "generateProxiesOnly",
		Args: []any{nodeList}, Go: func() (any, error) { return GenerateProxiesOnly(nodeList), nil }})
	runOracle(t, cases)
}

// customINIFixture 覆盖 [Proxy Group] / [Rule] 段、正则过滤、魔法占位符与链式代理声明。
const customINIFixture = `; 自定义模板
[custom]
ruleset=🎯 全球直连,https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/ChinaIp.list
ruleset=🛑 广告拦截,clash-classic:https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/BanProgramAD.list
ruleset=🍎 苹果,https://cdn.jsdelivr.net/gh/ACL4SSR/ACL4SSR@master/Clash/Ruleset/Apple.list
ruleset=🧪 测试,https://example.com/rules/custom.yaml
ruleset=🧪 测试,https://example.com/rules/geo.srs
ruleset=🧪 测试,[]DOMAIN-KEYWORD,test,no-resolve
ruleset=🎯 全球直连,[]GEOSITE,cn
ruleset=🐟 漏网之鱼,[]MATCH
custom_proxy_group=🚀 节点选择` + "`select`[]♻️ 自动选择`[]<%regionStrategyChain%>`[]<%protocolStrategyChain%>`[]🔗 落地`[]DIRECT" + `
custom_proxy_group=♻️ 自动选择` + "`url-test`(港|日|美)`http://www.gstatic.com/generate_204`300,,50" + `
custom_proxy_group=⚖️ 负载` + "`load-balance`.*`http://cp.cloudflare.com`600" + `
custom_proxy_group=🔀 回退` + "`fallback`[]♻️ 自动选择`[]DIRECT`HK|JP" + `
custom_proxy_group=🧪 测试` + "`select`[]🚀 节点选择`[]不存在的组`IPLC+`,`a=b" + `
custom_proxy_group=🔗 落地` + "`select`[]DIRECT" + `
custom_proxy_group=🛫 前置` + "`select`.*" + `
custom_chain_proxy=🔗 落地` + "`🛫 前置`CHAIN: " + `
custom_chain_proxy=🔗 落地` + "`🛫 前置" + `
custom_chain_proxy=bad
[Proxy Group]
手动组 = select, DIRECT, 🚀 节点选择, interval=300
空组 = url-test
[Rule]
DOMAIN-SUFFIX,example.com,🚀 节点选择
IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
GEOIP,US,🚀 节点选择
FINAL,🐟 漏网之鱼
MATCH
`

func iniOptionSets() []string {
	return []string{
		`{"ruleLevel": "std"}`,
		`{"ruleLevel": "base", "isMeta": true, "managedConfigUrl": "https://sub.example.com/x", "interval": 7200, "fileName": "测试"}`,
		`{"ruleLevel": "full", "isMeta": true, "enableUdp": true, "customDnsOverride": "nameserver: [https://1.1.1.1/dns-query]", "dnsMode": "polluted"}`,
		`{"ruleLevel": "none", "skipCertVerify": true, "addFlagEmoji": false}`,
		`{"ruleLevel": ""}`,
	}
}

func TestTemplatePipelineParity(t *testing.T) {
	nodeList := nodeListFixture()
	templates := map[string]string{"custom": customINIFixture, "empty": ""}
	for _, id := range BuiltinTemplateIDs {
		templates[id] = GetBuiltinTemplate(id).Content
	}
	type renderer struct {
		fn  string
		go_ func(tpl string, opts *js.Object) (string, error)
	}
	renderers := []renderer{
		{"renderClashFromIniTemplate", func(tpl string, o *js.Object) (string, error) { return RenderClashFromIniTemplate(tpl, o) }},
		{"renderSingboxFromIniTemplate", func(tpl string, o *js.Object) (string, error) { return RenderSingboxFromIniTemplate(tpl, o), nil }},
		{"renderSurgeFromIniTemplate", func(tpl string, o *js.Object) (string, error) { return RenderSurgeFromIniTemplate(tpl, o), nil }},
		{"renderLoonFromIniTemplate", func(tpl string, o *js.Object) (string, error) { return RenderLoonFromIniTemplate(tpl, o), nil }},
		{"renderQuanxFromIniTemplate", func(tpl string, o *js.Object) (string, error) { return RenderQuanxFromIniTemplate(tpl, o), nil }},
		{"renderEgernFromIniTemplate", func(tpl string, o *js.Object) (string, error) { return RenderEgernFromIniTemplate(tpl, o) }},
	}
	var cases []oracleCase
	for tplName, tpl := range templates {
		for _, r := range renderers {
			for i, raw := range iniOptionSets() {
				tpl, r, raw := tpl, r, raw
				opts := func() *js.Object {
					o := mustJSON(t, raw).(*js.Object)
					o.Set("nodeList", nodeList)
					return o
				}
				cases = append(cases, oracleCase{Name: r.fn + "/" + tplName + "/" + js.NumberToString(float64(i)),
					Mod: "modules/subscription/template-pipeline.js", Fn: r.fn, Args: []any{tpl, opts()},
					Go: func() (any, error) { return r.go_(tpl, opts()) }})
			}
		}
	}
	runOracle(t, cases)
}

func TestTemplateModelParity(t *testing.T) {
	proxies := func() *js.Array { return objs(URLsToClashProxies(sampleLinks, nil)) }
	model := func(raw string) *js.Object {
		o := mustJSON(t, raw).(*js.Object)
		o.Set("proxies", proxies())
		return ParseIniTemplate(customINIFixture, o)
	}
	var cases []oracleCase
	for i, raw := range iniOptionSets() {
		raw := raw
		n := js.NumberToString(float64(i))
		cases = append(cases, oracleCase{Name: "parseIniTemplate/" + n, Mod: "modules/subscription/template-parsers/ini-template-parser.js",
			Fn: "parseIniTemplate", Args: []any{customINIFixture, func() *js.Object { o := mustJSON(t, raw).(*js.Object); o.Set("proxies", proxies()); return o }()},
			Go: func() (any, error) { return model(raw), nil }})
		cases = append(cases, oracleCase{Name: "applySmartModelOptimizations/" + n, Mod: "modules/subscription/template-processor.js",
			Fn: "applySmartModelOptimizations", Args: []any{model(raw)},
			Go: func() (any, error) { return ApplySmartModelOptimizations(model(raw)), nil }})
		for _, fn := range []string{"renderClashFromTemplateModel", "renderSingboxFromTemplateModel", "renderSurgeFromTemplateModel",
			"renderLoonFromTemplateModel", "renderQuanxFromTemplateModel", "renderEgernFromTemplateModel"} {
			fn := fn
			mod := "modules/subscription/template-renderers/render-" + strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(fn, "render"), "FromTemplateModel")) + ".js"
			cases = append(cases, oracleCase{Name: fn + "/" + n, Mod: mod, Fn: fn, Args: []any{model(raw)},
				Go: func() (any, error) {
					m := model(raw)
					switch fn {
					case "renderClashFromTemplateModel":
						return RenderClashFromTemplateModel(m)
					case "renderSingboxFromTemplateModel":
						return RenderSingboxFromTemplateModel(m, nil), nil
					case "renderSurgeFromTemplateModel":
						return RenderSurgeFromTemplateModel(m, nil), nil
					case "renderLoonFromTemplateModel":
						return RenderLoonFromTemplateModel(m, nil), nil
					case "renderQuanxFromTemplateModel":
						return RenderQuanxFromTemplateModel(m, nil), nil
					}
					return RenderEgernFromTemplateModel(m)
				}})
		}
	}
	// 模型中没有节点时，渲染器按 options.nodeList 现场转换
	emptyModel := func() *js.Object { return ParseIniTemplate(customINIFixture, js.NewObject()) }
	nl := js.Obj("nodeList", nodeListFixture())
	cases = append(cases,
		oracleCase{Name: "renderSurge/nodeList", Mod: "modules/subscription/template-renderers/render-surge.js", Fn: "renderSurgeFromTemplateModel",
			Args: []any{emptyModel(), nl}, Go: func() (any, error) { return RenderSurgeFromTemplateModel(emptyModel(), nl), nil }},
		oracleCase{Name: "renderSingbox/nodeList", Mod: "modules/subscription/template-renderers/render-singbox.js", Fn: "renderSingboxFromTemplateModel",
			Args: []any{emptyModel(), nl}, Go: func() (any, error) { return RenderSingboxFromTemplateModel(emptyModel(), nl), nil }},
		oracleCase{Name: "createUnifiedTemplateModel/garbage", Mod: "modules/subscription/template-model.js", Fn: "createUnifiedTemplateModel",
			Args: []any{js.Obj("meta", "x", "proxies", 5.0, "settings", js.Obj("interval", 0.0, "enableUdp", 1.0), "extras", strs("a"))},
			Go: func() (any, error) {
				return CreateUnifiedTemplateModel(js.Obj("meta", "x", "proxies", 5.0, "settings", js.Obj("interval", 0.0, "enableUdp", 1.0), "extras", strs("a"))), nil
			}},
	)
	for _, meta := range []bool{true, false} {
		meta := meta
		cases = append(cases, oracleCase{Name: "applyMihomoChainProxy/" + js.ToString(meta), Mod: "modules/subscription/mihomo-chain-proxy.js",
			Fn: "applyMihomoChainProxy", Args: []any{model(`{}`), js.Obj("enableDialerProxy", meta)},
			Go: func() (any, error) { return ApplyMihomoChainProxy(model(`{}`), meta), nil }})
	}
	runOracle(t, cases)
}

func TestRulesAndDNSParity(t *testing.T) {
	var cases []oracleCase
	add := func(c oracleCase) { cases = append(cases, c) }
	const rp = "modules/subscription/builtin-rules-provider.js"
	for _, level := range []string{"base", "STD", "full", "relay", "unknown"} {
		for _, format := range []string{"clash", "singbox", "sing-box", "surge", "loon", "quanx", "other"} {
			level, format := level, format
			add(oracleCase{Name: "getBuiltinRules/" + level + "/" + format, Mod: rp, Fn: "getBuiltinRules", Args: []any{level, format},
				Go: func() (any, error) { return anys(GetBuiltinRules(level, format)), nil }})
			add(oracleCase{Name: "getRemoteProviderDefinitions/" + level + "/" + format, Mod: rp, Fn: "getRemoteProviderDefinitions",
				Args: []any{format, anys(GetBuiltinRules(level, format))},
				Go:   func() (any, error) { return GetRemoteProviderDefinitions(format, GetBuiltinRules(level, format)), nil }})
		}
	}
	for _, line := range []string{"RULE-SET,UNKNOWN,x", "DOMAIN,a.com,P", "IP-CIDR,1.0.0.0/8,P,no-resolve", "MATCH,P", "GEOIP,CN,DIRECT", "PROCESS-NAME,x,P"} {
		for _, format := range []string{"clash", "singbox", "surge", "quanx", "x"} {
			line, format := line, format
			add(oracleCase{Name: "translateRuleLine/" + line + "/" + format, Mod: rp, Fn: "translateRuleLine", Args: []any{line, format},
				Go: func() (any, error) { return TranslateRuleLine(line, format), nil }})
		}
	}
	proxies := URLsToClashProxies(sampleLinks, nil)
	for _, level := range []string{"BASE", "STD", "FULL", "RELAY"} {
		level := level
		add(oracleCase{Name: "POLICY_GROUPS/" + level, Mod: rp, Fn: "POLICY_GROUPS", Prop: level, Args: []any{objs(proxies)},
			Go: func() (any, error) { return anys(PolicyGroups(level, proxies, nil)), nil }})
		add(oracleCase{Name: "POLICY_GROUPS/empty/" + level, Mod: rp, Fn: "POLICY_GROUPS", Prop: level, Args: []any{js.Arr()},
			Go: func() (any, error) { return anys(PolicyGroups(level, nil, nil)), nil }})
		add(oracleCase{Name: "pruneProxyGroups/" + level, Mod: rp, Fn: "pruneProxyGroups",
			Args: []any{anys(PolicyGroups(level, proxies[:3], nil)), objs(proxies[:3])},
			Go: func() (any, error) {
				return objs(PruneProxyGroups(asObjects(PolicyGroups(level, proxies[:3], nil)), proxies[:3])), nil
			}})
	}
	for _, u := range []any{"https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/BanAD.list", "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-us.srs",
		"https://raw.githubusercontent.com/other/repo/main/a/b.list", "https://raw.githubusercontent.com/a/b/c", "  https://example.com/x  ", "ftp://x", nil, "https://[bad"} {
		u := u
		for _, mirror := range []bool{true, false} {
			mirror := mirror
			add(oracleCase{Name: "pinRemoteRuleUrl/" + js.ToString(u) + js.ToString(mirror), Mod: rp, Fn: "pinRemoteRuleUrl", Args: []any{u, js.Obj("mirror", mirror)},
				Go: func() (any, error) { return PinRemoteRuleURL(u, mirror), nil }})
		}
	}
	add(oracleCase{Name: "getSingboxDnsRuleSet", Mod: rp, Fn: "getSingboxDnsRuleSet", Go: func() (any, error) { return GetSingboxDNSRuleSet(), nil }})
	add(oracleCase{Name: "AI_SERVICE_RULES", Mod: rp, Fn: "AI_SERVICE_RULES", Go: func() (any, error) { return AIServiceRulesValue(), nil }})
	add(oracleCase{Name: "RULE_SETS", Mod: rp, Fn: "RULE_SETS", Go: func() (any, error) { return RuleSetsValue(), nil }})
	add(oracleCase{Name: "BUILTIN_TEMPLATE_REGISTRY", Mod: "modules/subscription/builtin-template-registry.js", Fn: "BUILTIN_TEMPLATE_REGISTRY",
		Go: func() (any, error) { return BuiltinTemplateRegistryValue(), nil }})
	add(oracleCase{Name: "getBuiltinTemplate/missing", Mod: "modules/subscription/builtin-template-registry.js", Fn: "getBuiltinTemplate", Args: []any{"nope"},
		Go: func() (any, error) { return js.Null, nil }})

	const sd = "modules/subscription/safe-dns.js"
	raws := []any{nil, "", "  ", "nameserver: [8.8.8.8, 'tls://1.1.1.1', localhost, '127.0.0.1', 'https://dns.google/dns-query#x', system]",
		"dns:\n  foreign: [udp://9.9.9.9:53]\n  domestic: 114.114.114.114\n  mode: polluted\n  polluted: [https://[2606:4700::1111]/dns-query]",
		"policy:\n  domestic: [223.6.6.6]\n  fake-ip-filter-mode: whitelist\nuse-hosts: 0\nfake-ip-ttl: 3\ncache-algorithm: arc\nfake-ip-filter-mode: bad",
		"enable: false\nnameserver: [1.1.1.1]", "nameserver: [1.1.1.1]\nnameserver-policy: {a: b}", "{bad yaml", "- a\n- b",
		js.Obj("enhanced-mode", "redir-host"), js.Obj("domestic", strs("10.0.0.1", "8.8.4.4"))}
	for i, raw := range raws {
		raw := raw
		n := js.NumberToString(float64(i))
		for _, mode := range []any{nil, "polluted", "CLEAN"} {
			mode := mode
			opts := js.Obj("mode", mode, "proxyGroup", "DNS组")
			add(oracleCase{Name: "resolveSafeDnsConfig/" + n + "/" + js.ToString(mode), Mod: sd, Fn: "resolveSafeDnsConfig", Args: []any{raw, opts},
				Go: func() (any, error) {
					return ResolveSafeDNSConfig(raw, SafeDNSOptions{Mode: mode, ProxyGroup: "DNS组"}), nil
				}})
			add(oracleCase{Name: "buildSingboxDnsConfig/" + n + "/" + js.ToString(mode), Mod: sd, Fn: "buildSingboxDnsConfig", Args: []any{raw, js.Obj("mode", mode)},
				Go: func() (any, error) { return BuildSingboxDNSConfig(raw, SafeDNSOptions{Mode: mode}), nil }})
			add(oracleCase{Name: "resolveDnsPolicy/" + n + "/" + js.ToString(mode), Mod: sd, Fn: "resolveDnsPolicy", Args: []any{raw, js.Obj("mode", mode)},
				Go: func() (any, error) { return ResolveDNSPolicy(raw, mode).Value(), nil }})
		}
		add(oracleCase{Name: "isExplicitDnsBlock/" + n, Mod: sd, Fn: "isExplicitDnsBlock", Args: []any{parseDNSOverride(raw)},
			Go: func() (any, error) { return isExplicitDNSBlockValue(parseDNSOverride(raw)), nil }})
	}
	add(oracleCase{Name: "DEFAULT_DNS_CONFIG", Mod: sd, Fn: "DEFAULT_DNS_CONFIG", Go: func() (any, error) { return DefaultDNSConfig(), nil }})

	const rg = "modules/subscription/region-groups.js"
	overrides := mustJSON(t, `{"regionOverrides": [{"pattern": "IPv6", "region": " 日本 "}, {"pattern": "(bad"}, {"pattern": "socks", "region": "印度", "flags": "gi"}, {"pattern": "socks", "region": "印度", "flags": "gi"}]}`).(*js.Object)
	add(oracleCase{Name: "groupNodeLinesByRegion", Mod: rg, Fn: "groupNodeLinesByRegion", Args: []any{objs(proxies), overrides},
		Go: func() (any, error) { return objs(GroupNodeLinesByRegion(proxies, overrides)), nil }})
	var lines []*js.Object
	for _, l := range sampleLinks {
		lines = append(lines, js.Obj("line", l))
	}
	add(oracleCase{Name: "groupNodeLinesByProtocol", Mod: "modules/subscription/protocol-groups.js", Fn: "groupNodeLinesByProtocol", Args: []any{objs(lines)},
		Go: func() (any, error) { return objs(GroupNodeLinesByProtocol(lines)), nil }})
	const tc = "modules/subscription/template-compatibility.js"
	for _, target := range []any{"clash", "Surge&ver=4", "sing-box", "egern", nil} {
		for _, u := range []any{"https://x.com/a/b.ini?x=1", "b.INI#f", "https://x.com/a.yaml", "", nil, "noext"} {
			target, u := target, u
			add(oracleCase{Name: "shouldApplyExternalTemplateForTarget/" + js.ToString(target) + "/" + js.ToString(u), Mod: tc,
				Fn: "shouldApplyExternalTemplateForTarget", Args: []any{target, u},
				Go: func() (any, error) { return ShouldApplyExternalTemplateForTarget(target, u), nil }})
		}
		target := target
		add(oracleCase{Name: "getTemplateCompatibility/" + js.ToString(target), Mod: tc, Fn: "getTemplateCompatibility", Args: []any{target},
			Go: func() (any, error) {
				if c := GetTemplateCompatibility(target); c != nil {
					return c.Value(), nil
				}
				return js.Null, nil
			}})
	}
	runOracle(t, cases)
}
