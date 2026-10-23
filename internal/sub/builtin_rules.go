package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/builtin-rules-provider.js

// 策略组标准名称。
const (
	DefaultSelectGroup = "🚀 节点选择"
	DefaultRelayGroup  = "🌍 总出口"
	AutoSelectGroup    = "♻️ 自动选择"
	FallbackGroup      = "🔯 故障转移"
	ManualSelectGroup  = "👋 手动切换"
	aiAutoGroup        = "🤖 AI 自动"
	aiFallbackGroup    = "🤖 AI 故障转移"
	gstaticURL         = "http://www.gstatic.com/generate_204"
)

// AIService 是 AI_SERVICE_RULES 的一项。
type AIService struct {
	ID, Name string
	Domains  []string
}

// AIServiceRules 对应 AI_SERVICE_RULES。
var AIServiceRules = []AIService{
	{"openai", "OpenAI", []string{"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "auth0.openai.com"}},
	{"claude", "Claude", []string{"anthropic.com", "claude.ai", "claude.com", "claudeusercontent.com", "anthropicusercontent.com"}},
	{"gemini", "Gemini", []string{"gemini.google.com", "aistudio.google.com", "generativelanguage.googleapis.com", "ai.google.dev", "makersuite.google.com"}},
	{"copilot", "Copilot", []string{"copilot.microsoft.com", "githubcopilot.com", "api.githubcopilot.com"}},
	{"grok", "Grok", []string{"x.ai", "grok.com", "grok.x.com"}},
	{"perplexity", "Perplexity", []string{"perplexity.ai", "pplx.ai"}},
	{"mistral", "Mistral", []string{"mistral.ai"}},
	{"deepseek", "DeepSeek", []string{"deepseek.com", "chat.deepseek.com", "api.deepseek.com"}},
	{"ai-platforms", "AI 平台", []string{"poe.com", "character.ai", "huggingface.co", "replicate.com", "openrouter.ai", "groq.com", "cohere.com"}},
}

// AIServiceRulesValue 返回与上游一致的 JS 数组形式。
func AIServiceRulesValue() *js.Array {
	a := js.Arr()
	for _, s := range AIServiceRules {
		a.Push(js.Obj("id", s.ID, "name", s.Name, "domains", js.StrArr(s.Domains)))
	}
	return a
}

// AIDomainRuleLines 对应 AI_DOMAIN_RULE_LINES。
var AIDomainRuleLines = func() []string {
	var out []string
	for _, s := range AIServiceRules {
		for _, d := range s.Domains {
			out = append(out, "DOMAIN-SUFFIX,"+d+",🤖 "+s.Name)
		}
	}
	return out
}()

func urlTestOptions() *js.Object {
	return js.Obj("url", gstaticURL, "interval", 300.0, "tolerance", 50.0)
}

func dnsProxyGroup(proxyNames *js.Array) *js.Object {
	proxies := proxyNames
	if proxyNames.Len() == 0 {
		proxies = strs2("REJECT")
	}
	return js.Obj("name", DNSProxyGroup, "type", "url-test", "proxies", proxies, "hidden", true, "options", urlTestOptions())
}

func aiPolicyGroups(proxyNames *js.Array, regionNames []string, relay bool) []any {
	nodeCandidates := proxyNames
	if proxyNames.Len() == 0 {
		nodeCandidates = strs2("REJECT")
	}
	candidates := strs2(aiAutoGroup, aiFallbackGroup)
	for _, r := range regionNames {
		candidates.Push(r)
	}
	candidates.Push(ManualSelectGroup)
	if relay {
		candidates.Push("🔗 链式代理", "🚀 常用节点")
	}
	out := []any{
		js.Obj("name", aiAutoGroup, "type", "url-test", "proxies", nodeCandidates, "hidden", true, "options", urlTestOptions()),
		js.Obj("name", aiFallbackGroup, "type", "fallback", "proxies", nodeCandidates, "hidden", true, "options", urlTestOptions()),
		js.Obj("name", "🤖 智能 AI", "type", "select", "proxies", candidates),
	}
	for _, s := range AIServiceRules {
		out = append(out, js.Obj("name", "🤖 "+s.Name, "type", "select", "proxies", candidates))
	}
	return out
}

// PruneProxyGroups 对应 pruneProxyGroups：清理不存在的成员、禁止自引用与回引顶级组。
func PruneProxyGroups(proxyGroups []*js.Object, proxies []*js.Object) []*js.Object {
	valid := map[string]bool{}
	addValid := func(v any) { valid[js.ToString(v)] = true }
	for _, p := range proxies {
		addValid(js.Or(p.Get("tag"), p.Get("name")))
	}
	for _, g := range proxyGroups {
		addValid(g.Get("name"))
	}
	for _, n := range []string{DefaultSelectGroup, DefaultRelayGroup, AutoSelectGroup, FallbackGroup, ManualSelectGroup, DNSProxyGroup,
		"DIRECT", "REJECT", "REJECT-DROP", "ANY"} {
		valid[n] = true
	}
	out := make([]*js.Object, len(proxyGroups))
	for i, group := range proxyGroups {
		members, ok := group.Get("proxies").(*js.Array)
		if !ok {
			out[i] = group
			continue
		}
		kept := js.Arr()
		for _, m := range members.Items {
			if js.StrictEquals(m, group.Get("name")) {
				continue
			}
			if js.StrictEquals(m, DefaultSelectGroup) || js.StrictEquals(m, DefaultRelayGroup) {
				continue
			}
			if s, isStr := m.(string); isStr && (strings.HasPrefix(s, "(") || strings.Contains(s, ".*") ||
				strings.Contains(s, "+") || strings.Contains(s, "$")) {
				kept.Push(m)
				continue
			}
			if s, isStr := m.(string); isStr && valid[s] {
				kept.Push(m)
			}
		}
		failClosed := strings.HasPrefix(strOr(group.Get("name")), "🤖") || js.StrictEquals(group.Get("name"), DNSProxyGroup)
		if kept.Len() == 0 {
			if failClosed {
				kept = strs2("REJECT")
			} else {
				kept = strs2("DIRECT")
			}
		}
		n := group.Clone()
		n.Set("proxies", kept)
		out[i] = n
	}
	return out
}

type regionGroupSet struct {
	selectGroups, supportGroups []any
	names                       []string
}

func generateRegionGroups(proxies []*js.Object, options *js.Object) regionGroupSet {
	var set regionGroupSet
	for _, r := range GroupNodeLinesByRegion(proxies, options) {
		name := js.ToString(r.Get("name"))
		autoName := "⚡️ " + strings.Replace(name, "节点", "", 1) + " - 自动测速"
		set.names = append(set.names, name)
		tags := r.Get("tags").(*js.Array)
		members := strs2(autoName)
		members.Push(tags.Items...)
		set.selectGroups = append(set.selectGroups, js.Obj("name", name, "type", "select", "proxies", members))
		set.supportGroups = append(set.supportGroups, js.Obj("name", autoName, "type", "url-test", "proxies", tags,
			"hidden", true, "options", urlTestOptions()))
	}
	return set
}

func proxyNamesOf(proxies []*js.Object) *js.Array {
	a := js.Arr()
	for _, p := range proxies {
		a.Push(js.Or(p.Get("tag"), p.Get("name")))
	}
	return a
}

func selectGroup(name string, members ...any) *js.Object {
	return js.Obj("name", name, "type", "select", "proxies", js.Arr(members...))
}

func withRegions(prefix []any, regionNames []string, suffix ...any) []any {
	out := append([]any(nil), prefix...)
	for _, r := range regionNames {
		out = append(out, r)
	}
	return append(out, suffix...)
}

// PolicyGroups 对应 POLICY_GROUPS 的各等级（BASE / STD / FULL / RELAY）。
func PolicyGroups(level string, proxies []*js.Object, options *js.Object) []any {
	names := proxyNamesOf(proxies)
	base := []any{
		dnsProxyGroup(names),
	}
	core := func() []any {
		return []any{
			js.Obj("name", AutoSelectGroup, "type", "url-test", "proxies", names),
			js.Obj("name", FallbackGroup, "type", "fallback", "proxies", names),
			js.Obj("name", ManualSelectGroup, "type", "select", "proxies", names),
		}
	}
	streaming := func() *js.Object {
		return selectGroup("🎥 流媒体", "🇸🇬 狮城节点", "🇭🇰 香港节点", "🇹🇼 台湾节点", "🇯🇵 日本节点", AutoSelectGroup, ManualSelectGroup, "DIRECT")
	}
	switch level {
	case "BASE":
		out := append(base, selectGroup(DefaultSelectGroup, AutoSelectGroup, FallbackGroup, ManualSelectGroup, "DIRECT"))
		out = append(out, core()...)
		return append(out, aiPolicyGroups(names, nil, false)...)
	case "STD", "FULL":
		rg := generateRegionGroups(proxies, options)
		out := append(base, selectGroup(DefaultSelectGroup, withRegions([]any{AutoSelectGroup, FallbackGroup, ManualSelectGroup}, rg.names, "DIRECT")...))
		out = append(out, core()...)
		out = append(out, rg.selectGroups...)
		out = append(out, aiPolicyGroups(names, rg.names, false)...)
		out = append(out,
			selectGroup("🎬 视频广告", "REJECT", "DIRECT"),
			streaming(),
			selectGroup("🍎 Apple", "DIRECT", AutoSelectGroup, ManualSelectGroup),
			selectGroup("Ⓜ️ Microsoft", "DIRECT", AutoSelectGroup, ManualSelectGroup),
			selectGroup("📲 Telegram", AutoSelectGroup, "🇸🇬 狮城节点", "🇭🇰 香港节点", ManualSelectGroup, "DIRECT"),
		)
		if level == "FULL" {
			out = append(out,
				selectGroup("🎧 Spotify", AutoSelectGroup, "🇸🇬 狮城节点", ManualSelectGroup, "DIRECT"),
				selectGroup("🎮 游戏平台", "DIRECT", AutoSelectGroup, ManualSelectGroup),
			)
		}
		return append(out, rg.supportGroups...)
	case "RELAY":
		rg := generateRegionGroups(proxies, options)
		out := append(base, selectGroup(DefaultRelayGroup, withRegions([]any{"🔗 链式代理", AutoSelectGroup, ManualSelectGroup, "🚀 常用节点"}, rg.names, "DIRECT")...))
		out = append(out, selectGroup("🔗 链式代理", append([]any{"入口节点", AutoSelectGroup, ManualSelectGroup, "DIRECT"}, names.Items...)...))
		out = append(out, selectGroup("入口节点", append([]any{AutoSelectGroup, ManualSelectGroup, "DIRECT"}, names.Items...)...))
		out = append(out, rg.selectGroups...)
		out = append(out, selectGroup("🚀 常用节点", withRegions([]any{AutoSelectGroup, FallbackGroup, ManualSelectGroup}, rg.names, "DIRECT")...))
		out = append(out, core()...)
		out = append(out, selectGroup("🎬 视频广告", "REJECT", "DIRECT"), streaming())
		out = append(out, aiPolicyGroups(names, rg.names, true)...)
		out = append(out,
			selectGroup("🍎 Apple", "DIRECT", "🚀 常用节点", AutoSelectGroup),
			selectGroup("Ⓜ️ Microsoft", "DIRECT", "🚀 常用节点", AutoSelectGroup),
		)
		return append(out, rg.supportGroups...)
	}
	return nil
}

// PinnedRuleRevisions 对应 PINNED_RULE_REVISIONS。
var PinnedRuleRevisions = map[string]string{
	"ACL4SSR":      "433381ebc4b1de59350fa8bed2a04a888228f801",
	"SING_GEOSITE": "0adeef8a3b9201292f6786ef4de81bcc02e971eb",
	"SING_GEOIP":   "b9c5e675b4d5359d4b47f4434fa7ae77e9991306",
	"BLACKMATRIX":  "538b8a79532c44dfbcb8e694d2f43e753c60b157",
}

var (
	acl4ssrBase     = "https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/" + PinnedRuleRevisions["ACL4SSR"]
	singGeositeBase = "https://raw.githubusercontent.com/SagerNet/sing-geosite/" + PinnedRuleRevisions["SING_GEOSITE"]
	singGeoipBase   = "https://raw.githubusercontent.com/SagerNet/sing-geoip/" + PinnedRuleRevisions["SING_GEOIP"]
	reHTTPScheme    = js.MustRegExp(`^https?:\/\/`, "i")
	reGithubRawHost = js.MustRegExp(`raw\.githubusercontent\.com$`, "i")
)

var pinnedRepoRevisions = map[string]string{
	"acl4ssr/acl4ssr":              PinnedRuleRevisions["ACL4SSR"],
	"sagernet/sing-geosite":        PinnedRuleRevisions["SING_GEOSITE"],
	"sagernet/sing-geoip":          PinnedRuleRevisions["SING_GEOIP"],
	"blackmatrix7/ios_rule_script": PinnedRuleRevisions["BLACKMATRIX"],
}

// PinRemoteRuleURL 对应 pinRemoteRuleUrl：把 raw.githubusercontent.com 改写为 jsDelivr 镜像并锁定修订号。
// 非 http(s) 地址原样返回（包括 undefined）。
func PinRemoteRuleURL(sourceURL any, mirror bool) any {
	raw := js.Trim(strOr(sourceURL))
	if !reHTTPScheme.Test(raw) {
		return sourceURL
	}
	u, err := js.ParseURL(raw)
	if err != nil {
		return raw
	}
	if !reGithubRawHost.Test(u.Hostname()) {
		return raw
	}
	var parts []string
	for _, p := range strings.Split(u.Pathname(), "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) < 4 {
		return raw
	}
	key := js.ToLower(parts[0]) + "/" + js.ToLower(parts[1])
	if rev, ok := pinnedRepoRevisions[key]; ok {
		parts[2] = rev
	}
	owner, repo, ref, rest := parts[0], parts[1], parts[2], parts[3:]
	if mirror {
		return "https://cdn.jsdelivr.net/gh/" + owner + "/" + repo + "@" + ref + "/" + strings.Join(rest, "/")
	}
	return "https://raw.githubusercontent.com/" + owner + "/" + repo + "/" + ref + "/" + strings.Join(rest, "/")
}

// pinURL 是 PinRemoteRuleURL 的默认镜像版本，返回字符串。
func pinURL(u string) string { return js.ToString(PinRemoteRuleURL(u, true)) }

// RemoteSource 是 REMOTE_SOURCES 的一项（空字段对应 undefined）。
type RemoteSource struct {
	Name, Clash, Singbox, Surge, Quanx string
}

// remoteSourceOrder 保持 REMOTE_SOURCES 的键顺序。
var remoteSourceOrder = []string{"ADS", "STREAM", "SOCIAL", "APPLE", "MICROSOFT", "AI", "geoip-cn", SingboxCNRuleSet}

// RemoteSources 对应 REMOTE_SOURCES。
var RemoteSources = map[string]RemoteSource{
	"ADS":            {"广告拦截", acl4ssrBase + "/Clash/Providers/BanAD.yaml", singGeositeBase + "/geosite-category-ads-all.srs", acl4ssrBase + "/Clash/BanAD.list", acl4ssrBase + "/Clash/BanAD.list"},
	"STREAM":         {"流媒体", acl4ssrBase + "/Clash/Providers/Ruleset/Netflix.yaml", singGeositeBase + "/geosite-netflix.srs", acl4ssrBase + "/Clash/Netflix.list", ""},
	"SOCIAL":         {"社交媒体", acl4ssrBase + "/Clash/Providers/Ruleset/Telegram.yaml", singGeositeBase + "/geosite-telegram.srs", acl4ssrBase + "/Clash/Telegram.list", ""},
	"APPLE":          {"苹果服务", acl4ssrBase + "/Clash/Providers/Ruleset/Apple.yaml", singGeositeBase + "/geosite-apple.srs", acl4ssrBase + "/Clash/Apple.list", ""},
	"MICROSOFT":      {"微软服务", acl4ssrBase + "/Clash/Providers/Ruleset/Microsoft.yaml", singGeositeBase + "/geosite-microsoft.srs", acl4ssrBase + "/Clash/Microsoft.list", acl4ssrBase + "/Clash/Microsoft.list"},
	"AI":             {"智能 AI", acl4ssrBase + "/Clash/Providers/Ruleset/OpenAi.yaml", singGeositeBase + "/geosite-openai.srs", acl4ssrBase + "/Clash/Ruleset/OpenAi.list", acl4ssrBase + "/Clash/Ruleset/OpenAi.list"},
	"geoip-cn":       {Name: "China IP (GeoIP)", Singbox: singGeoipBase + "/geoip-cn.srs"},
	SingboxCNRuleSet: {Name: "China Domains (GeoSite)", Singbox: singGeositeBase + "/geosite-cn.srs"},
}

func ruleSetLines(level string) []string {
	ai := append(append([]string(nil), AIDomainRuleLines...), "RULE-SET,AI,🤖 智能 AI")
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	media := []string{"RULE-SET,STREAM,🎥 流媒体", "RULE-SET,APPLE,🍎 Apple", "RULE-SET,MICROSOFT,Ⓜ️ Microsoft"}
	tail := func(group string) []string {
		return []string{"DOMAIN-SUFFIX,google.com," + group, "DOMAIN-SUFFIX,github.com," + group, "GEOIP,CN,DIRECT", "MATCH," + group}
	}
	switch level {
	case "BASE":
		return cat(ai, []string{"DOMAIN-SUFFIX,google.com," + DefaultSelectGroup, "DOMAIN-KEYWORD,google," + DefaultSelectGroup,
			"DOMAIN-SUFFIX,github.com," + DefaultSelectGroup, "GEOIP,CN,DIRECT", "MATCH," + DefaultSelectGroup})
	case "STD":
		return cat([]string{"RULE-SET,ADS,🎬 视频广告"}, ai, media, tail(DefaultSelectGroup))
	case "FULL":
		return cat([]string{"RULE-SET,ADS,🎬 视频广告", "RULE-SET,SOCIAL,📲 Telegram"}, ai, media, tail(DefaultSelectGroup))
	case "RELAY":
		return cat([]string{"RULE-SET,ADS,🎬 视频广告"}, ai, media, tail(DefaultRelayGroup))
	}
	return nil
}

// RuleSetsValue 对应 RULE_SETS（JS 对象形式）。
func RuleSetsValue() *js.Object {
	o := js.NewObject()
	for _, l := range []string{"BASE", "STD", "FULL", "RELAY"} {
		o.Set(l, js.StrArr(ruleSetLines(l)))
	}
	return o
}

func valueOrUndefined(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// TranslateRuleLine 对应 translateRuleLine：返回字符串、中间对象或 nil（null）。
func TranslateRuleLine(line, format string) any {
	parts := strings.Split(line, ",")
	at := func(i int) any {
		if i < len(parts) {
			return parts[i]
		}
		return nil
	}
	typ, value, target, extra := parts[0], at(1), at(2), at(3)
	if typ == "RULE-SET" {
		src, ok := RemoteSources[js.ToString(value)]
		if !ok || value == nil {
			return js.Null
		}
		switch format {
		case "clash":
			return js.Obj("type", "rule-provider", "provider", value, "target", target)
		case "singbox", "sing-box":
			return js.Obj("type", "rule_set", "tag", value, "outbound", target)
		case "surge", "loon":
			return "RULE-SET," + js.ToString(js.Or(valueOrUndefined(src.Surge), valueOrUndefined(src.Clash))) + "," + js.ToString(target)
		case "quanx":
			return "filter_remote, " + js.ToString(js.Or(valueOrUndefined(src.Quanx), valueOrUndefined(src.Clash))) + ", tag=" + src.Name +
				", force-policy=" + js.ToString(target) + ", update-interval=86400"
		}
		return js.Null
	}
	switch format {
	case "singbox", "sing-box":
		switch typ {
		case "DOMAIN-SUFFIX":
			return js.Obj("domain_suffix", js.Arr(value), "outbound", target)
		case "DOMAIN-KEYWORD":
			return js.Obj("domain_keyword", js.Arr(value), "outbound", target)
		case "DOMAIN":
			return js.Obj("domain", js.Arr(value), "outbound", target)
		case "IP-CIDR":
			return js.Obj("ip_cidr", js.Arr(value), "outbound", target)
		case "GEOIP":
			return js.Obj("type", "rule_set", "tag", "geoip-"+js.ToLower(js.ToString(value)), "outbound", target)
		}
		return js.Null
	case "surge", "loon":
		return line
	case "quanx":
		qxType := typ
		switch typ {
		case "DOMAIN-SUFFIX":
			qxType = "HOST-SUFFIX"
		case "DOMAIN-KEYWORD":
			qxType = "HOST-KEYWORD"
		case "DOMAIN":
			qxType = "HOST"
		case "MATCH":
			return "FINAL, " + js.ToString(value)
		}
		out := qxType + ", " + js.ToString(value) + ", " + js.ToString(target)
		if js.Truthy(extra) {
			out += ", " + js.ToString(extra)
		}
		return out
	}
	return line
}

// GetBuiltinRules 对应 getBuiltinRules。
func GetBuiltinRules(level, format string) []any {
	lines := ruleSetLines(js.ToUpper(level))
	if lines == nil {
		lines = ruleSetLines("STD")
	}
	var out []any
	for _, l := range lines {
		if r := TranslateRuleLine(l, format); js.Truthy(r) {
			out = append(out, r)
		}
	}
	return out
}

// GetRemoteProviderDefinitions 对应 getRemoteProviderDefinitions。
func GetRemoteProviderDefinitions(format string, ruleLines []any) *js.Object {
	providers := js.NewObject()
	var used []string
	seen := map[string]bool{}
	for _, line := range ruleLines {
		typ := objGet(line, "type")
		var tag any
		if format == "clash" && js.StrictEquals(typ, "rule-provider") {
			tag = objGet(line, "provider")
		} else if (format == "singbox" || format == "sing-box") && js.StrictEquals(typ, "rule_set") {
			tag = objGet(line, "tag")
		} else {
			continue
		}
		k := js.ToString(tag)
		if !seen[k] {
			seen[k] = true
			used = append(used, k)
		}
	}
	for _, tag := range used {
		src, ok := RemoteSources[tag]
		if !ok {
			continue
		}
		if format == "clash" {
			providers.Set(tag, js.Obj("type", "http", "behavior", "classical", "url", PinRemoteRuleURL(valueOrUndefined(src.Clash), true),
				"path", "./ruleset/"+tag+".yaml", "interval", 86400.0))
		} else {
			f := "source"
			if strings.HasSuffix(js.ToLower(src.Singbox), ".srs") {
				f = "binary"
			}
			providers.Set(tag, js.Obj("tag", tag, "type", "remote", "format", f, "url", PinRemoteRuleURL(valueOrUndefined(src.Singbox), true),
				"update_interval", "24h", "download_detour", DNSProxyGroup))
		}
	}
	return providers
}

// GetSingboxDNSRuleSet 对应 getSingboxDnsRuleSet。
func GetSingboxDNSRuleSet() *js.Object {
	return GetRemoteProviderDefinitions("singbox", []any{js.Obj("type", "rule_set", "tag", SingboxCNRuleSet)}).Get(SingboxCNRuleSet).(*js.Object)
}
