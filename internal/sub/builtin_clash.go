package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/builtin-clash-generator.js：不依赖 subconverter，直接生成完整 Clash 配置。

var reControlChars = js.MustRegExp(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`, "g")

func cleanControlChars(v string) string { return reControlChars.Replace(v, "") }

// deepCleanControlChars 递归清理字符串中的控制字符（对象会被复制）。
func deepCleanControlChars(v any) any {
	switch x := v.(type) {
	case string:
		return cleanControlChars(x)
	case *js.Array:
		out := js.Arr()
		for _, item := range x.Items {
			out.Push(deepCleanControlChars(item))
		}
		return out
	case *js.Object:
		out := js.NewObject()
		for _, kv := range x.Entries() {
			out.Set(cleanControlChars(kv.Key), deepCleanControlChars(kv.Value))
		}
		return out
	}
	return v
}

func deduplicateAllNames(proxies []*js.Object) {
	namer := NewUniqueNamer()
	for _, p := range proxies {
		p.Set("name", namer.GetUniqueNameValue(p.Get("name")))
	}
}

func stripInternalProxyFields(proxies []*js.Object) []*js.Object {
	out := make([]*js.Object, len(proxies))
	for i, p := range proxies {
		c := p.Clone()
		c.Delete("metadata")
		out[i] = c
	}
	return out
}

func applyMihomoRelayDialerProxy(proxies, publicProxies []*js.Object, groups []*js.Object) ([]*js.Object, []*js.Object) {
	var relay *js.Object
	for _, g := range groups {
		if js.StrictEquals(g.Get("name"), "🔗 链式代理") {
			relay = g
			break
		}
	}
	if relay == nil {
		return publicProxies, groups
	}
	chain := make([]*js.Object, len(publicProxies))
	names := js.Arr()
	for i, p := range publicProxies {
		c := p.Clone()
		c.Set("name", "🔗 链式代理 - "+s(p.Get("name")))
		c.Set("dialer-proxy", "入口节点")
		chain[i] = c
		names.Push(c.Get("name"))
	}
	var next []*js.Object
	for _, g := range groups {
		switch {
		case js.StrictEquals(g.Get("name"), "🔗 链式代理"):
			c := g.Clone()
			c.Set("type", "select")
			c.Set("proxies", names)
			next = append(next, c)
		case js.StrictEquals(g.Get("name"), "落地节点"):
		default:
			next = append(next, g)
		}
	}
	return append(append([]*js.Object(nil), publicProxies...), chain...), PruneProxyGroups(next, append(append([]*js.Object(nil), proxies...), chain...))
}

func optionString(options *js.Object, key, def string) string {
	v := options.Get(key)
	if v == nil {
		return def
	}
	return s(v)
}

// searchParamsOption 读取 options.searchParams（调用方以 *js.URLSearchParams 传入）。
func searchParamsOption(options *js.Object) *js.URLSearchParams {
	sp, _ := options.Get("searchParams").(*js.URLSearchParams)
	return sp
}

var (
	reHiddifyUA = js.MustRegExp(`hiddify`, "i")
	reStashUA   = js.MustRegExp(`stash`, "i")
)

func asObjects(items []any) []*js.Object {
	out := make([]*js.Object, 0, len(items))
	for _, it := range items {
		out = append(out, objOrEmpty(it))
	}
	return out
}

// GenerateBuiltinClashConfig 对应 generateBuiltinClashConfig。
func GenerateBuiltinClashConfig(nodeList string, options *js.Object) string {
	if options == nil {
		options = js.NewObject()
	}
	ruleLevel := options.Get("ruleLevel")
	if ruleLevel == nil {
		ruleLevel = "std"
	}
	userAgent := optionString(options, "userAgent", "")
	enableMihomo := js.Truthy(options.Get("isMeta")) || IsMetaCore(userAgent, searchParamsOption(options))
	isHiddify := js.Truthy(options.Get("hiddifyCompatible")) || reHiddifyUA.Test(userAgent)
	isStash := reStashUA.Test(userAgent)

	var urls []string
	for _, l := range strings.Split(cleanControlChars(nodeList), "\n") {
		if l = js.Trim(l); l != "" && !strings.HasPrefix(l, "#") {
			urls = append(urls, l)
		}
	}
	var proxies []*js.Object
	for _, p := range URLsToClashProxies(urls, options) {
		proxies = append(proxies, deepCleanControlChars(p).(*js.Object))
	}
	deduplicateAllNames(proxies)
	if len(proxies) == 0 {
		return "# No valid proxies found\nproxies: []\n"
	}
	levelKey := js.ToUpper(s(js.Or(ruleLevel, "std")))
	var rawRules []any
	if isHiddify {
		rawRules = []any{"MATCH,🚀 节点选择"}
	} else {
		rawRules = GetBuiltinRules(levelKey, "clash")
	}
	factoryLevel := levelKey
	switch levelKey {
	case "BASE", "STD", "FULL", "RELAY":
	default:
		factoryLevel = "STD"
	}
	groups := PruneProxyGroups(asObjects(PolicyGroups(factoryLevel, proxies, options)), proxies)
	ruleProviders := js.NewObject()
	if !isHiddify {
		ruleProviders = GetRemoteProviderDefinitions("clash", rawRules)
	}
	clashRules := js.Arr()
	for _, r := range rawRules {
		if str, ok := r.(string); ok {
			clashRules.Push(str)
		} else if js.StrictEquals(objGet(r, "type"), "rule-provider") {
			clashRules.Push("RULE-SET," + s(objGet(r, "provider")) + "," + s(objGet(r, "target")))
		}
	}
	publicProxies := stripInternalProxyFields(proxies)
	if levelKey == "RELAY" && enableMihomo {
		publicProxies, groups = applyMihomoRelayDialerProxy(proxies, publicProxies, groups)
	}
	custom := options.Get("customDnsOverride")
	if js.Truthy(custom) && js.Trim(s(custom)) != "" && !strings.Contains(s(custom), DNSProxyGroup) {
		var kept []*js.Object
		for _, g := range groups {
			if !js.StrictEquals(g.Get("name"), DNSProxyGroup) {
				kept = append(kept, g)
			}
		}
		groups = kept
	}
	var policies []any
	if a, ok := options.Get("subscriptionDnsPolicies").(*js.Array); ok {
		policies = a.Items
	}
	dns := ResolveSafeDNSConfig(js.Or(custom, ""), SafeDNSOptions{Mode: options.Get("dnsMode"), ProxyGroup: DNSProxyGroup, SubscriptionDNSPolicies: policies})
	config := js.Obj(
		"mixed-port", 7890.0,
		"allow-lan", false,
		"bind-address", "127.0.0.1",
		"ipv6", false,
		"mode", "rule",
		"log-level", "info",
		"external-controller", "127.0.0.1:9090",
		"dns", dns,
		"proxies", objs(publicProxies),
		"profile", js.Obj("store-selected", true, "subscription-url", js.Or(options.Get("managedConfigUrl"), "")),
		"proxy-groups", objs(groups),
	)
	if ruleProviders.Len() > 0 {
		config.Set("rule-providers", ruleProviders)
	}
	config.Set("rules", clashRules)
	out, err := js.YAMLDump(config, js.YAMLDumpOptions{DoubleQuote: true})
	if err != nil {
		return clashFallbackYAML(proxies, levelKey)
	}
	if mc := options.Get("managedConfigUrl"); isStash && js.Truthy(mc) && js.Trim(s(mc)) != "" {
		out = "#SUBSCRIBED " + s(mc) + "\n" + out
	}
	return cleanControlChars(out)
}

// clashFallbackYAML 对应生成失败时的兜底输出（至少保证是合法的 Clash YAML）。
func clashFallbackYAML(proxies []*js.Object, levelKey string) string {
	public := stripInternalProxyFields(proxies)
	selectGroup := DefaultSelectGroup
	if levelKey == "RELAY" {
		selectGroup = DefaultRelayGroup
	}
	lines := make([]string, len(public))
	names := js.Arr()
	for i, p := range public {
		lines[i] = "  - " + js.MustStringify(p)
		names.Push(p.Get("name"))
	}
	return "proxies:\n" + strings.Join(lines, "\n") + "\n" +
		"proxy-groups:\n  - name: " + selectGroup + "\n    type: select\n    proxies: " + js.MustStringify(names) + "\n" +
		"rules:\n  - MATCH," + selectGroup + "\n"
}

// GenerateProxiesOnly 对应 generateProxiesOnly：只输出 proxies 段。
func GenerateProxiesOnly(nodeList string) string {
	var urls []string
	for _, l := range strings.Split(cleanControlChars(nodeList), "\n") {
		if l = js.Trim(l); l != "" && !strings.HasPrefix(l, "#") {
			urls = append(urls, l)
		}
	}
	var proxies []*js.Object
	for _, p := range URLsToClashProxies(urls, nil) {
		proxies = append(proxies, deepCleanControlChars(p).(*js.Object))
	}
	deduplicateAllNames(proxies)
	out, err := js.YAMLDump(js.Obj("proxies", objs(stripInternalProxyFields(proxies))), js.YAMLDumpOptions{})
	if err != nil {
		lines := make([]string, 0, len(proxies))
		for _, p := range stripInternalProxyFields(proxies) {
			lines = append(lines, "  - "+js.MustStringify(p))
		}
		return "proxies:\n" + strings.Join(lines, "\n") + "\n"
	}
	return cleanControlChars(out)
}
