package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/template-renderers/render-clash.js

func mapClashGroupType(t any) string {
	n := js.ToLower(js.Trim(strOr(t)))
	switch n {
	case "url-test", "fallback", "load-balance", "select":
		return n
	}
	return "select"
}

func filterAutoSelectMembers(group *js.Object) *js.Array {
	typ := mapClashGroupType(group.Get("type"))
	out := js.Arr()
	for _, m := range arrayOrEmpty(group.Get("members")).Items {
		if !js.Truthy(m) {
			continue
		}
		if autoSelectTypes[typ] {
			switch js.ToUpper(s(m)) {
			case "DIRECT", "REJECT", "REJECT-DROP", "PASS":
				continue
			}
		}
		out.Push(m)
	}
	return out
}

func stringSet(items ...string) map[string]bool {
	m := map[string]bool{}
	for _, i := range items {
		m[i] = true
	}
	return m
}

var (
	acl4ssrRootProviderFiles = stringSet("apple", "banad", "baneasylist", "baneasylistchina", "baneasyprivacy", "banprogramad",
		"chinacompanyip", "chinadomain", "chinaip", "chinaipv6", "chinamedia", "download", "localareanetwork", "mjj", "proxylite",
		"proxygfwlist", "proxymedia", "unban")
	acl4ssrIPCIDRProviderFiles = stringSet("amazonip", "chinacompanyip", "chinaip", "chinaipv6", "netflixip")
	acl4ssrRootListOnlyFiles   = stringSet("localareanetwork", "banad", "banprogramad", "chinamedia", "proxymedia", "chinadomain", "download", "unban")

	reJsdelivrGH     = js.MustRegExp(`^\/gh\/([^/@]+)\/([^/@]+)@([^/]+)\/(.+)$`, "")
	reClashListPath  = js.MustRegExp(`\/Clash\/.*\.(list|txt)$`, "i")
	reListExt        = js.MustRegExp(`\.(list|txt)$`, "i")
	reClashRootList  = js.MustRegExp(`\/Clash\/[^/]+\.(list|txt)$`, "i")
	reClashRulesetIn = js.MustRegExp(`\/Clash\/Ruleset\/`, "i")
	reClashDir       = js.MustRegExp(`\/Clash\/`, "i")
	reProviderExt    = js.MustRegExp(`\.(yaml|yml|list|txt|conf)$`, "i")
	reNonAlnum       = js.MustRegExp(`[^a-zA-Z0-9]`, "g")
)

func toClashRuleProviderURL(sourceURL any) any {
	src := strOr(sourceURL)
	if !reHTTPScheme.Test(src) {
		return sourceURL
	}
	u, err := js.ParseURL(src)
	if err != nil {
		return sourceURL
	}
	var mirror *js.Match
	if u.Hostname() == "cdn.jsdelivr.net" {
		mirror = reJsdelivrGH.MatchFirst(u.Pathname())
	}
	var parts []string
	if mirror != nil {
		parts = append([]string{mirror.Str(1), mirror.Str(2), mirror.Str(3)}, strings.Split(mirror.Str(4), "/")...)
	} else {
		for _, p := range strings.Split(u.Pathname(), "/") {
			if p != "" {
				parts = append(parts, p)
			}
		}
	}
	if !reGithubRawHost.Test(u.Hostname()) && mirror == nil {
		return sourceURL
	}
	at := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	owner, repo := at(0), at(1)
	if js.ToLower(owner) != "acl4ssr" || js.ToLower(repo) != "acl4ssr" {
		return sourceURL
	}
	rest := ""
	if len(parts) > 3 {
		rest = strings.Join(parts[3:], "/")
	}
	repoPath := "/" + rest
	if !reClashListPath.Test(repoPath) {
		return sourceURL
	}
	fileName := reListExt.Replace(lastSegment(repoPath, "/"), "")
	if reClashRootList.Test(repoPath) && acl4ssrRootListOnlyFiles[js.ToLower(fileName)] {
		return sourceURL
	}
	var newPath string
	switch {
	case reClashRulesetIn.Test(repoPath):
		newPath = reListExt.Replace(reClashRulesetIn.Replace(repoPath, "/Clash/Providers/Ruleset/"), ".yaml")
	case acl4ssrRootProviderFiles[js.ToLower(fileName)]:
		newPath = reListExt.Replace(reClashDir.Replace(repoPath, "/Clash/Providers/"), ".yaml")
	default:
		newPath = reListExt.Replace(reClashDir.Replace(repoPath, "/Clash/Providers/Ruleset/"), ".yaml")
	}
	if mirror != nil {
		return "https://cdn.jsdelivr.net/gh/" + owner + "/" + repo + "@" + mirror.Str(3) + newPath
	}
	return "https://raw.githubusercontent.com/" + owner + "/" + repo + "/" + s(js.Coalesce(stringOrNil(parts, 2))) + newPath
}

// stringOrNil 对应 parts[i]（越界为 undefined）。
func stringOrNil(parts []string, i int) any {
	if i < len(parts) {
		return parts[i]
	}
	return nil
}

func providerFileName(providerURL string) (string, bool) {
	u, err := js.ParseURL(providerURL)
	if err != nil {
		return "", false
	}
	return reProviderExt.Replace(lastSegment(u.Pathname(), "/"), ""), true
}

