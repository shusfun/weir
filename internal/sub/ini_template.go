package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/template-parsers/ini-template-parser.js
// 支持 subconverter / ACL4SSR 风格的 ini 模板（[custom] 段的 ruleset / custom_proxy_group / custom_chain_proxy）。

var (
	reINISection    = js.MustRegExp(`^\[(.+)]$`, "")
	reINILineBreak  = js.MustRegExp(`\r?\n`, "")
	reEqRest        = js.MustRegExp(`=(.*)`, "")
	reRulesetPrefix = js.MustRegExp(`^ruleset=`, "i")
	reRuleSource    = js.MustRegExp(`^(clash-classic|surge|quanx|loon|sing-box|singbox):`, "i")
	reGroupPrefix   = js.MustRegExp(`^custom_proxy_group=`, "i")
	reHTTPPrefix    = js.MustRegExp(`^https?:\/\/`, "i")
	reNumberSeq     = js.MustRegExp(`^\d+(?:,\s*\d*)*$`, "")
	reRegexChars    = js.MustRegExp(`[*+[\]?|]`, "")
	reLeadingSpace  = js.MustRegExp(`^\s*`, "")
	reChainPrefix   = js.MustRegExp(`^custom_chain_proxy=`, "i")
)

type iniSections struct {
	order []string
	lines map[string][]string
}

func (s *iniSections) get(name string) []string { return s.lines[name] }

func parseIniSections(templateText any) *iniSections {
	secs := &iniSections{lines: map[string][]string{}}
	current := "custom"
	ensure := func(name string) {
		if _, ok := secs.lines[name]; !ok {
			secs.order = append(secs.order, name)
			secs.lines[name] = []string{}
		}
	}
	for _, rawLine := range reINILineBreak.Split(strOr(templateText), -1) {
		line := js.Trim(rawLine)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if m := reINISection.MatchFirst(line); m != nil {
			current = js.ToLower(js.Trim(m.Str(1)))
			ensure(current)
			continue
		}
		ensure(current)
		secs.lines[current] = append(secs.lines[current], rawLine)
	}
	return secs
}

