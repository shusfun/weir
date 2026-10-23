package sub

import (
	"strings"
	"testing"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

func nullIfFalse(s string, ok bool) any {
	if !ok {
		return js.Null
	}
	return s
}

func mustYAML(t *testing.T, s string) any {
	t.Helper()
	v, err := js.YAMLLoad(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestParseParity(t *testing.T) {
	var cases []oracleCase
	add := func(c oracleCase) { cases = append(cases, c) }
	allLinks := strings.Join(sampleLinks, "\n")
	b64 := js.Base64EncodeUTF8(allLinks)
	urlSafeB64 := strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(b64), "=")
	texts := map[string]string{
		"links": allLinks, "base64": b64, "base64-urlsafe": urlSafeB64, "clash": clashYAMLFixture,
		"singbox": singboxJSONFixture, "surge": surgeFixture, "crlf": strings.ReplaceAll(allLinks, "\n", "\r\n"),
		"empty": "", "garbage": "hello world\nnot a node",
		"yaml-bad": "proxies:\n  - name: a\n    name: b\n", "json-nonsb": `{"a":1}`,
	}
	for name, text := range texts {
		text := text
		add(oracleCase{Name: "extractValidNodes/" + name, Mod: "modules/utils/node-parser.js", Fn: "extractValidNodes",
			Args: []any{text}, Go: func() (any, error) { return ExtractValidNodes(text), nil }})
		add(oracleCase{Name: "parseNodeList/" + name, Mod: "modules/utils/node-parser.js", Fn: "parseNodeList",
			Args: []any{text}, Go: func() (any, error) { return objs(ParseNodeList(text, FixOptions{})), nil }})
	}
	add(oracleCase{Name: "parseNodeList/plusAsSpace", Mod: "modules/utils/node-parser.js", Fn: "parseNodeList",
		Args: []any{allLinks, js.Obj("plusAsSpace", true)},
		Go:   func() (any, error) { return objs(ParseNodeList(allLinks, FixOptions{PlusAsSpace: true})), nil }})

	for i, link := range sampleLinks {
		link := link
		n := js.NumberToString(float64(i))
		add(oracleCase{Name: "parseNodeInfo/" + n, Mod: "modules/utils/geo-utils.js", Fn: "parseNodeInfo",
			Args: []any{link}, Go: func() (any, error) { return ParseNodeInfo(link).Object(), nil }})
		add(oracleCase{Name: "urlToClashProxy/" + n, Mod: "utils/url-to-clash.js", Fn: "urlToClashProxy",
			Args: []any{link}, Go: func() (any, error) { return URLToClashProxy(link), nil }})
		add(oracleCase{Name: "fixNodeUrlEncoding(utils)/" + n, Mod: "utils/node-utils.js", Fn: "fixNodeUrlEncoding",
			Args: []any{link}, Go: func() (any, error) { return FixNodeURLEncoding(link, FixOptions{}), nil }})
		add(oracleCase{Name: "fixNodeUrlEncoding(cleaner)/" + n, Mod: "modules/utils/node-cleaner.js", Fn: "fixNodeUrlEncoding",
			Args: []any{link, js.Obj("plusAsSpace", true)}, Go: func() (any, error) { return CleanerFixNodeURLEncoding(link, FixOptions{PlusAsSpace: true}), nil }})
		add(oracleCase{Name: "addFlagEmoji/" + n, Mod: "utils/node-utils.js", Fn: "addFlagEmoji",
			Args: []any{link}, Go: func() (any, error) { return AddFlagEmoji(link), nil }})
		add(oracleCase{Name: "removeFlagEmoji/" + n, Mod: "utils/node-utils.js", Fn: "removeFlagEmoji",
			Args: []any{link}, Go: func() (any, error) { return RemoveFlagEmoji(link), nil }})
		add(oracleCase{Name: "prependNodeName/" + n, Mod: "utils/node-utils.js", Fn: "prependNodeName",
			Args: []any{link, "前缀"}, Go: func() (any, error) { return PrependNodeName(link, "前缀"), nil }})
		add(oracleCase{Name: "sanitizeNodeForYaml/" + n, Mod: "utils/node-utils.js", Fn: "sanitizeNodeForYaml",
			Args: []any{link}, Go: func() (any, error) { return SanitizeNodeForYaml(link), nil }})
		add(oracleCase{Name: "isLocalProxyEndpoint/" + n, Mod: "utils/node-utils.js", Fn: "isLocalProxyEndpoint",
			Args: []any{link}, Go: func() (any, error) { return IsLocalProxyEndpoint(link), nil }})
		add(oracleCase{Name: "validateSS2022Node/" + n, Mod: "modules/utils/ss2022-validator.js", Fn: "validateSS2022Node",
			Args: []any{link}, Go: func() (any, error) { return ValidateSS2022Node(link), nil }})
		add(oracleCase{Name: "fixSS2022Node/" + n, Mod: "modules/utils/ss2022-validator.js", Fn: "fixSS2022Node",
			Args: []any{link}, Go: func() (any, error) { return FixSS2022Node(link), nil }})
		add(oracleCase{Name: "applyManualNodeName/" + n, Mod: "modules/utils/node-cleaner.js", Fn: "applyManualNodeName",
			Args: []any{link, "自定义 名称/#?"}, Go: func() (any, error) { return ApplyManualNodeName(link, "自定义 名称/#?"), nil }})
		add(oracleCase{Name: "validateSnellNode/" + n, Mod: "modules/utils/node-parser.js", Fn: "validateSnellNode",
			Args: []any{link}, Go: func() (any, error) { return ValidateSnellNode(link), nil }})
		// Go 端生成的代理对象作为共同输入，单独对照 Clash → URL 的转换
		if proxy := URLToClashProxy(link); proxy != nil {
			add(oracleCase{Name: "convertClashProxyToUrl/" + n, Mod: "utils/clash-to-url.js", Fn: "convertClashProxyToUrl",
				Args: []any{proxy}, Go: func() (any, error) { return nullIfFalse(ConvertClashProxyToURL(proxy)), nil }})
		}
	}
	clashDoc := mustYAML(t, clashYAMLFixture)
	for i, p := range objGet(clashDoc, "proxies").(*js.Array).Items {
		p := p
		add(oracleCase{Name: "convertClashProxyToUrl/yaml-" + js.NumberToString(float64(i)), Mod: "utils/clash-to-url.js", Fn: "convertClashProxyToUrl",
			Args: []any{p}, Go: func() (any, error) { return nullIfFalse(ConvertClashProxyToURL(p)), nil }})
	}
	sbDoc, _ := js.JSONParse(singboxJSONFixture)
	add(oracleCase{Name: "extractSingboxNodes", Mod: "modules/utils/singbox-to-url.js", Fn: "extractSingboxNodes",
		Args: []any{sbDoc}, Go: func() (any, error) { return ExtractSingboxNodes(sbDoc), nil }})

	optionSets := []*js.Object{
		js.NewObject(),
		js.Obj("enableTfo", true, "enableUdp", false, "skipCertVerify", true, "supportX25519mlkem768", false),
		js.Obj("enableUdp", true, "addFlagEmoji", false, "supportX25519mlkem768", true),
	}
	for i, opts := range optionSets {
		opts := opts
		add(oracleCase{Name: "urlsToClashProxies/" + js.NumberToString(float64(i)), Mod: "utils/url-to-clash.js", Fn: "urlsToClashProxies",
			Args: []any{strs(sampleLinks...), opts}, Go: func() (any, error) { return objs(URLsToClashProxies(sampleLinks, opts)), nil }})
		add(oracleCase{Name: "generateClashConfig/" + js.NumberToString(float64(i)), Mod: "utils/url-to-clash.js", Fn: "generateClashConfig",
			Args: []any{strs(sampleLinks...), opts}, Go: func() (any, error) { return GenerateClashConfig(sampleLinks, opts), nil }})
	}
	names := []any{"🇭🇰 香港 IPLC 01 [x1.5]", "Japan Tokyo 2倍 (BGP) #tag_one", "US-LAX x3 [Netflix]", "kristi user", "", nil, 5.0,
		"🇹🇼 台湾", "Singapore SG", "日本 东京 0.5x", "未知地区 node", "uk london", "UAE Dubai", "Hong Kong-HKT"}
	for i, name := range names {
		name := name
		n := js.NumberToString(float64(i))
		add(oracleCase{Name: "extractNodeMetadata/" + n, Mod: "modules/utils/metadata-extractor.js", Fn: "extractNodeMetadata",
			Args: []any{name}, Go: func() (any, error) { return ExtractNodeMetadata(name), nil }})
		add(oracleCase{Name: "extractNodeRegion/" + n, Mod: "modules/utils/geo-utils.js", Fn: "extractNodeRegion",
			Args: []any{name}, Go: func() (any, error) { return ExtractNodeRegion(name), nil }})
		add(oracleCase{Name: "getRegionEmoji/" + n, Mod: "modules/utils/geo-utils.js", Fn: "getRegionEmoji",
			Args: []any{name}, Go: func() (any, error) { return GetRegionEmoji(name), nil }})
	}
	parsed := ParseNodeList(allLinks, FixOptions{})
	add(oracleCase{Name: "calculateProtocolStats", Mod: "modules/utils/node-parser.js", Fn: "calculateProtocolStats",
		Args: []any{objs(parsed)}, Go: func() (any, error) { return CalculateProtocolStats(parsed), nil }})
	add(oracleCase{Name: "calculateRegionStats", Mod: "modules/utils/node-parser.js", Fn: "calculateRegionStats",
		Args: []any{objs(parsed)}, Go: func() (any, error) { return CalculateRegionStats(parsed), nil }})
	add(oracleCase{Name: "removeDuplicateNodes", Mod: "modules/utils/node-parser.js", Fn: "removeDuplicateNodes",
		Args: []any{objs(append(parsed, parsed...))}, Go: func() (any, error) { return objs(RemoveDuplicateNodes(append(parsed, parsed...))), nil }})
	add(oracleCase{Name: "validateNodeList", Mod: "modules/utils/ss2022-validator.js", Fn: "validateNodeList",
		Args: []any{strs(sampleLinks...)}, Go: func() (any, error) { return ValidateNodeList(strs(sampleLinks...)), nil }})
	for _, c := range []any{3.0, -1.0, "3", 0.0} {
		c := c
		add(oracleCase{Name: "formatNodeCount/" + js.ToString(c), Mod: "modules/utils/node-parser.js", Fn: "formatNodeCount",
			Args: []any{c}, Go: func() (any, error) { return FormatNodeCount(c), nil }})
	}
	runOracle(t, cases)
}

func TestCleanerParity(t *testing.T) {
	var cases []oracleCase
	add := func(c oracleCase) { cases = append(cases, c) }
	ruleTexts := []any{
		"香港\n日本\n---\nkeep:美国\nproto:vless, Trojan",
		"proto:ss\n(bad",
		strs("  IPLC ", "", "---", "keep: 新加坡"),
		"",
	}
	for i, rt := range ruleTexts {
		rt := rt
		n := js.NumberToString(float64(i))
		add(oracleCase{Name: "parseFilterRuleText/" + n, Mod: "modules/utils/node-cleaner.js", Fn: "parseFilterRuleText",
			Args: []any{rt}, Go: func() (any, error) { return ParseFilterRuleText(rt).Value(), nil }})
		add(oracleCase{Name: "buildRuleSet/" + n, Mod: "modules/utils/node-cleaner.js", Fn: "buildRuleSet",
			Args: []any{rt, true}, Go: func() (any, error) { return BuildRuleSet(rt, true).Value(), nil }})
	}
	subs := []*js.Object{
		js.Obj("filterInclude", "香港|日本\nproto:trojan"),
		js.Obj("filterExclude", "流量|到期\n---\nkeep:美国"),
		js.Obj("filterInclude", "  ", "filterExclude", "proto:ss"),
	}
	for i, s := range subs {
		s := s
		add(oracleCase{Name: "applyFilterRules/" + js.NumberToString(float64(i)), Mod: "modules/utils/node-cleaner.js", Fn: "applyFilterRules",
			Args: []any{strs(sampleLinks...), s}, Go: func() (any, error) { return ApplyFilterRules(sampleLinks, s), nil }})
	}
	for _, u := range []string{"ss://YWVz%2DMjU2:pw@h:1#a%2520b+c", "ss://YWVzLTI1Ni1nY206cHc%3D#x", "ss://abc"} {
		u := u
		add(oracleCase{Name: "fixSSEncoding/" + u, Mod: "modules/utils/node-cleaner.js", Fn: "fixSSEncoding",
			Args: []any{u, js.Obj("plusAsSpace", true)}, Go: func() (any, error) { return FixSSEncoding(u, FixOptions{PlusAsSpace: true}), nil }})
	}
	runOracle(t, cases)
}