func getRuleProviderBehavior(providerURL string) string {
	if f, ok := providerFileName(providerURL); ok && acl4ssrIPCIDRProviderFiles[js.ToLower(f)] {
		return "ipcidr"
	}
	return "classical"
}

type providerIndex struct {
	keys  []string
	names map[string]string
}

func (p *providerIndex) get(u any) (string, bool) {
	n, ok := p.names[setKey(u)]
	return n, ok
}

func mapClashRule(rule *js.Object, providers *providerIndex) any {
	typ := js.ToUpper(strOr(rule.Get("type")))
	switch typ {
	case "":
		return nil
	case "MATCH", "FINAL":
		return "MATCH," + s(rule.Get("policy"))
	case "GEOIP":
		return "GEOIP," + s(js.Or(rule.Get("value"), "CN")) + "," + s(rule.Get("policy"))
	case "RULE-SET":
		if n, ok := providers.get(rule.Get("value")); ok && n != "" {
			return "RULE-SET," + n + "," + s(rule.Get("policy"))
		}
		return "RULE-SET," + s(rule.Get("value")) + "," + s(rule.Get("policy"))
	}
	return typ + "," + s(rule.Get("value")) + "," + s(rule.Get("policy"))
}

// RenderClashFromTemplateModel 对应 renderClashFromTemplateModel（输出 Clash / Mihomo YAML）。
func RenderClashFromTemplateModel(modelVal any) (string, error) {
	model := NormalizeUnifiedTemplateModel(modelVal)
	if js.IsArray(objGet(model.Get("extras"), "chainProxy")) {
		ApplyMihomoChainProxy(model, js.Truthy(objGet(model.Get("meta"), "isMeta")))
	}
	ruleProviders := js.NewObject()
	providers := &providerIndex{names: map[string]string{}}
	counter := 0
	rules := arrayOrEmpty(model.Get("rules")).Items
	for _, r := range rules {
		ro := objOrEmpty(r)
		value := ro.Get("value")
		if js.ToUpper(strOr(ro.Get("type"))) != "RULE-SET" || !js.Truthy(value) || !reHTTPScheme.Test(js.ToString(value)) {
			continue
		}
		providerURL := toClashRuleProviderURL(value)
		if _, exists := providers.get(providerURL); exists {
			continue
		}
		urlStr := s(providerURL)
		nameHint := "rs"
		if f, ok := providerFileName(urlStr); ok && f != "" {
			nameHint = js.ToLower(reNonAlnum.Replace(f, "_"))
		}
		providerName := nameHint + "_" + js.NumberToString(float64(counter))
		counter++
		providers.names[setKey(providerURL)] = providerName
		usesText := reListExt.Test(urlStr)
		ext := "yaml"
		if usesText {
			ext = "list"
		}
		def := js.Obj("type", "http", "behavior", getRuleProviderBehavior(urlStr), "url", providerURL,
			"path", "./ruleset/"+providerName+"."+ext, "interval", 86400.0)
		if usesText {
			def.Set("format", "text")
		}
		ruleProviders.Set(providerName, def)
	}
	settings := model.Get("settings")
	groups := js.Arr()
	for _, g := range arrayOrEmpty(model.Get("groups")).Items {
		group := objOrEmpty(g)
		members, mOK := group.Get("members").(*js.Array)
		filters, fOK := group.Get("filters").(*js.Array)
		if !((mOK && members.Len() > 0) || (fOK && filters.Len() > 0)) {
			continue
		}
		var filter any
		if fOK && filters.Len() > 0 {
			filter = filters.Join("|")
		}
		out := js.Obj("name", group.Get("name"), "type", mapClashGroupType(group.Get("type")), "proxies", filterAutoSelectMembers(group), "filter", filter)
		out.Assign(group.Get("options"))
		groups.Push(out)
	}
	clashRules := js.Arr()
	for _, r := range rules {
		ro := objOrEmpty(r)
		var line any
		if js.ToUpper(strOr(ro.Get("type"))) != "RULE-SET" || !js.Truthy(ro.Get("value")) {
			line = mapClashRule(ro, providers)
		} else {
			c := ro.Clone()
			c.Set("value", toClashRuleProviderURL(ro.Get("value")))
			line = mapClashRule(c, providers)
		}
		if js.Truthy(line) {
			clashRules.Push(line)
		}
	}
	var rp any
	if ruleProviders.Len() > 0 {
		rp = ruleProviders
	}
	config := js.Obj(
		"mixed-port", 7890.0,
		"allow-lan", false,
		"bind-address", "127.0.0.1",
		"ipv6", false,
		"mode", "rule",
		"log-level", "info",
		"external-controller", "127.0.0.1:9090",
		"dns", ResolveSafeDNSConfig(objGet(settings, "customDnsOverride"), SafeDNSOptions{Mode: objGet(settings, "dnsMode"), ProxyGroup: DNSProxyGroup}),
		"proxies", model.Get("proxies"),
		"proxy-groups", groups,
		"rule-providers", rp,
		"rules", clashRules,
		"profile", js.Obj("store-selected", true, "subscription-url", js.Or(objGet(settings, "managedConfigUrl"), "")),
	)
	out, err := js.YAMLDump(config, js.YAMLDumpOptions{DoubleQuote: true})
	if err != nil {
		return "", err
	}
	return ClashFix(out), nil
}