func trimNonEmpty(parts []string) []string {
	var out []string
	for _, p := range parts {
		if p = js.Trim(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseGroupLine(line string) *js.Object {
	parts := strings.Split(line, "=")
	if len(parts) < 2 {
		return nil
	}
	name := js.Trim(parts[0])
	body := trimNonEmpty(strings.Split(strings.Join(parts[1:], "="), ","))
	if len(body) == 0 {
		return nil
	}
	members := js.Arr()
	options := js.NewObject()
	for _, part := range body[1:] {
		if strings.Contains(part, "=") {
			kv := reEqRest.Split(part, 2)
			value := ""
			if len(kv) > 1 {
				value = kv[1]
			}
			options.Set(js.Trim(kv[0]), js.Trim(value))
		} else {
			members.Push(part)
		}
	}
	return js.Obj("name", name, "type", body[0], "members", members, "options", options)
}

func parseRuleLine(line string) *js.Object {
	parts := trimNonEmpty(strings.Split(line, ","))
	if len(parts) < 2 {
		return nil
	}
	value, policy := "", parts[1]
	if len(parts) > 2 {
		value, policy = parts[1], parts[2]
	}
	extras := js.Arr()
	if len(parts) > 3 {
		extras = js.StrArr(parts[3:])
	}
	return js.Obj("type", js.ToLower(parts[0]), "value", value, "policy", policy, "extras", extras)
}

func parseAclRuleSetLine(line string) *js.Object {
	raw := reRulesetPrefix.Replace(line, "")
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = js.Trim(parts[i])
	}
	if len(parts) < 2 {
		return nil
	}
	policy := parts[0]
	source := reRuleSource.Replace(strings.Join(parts[1:], ","), "")
	if strings.HasPrefix(source, "[]") {
		inline := trimNonEmpty(strings.Split(source[2:], ","))
		typ, value := "", ""
		if len(inline) > 0 {
			typ = js.ToLower(inline[0])
		}
		if len(inline) > 1 {
			value = inline[1]
		}
		extras := js.Arr()
		if len(inline) > 2 {
			extras = js.StrArr(inline[2:])
		}
		return js.Obj("type", typ, "value", value, "policy", policy, "source", "inline", "extras", extras)
	}
	return js.Obj("type", "rule-set", "value", PinRemoteRuleURL(source, true), "policy", policy, "source", "remote", "extras", js.Arr())
}

func parseAclProxyGroupLine(line string) *js.Object {
	raw := reGroupPrefix.Replace(line, "")
	parts := trimNonEmpty(strings.Split(raw, "`"))
	if len(parts) < 2 {
		return nil
	}
	members, filters, options := js.Arr(), js.Arr(), js.NewObject()
	for _, part := range parts[2:] {
		switch {
		case strings.HasPrefix(part, "[]"):
			members.Push(part[2:])
		case reHTTPPrefix.Test(part):
			options.Set("url", part)
		case reNumberSeq.Test(part):
			var nums []float64
			for _, s := range strings.Split(part, ",") {
				if s = js.Trim(s); s != "" {
					nums = append(nums, js.StringToNumber(s))
				}
			}
			if len(nums) > 0 {
				if !js.Truthy(options.Get("interval")) {
					options.Set("interval", nums[0])
				}
				if len(nums) > 1 && !js.Truthy(options.Get("tolerance")) {
					options.Set("tolerance", nums[1])
				}
			}
		case part == ".*" || (strings.HasPrefix(part, "(") && strings.HasSuffix(part, ")")):
			if part == ".*" {
				filters.Push(".*")
			} else {
				filters.Push(js.Slice(part, 1, -1))
			}
		case part == ",":
		default:
			if part != "" && !strings.Contains(part, "=") {
				if reRegexChars.Test(part) {
					filters.Push(part)
				} else {
					members.Push(part)
				}
			}
		}
	}
	return js.Obj("name", parts[0], "type", parts[1], "members", members, "filters", filters, "options", options)
}

func parseChainProxyLine(line string) *js.Object {
	raw := reChainPrefix.Replace(reLeadingSpace.Replace(line, ""), "")
	parts := strings.Split(raw, "`")
	group := js.Trim(parts[0])
	entry := ""
	if len(parts) > 1 {
		entry = js.Trim(parts[1])
	}
	if group == "" || entry == "" || group == entry {
		return nil
	}
	prefix := ""
	if len(parts) > 2 {
		prefix = parts[2]
	}
	return js.Obj("group", group, "entry", entry, "prefix", prefix)
}

// ParseIniTemplate 对应 parseIniTemplate，返回统一模板模型。
func ParseIniTemplate(templateText any, options *js.Object) *js.Object {
	secs := parseIniSections(templateText)
	custom := secs.get("custom")
	groups := js.Arr()
	rules := js.Arr()
	chains := js.Arr()
	for _, l := range secs.get("proxy group") {
		if g := parseGroupLine(l); g != nil {
			groups.Push(g)
		}
	}
	for _, l := range custom {
		if reGroupPrefix.Test(js.Trim(l)) {
			if g := parseAclProxyGroupLine(l); g != nil {
				groups.Push(g)
			}
		}
	}
	for _, l := range secs.get("rule") {
		if r := parseRuleLine(l); r != nil {
			rules.Push(r)
		}
	}
	for _, l := range custom {
		if reRulesetPrefix.Test(js.Trim(l)) {
			if r := parseAclRuleSetLine(l); r != nil {
				rules.Push(r)
			}
		}
	}
	for _, l := range custom {
		if reChainPrefix.Test(js.Trim(l)) {
			if c := parseChainProxyLine(l); c != nil {
				chains.Push(c)
			}
		}
	}
	opt := options.Get
	extras := js.NewObject()
	if chains.Len() > 0 {
		extras.Set("chainProxy", chains)
	}
	return CreateUnifiedTemplateModel(js.Obj(
		"meta", js.Obj("name", js.Or(opt("fileName"), "MiSub"), "source", "ini", "target", js.Or(opt("targetFormat"), "clash"),
			"ruleLevel", js.Or(opt("ruleLevel"), "std"), "isMeta", js.Truthy(opt("isMeta"))),
		"proxies", js.Or(opt("proxies"), js.Arr()),
		"groups", groups,
		"rules", rules,
		"settings", js.Obj("managedConfigUrl", js.Or(opt("managedConfigUrl"), ""), "interval", js.Or(opt("interval"), 86400.0),
			"skipCertVerify", js.Truthy(opt("skipCertVerify")), "enableUdp", js.Truthy(opt("enableUdp")),
			"customDnsOverride", js.Or(opt("customDnsOverride"), ""), "dnsMode", js.Or(opt("dnsMode"), "clean")),
		"extras", extras,
	))
}
