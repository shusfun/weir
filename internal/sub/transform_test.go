package sub

import (
	"testing"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

func anys(list []any) *js.Array { return &js.Array{Items: list} }

func mustJSON(t *testing.T, s string) any {
	t.Helper()
	v, err := js.JSONParse(s)
	if err != nil {
		t.Fatalf("fixture JSON 无效: %v\n%s", err, s)
	}
	return v
}

func TestDSLParity(t *testing.T) {
	ctx := js.Obj("name", "🇭🇰 香港 IPLC 01", "protocol", "vless", "region", "HK", "regionZh", "香港", "port", "443",
		"server", "hk.example.com", "index", 3.0, "emoji", "🇭🇰", "n", js.Null)
	exprs := []string{
		"", "protocol === 'vless'", "protocol !== \"vless\"", "port >= 400 && port < 500", "index > 5 || region === 'HK'",
		"contains(name, 'iplc')", "match(name, '^🇭🇰', 'u')", "match(name, '(bad')", "upper(protocol) === 'VLESS'",
		"fallback(missing, '', regionZh)", "pick(true, 'a', 'b') === 'a'", "pick(0, 'a')", "unknownFn(1)", "name",
		"__proto__", "constructor", "n", "1.5 >= 1", "'a\\'b' === \"a'b\"", "true", "false", "null",
		"replace(name, '\\\\d+', 'N') === '🇭🇰 香港 IPLC N'", "title(lower(protocol))", "trim('  x  ') === 'x'",
		"a ||b", "x === y === z",
	}
	var cases []oracleCase
	for _, e := range exprs {
		e := e
		cases = append(cases, oracleCase{Name: "evaluateDslExpression/" + e, Mod: "utils/expression-dsl.js", Fn: "evaluateDslExpression",
			Args: []any{e, ctx}, Go: func() (any, error) { return EvaluateDslExpression(e, ctx), nil }})
	}
	templates := []string{
		"{emoji} {upper(regionZh)}-{index}", "{name}", "{missing}{n}", "{replace(name, ' ', '_', 'g')}", "{ title(protocol) } {port}",
		"{fallback(missing, server)}", "{pick(match(name, 'IPLC'), '专线', '普通')}", "无变量", "{{name}}", "{replace(name, '(\\\\d+)', '#$1')}",
		"{contains(name, 'HK')}", "{1.50}", "{'lit'}",
	}
	for _, tp := range templates {
		tp := tp
		cases = append(cases, oracleCase{Name: "renderDslTemplate/" + tp, Mod: "utils/expression-dsl.js", Fn: "renderDslTemplate",
			Args: []any{tp, ctx}, Go: func() (any, error) { return RenderDslTemplate(tp, ctx), nil }})
	}
	conditions := []any{
		"protocol === 'vless'", js.Obj("field", "name", "op", "contains", "value", "iplc"),
		js.Obj("field", "port", "op", "eq", "value", 443.0), js.Obj("field", "protocol", "op", "in", "value", strs("trojan", "vless")),
		js.Obj("field", "name", "op", "regex", "value", "^🇭🇰", "flags", "u"), js.Obj("op", "not_contains", "value", "美国"),
		js.Obj("field", "bad field", "op", "eq", "value", ""), js.Obj("op", "unknown"), nil, js.Null, 5.0, strs("x"),
	}
	for i, c := range conditions {
		c := c
		cases = append(cases, oracleCase{Name: "matchesDslCondition/" + js.NumberToString(float64(i)), Mod: "utils/expression-dsl.js",
			Fn: "matchesDslCondition", Args: []any{ctx, c}, Go: func() (any, error) { return MatchesDslCondition(ctx, c), nil }})
	}
	runOracle(t, cases)
}

func TestTransformerParity(t *testing.T) {
	configs := []string{
		`{"enabled": false}`,
		`{"enabled": true}`,
		`{"enabled": true, "filter": {"include": {"enabled": true, "rules": ["香港|日本", {"pattern": "US", "flags": ""}]}, "exclude": {"enabled": true, "rules": ["(bad", "x2"]}}}`,
		`{"enabled": true, "filter": {"protocols": {"enabled": true, "values": ["VLESS", "hy2", "ss"]}, "regions": {"enabled": true, "values": ["HK", "日本", "US", "TW"]}, "useless": {"enabled": true}}}`,
		`{"enabled": true, "filter": {"script": {"enabled": true, "expression": "protocol === 'vless' || index > 20"}}}`,
		`{"enabled": true, "filter": {"useless": {"enabled": true}}, "rename": {"regex": {"enabled": true, "rules": [{"pattern": "(\\d+)$", "replacement": "No.$1"}, {"pattern": "(?<word>节点)", "replacement": "[$<word>]", "flags": "g"}, "IPv6"]}}}`,
		`{"enabled": true, "rename": {"template": {"enabled": true, "template": "{emoji}{region:zh}-{protocol:UPPER}-{index:3}-{g1}", "indexScope": "region", "indexStart": 0, "regionAlias": {"HK": "港"}, "protocolAlias": {"vless": "VL"}}, "regex": {"enabled": true, "rules": [{"pattern": "(\\w+)"}]}}}`,
		`{"enabled": true, "enableEmoji": false, "rename": {"template": {"enabled": true}}}`,
		`{"enabled": true, "rename": {"script": {"enabled": true, "expression": "{regionAlias}-{protocolAlias}-{index}"}}}`,
		`{"enabled": true, "rename": {"script": {"enabled": true, "expression": "upper(name)"}}, "filter": {"regions": {"enabled": true, "values": ["HK", "JP", "US"]}}}`,
		`{"enabled": true, "dedup": {"enabled": true}, "sort": {"enabled": true}}`,
		`{"enabled": true, "dedup": {"enabled": true, "mode": "url"}, "sort": {"enabled": true, "keys": [{"key": "protocol", "order": "desc"}, {"key": "port"}, {"key": "server"}]}}`,
		`{"enabled": true, "dedup": {"enabled": true, "includeProtocol": true, "prefer": {"protocolOrder": ["Trojan", "vless"]}}, "sort": {"enabled": true, "nameIgnoreEmoji": false, "keys": [{"key": "name", "order": "desc"}, {"key": "group"}, {"key": "url"}]}}`,
	}
	links := append(append([]string{}, sampleLinks...), sampleLinks[0], sampleLinks[6], "  ", "")
	var cases []oracleCase
	for i, c := range configs {
		cfg := mustJSON(t, c)
		cases = append(cases, oracleCase{Name: "applyNodeTransformPipeline/" + js.NumberToString(float64(i)), Mod: "utils/node-transformer.js",
			Fn: "applyNodeTransformPipeline", Args: []any{strs(links...), cfg},
			Go: func() (any, error) { return anys(ApplyNodeTransformPipeline(links, cfg)), nil }})
	}
	records := NodeURLsToRecords(sampleLinks, true, true)
	cases = append(cases, oracleCase{Name: "nodeUrlsToRecords", Mod: "utils/node-transformer.js", Fn: "nodeUrlsToRecords",
		Args: []any{strs(sampleLinks...), js.Obj("ensureRegion", true, "enableEmoji", true)},
		Go:   func() (any, error) { return objs(NodeURLsToRecords(sampleLinks, true, true)), nil }})
	cases = append(cases, oracleCase{Name: "recordsToNodeUrls", Mod: "utils/node-transformer.js", Fn: "recordsToNodeUrls",
		Args: []any{objs(records)}, Go: func() (any, error) { return anys(RecordsToNodeURLs(records)), nil }})
	for i, link := range sampleLinks {
		link := link
		info := ParseNodeInfo(link)
		cases = append(cases, oracleCase{Name: "setNodeName/" + js.NumberToString(float64(i)), Mod: "utils/node-transformer.js", Fn: "setNodeName",
			Args: []any{link, info.Protocol, "新 名称#1"}, Go: func() (any, error) { return SetNodeName(link, info.Protocol, "新 名称#1"), nil }})
	}
	rec := func() *js.Object { return js.Obj("name", "香港 HK 01 节点", "url", "x") }
	renameRules := mustJSON(t, `[{"pattern": "(?<area>香港) (\\w+) (\\d+)", "replacement": "$2-$<area>-$3"}, "节点", {"pattern": "[", "replacement": "x"}]`)
	cases = append(cases, oracleCase{Name: "applyRegexRename", Mod: "utils/node-transformer.js", Fn: "applyRegexRename", ReturnArgs: true,
		Args: []any{"香港 HK 01 节点", renameRules, rec()},
		Go: func() (any, error) {
			r := rec()
			res := ApplyRegexRename("香港 HK 01 节点", renameRules, r)
			return js.Arr(res, js.Arr("香港 HK 01 节点", renameRules, r)), nil
		}})
	tplRecord := js.Obj("regionZh", "香港", "regexGroups", js.Obj("1", "A", "name", "N"))
	for _, tp := range []string{"{region:zh}-{index:3}-{g1}-{g_name}-{protocol:UPPER}-{name:Title}-{missing}-{port:lower}", "{index:x}{nil}"} {
		tp := tp
		vars := js.Obj("region", "HK", "index", 7.0, "protocol", "vless", "name", "abc", "port", 443.0, "nil", js.Null)
		cases = append(cases, oracleCase{Name: "renderTemplate/" + tp, Mod: "utils/node-transformer.js", Fn: "renderTemplate",
			Args: []any{tp, vars, tplRecord}, Go: func() (any, error) { return RenderTemplate(tp, vars, tplRecord), nil }})
	}
	ruleSets := []any{strs("香港", "(bad"), anys([]any{js.Obj("pattern", "us", "flags", ""), js.Null, ""}), strs()}
	for i, rs := range ruleSets {
		rs := rs
		for j, name := range []any{"香港 01", "US node", nil, 5.0} {
			name := name
			cases = append(cases, oracleCase{Name: "matchesRegexRules/" + js.NumberToString(float64(i*10+j)), Mod: "utils/node-transformer.js",
				Fn: "matchesRegexRules", Args: []any{name, rs}, Go: func() (any, error) { return MatchesRegexRules(name, rs.(*js.Array).Items), nil }})
		}
	}
	runOracle(t, cases)
}

func TestOperatorParity(t *testing.T) {
	chains := []string{
		`[]`,
		`[{"type": "filter", "params": {"include": {"enabled": true, "rules": ["HK\nJP", {"pattern": "美国"}]}}}]`,
		`[{"type": "filter", "params": {"exclude": {"enabled": true, "rules": "流量|到期\n本机"}, "protocols": {"enabled": true, "values": ["VLESS", "trojan", "ss", "hysteria2"]}}}]`,
		`[{"type": "filter", "params": {"regions": {"enabled": true, "values": ["香港", "JP", "US"]}}}]`,
		`[{"type": "rename", "params": {"regex": {"enabled": true, "rules": [{"pattern": "(\\d+)", "replacement": "{regionZh}-$1"}, "节点"]}}}]`,
		`[{"type": "rename", "params": {"template": {"enabled": true, "template": "{emoji} {regionZh} {index:2} [{globalIndex}]", "offset": 0}}}]`,
		`[{"type": "rename", "params": {"template": {"enabled": true, "template": "{protocol}-{index}", "scope": "protocol", "indexStart": 5}}}]`,
		`[{"type": "script", "params": {"dsl": [{"action": "filter", "field": "protocol", "op": "in", "value": ["vless", "trojan"]}, {"action": "rename", "template": "{upper(regionZh)}-{index}-{target}"}]}}]`,
		`[{"type": "script", "params": {"action": "filter", "field": "name", "op": "regex", "value": "^\\S+$"}}]`,
		`[{"type": "script", "params": {"code": "return nodes"}}]`,
		`[{"type": "sort", "params": {"keys": [{"key": "region"}, {"key": "name", "order": "desc"}]}}, {"type": "dedup"}]`,
		`[{"type": "dedup", "params": {"includeProtocol": false}}, {"type": "sort", "params": {"keys": [{"key": "server"}, {"key": "port", "order": "desc"}]}}]`,
		`[{"type": "filter", "enabled": false, "params": {"include": {"enabled": true, "rules": ["nothing"]}}}, {"type": "unknown"}]`,
	}
	var cases []oracleCase
	contexts := []struct {
		js  js.RawJSON
		go_ OperatorContext
	}{
		{`{}`, OperatorContext{}},
		{`{"target": "clash", "userAgent": "clash-verge"}`, OperatorContext{Target: "clash", UserAgent: "clash-verge"}},
	}
	for i, c := range chains {
		ops := mustJSON(t, c)
		for j, ctx := range contexts {
			ctx := ctx
			cases = append(cases, oracleCase{Name: "runOperatorChain/" + js.NumberToString(float64(i*10+j)), Mod: "utils/operator-runner.js",
				Fn: "runOperatorChain", Args: []any{strs(sampleLinks...), ops, ctx.js},
				Go: func() (any, error) { return anys(RunOperatorChain(sampleLinks, ops, ctx.go_)), nil }})
		}
	}
	meta := js.Obj(sampleLinks[0], js.Obj("name", "覆盖名称", "group", "G1"), sampleLinks[1], js.Obj("group", "G0"))
	ops := mustJSON(t, `[{"type": "sort", "params": {"keys": [{"key": "group"}]}}]`)
	cases = append(cases, oracleCase{Name: "runOperatorChain/metadata", Mod: "utils/operator-runner.js", Fn: "runOperatorChain",
		Args: []any{strs(sampleLinks...), ops, js.Obj("nodeMetadataByUrl", meta)},
		Go: func() (any, error) {
			return anys(RunOperatorChain(sampleLinks, ops, OperatorContext{NodeMetadataByURL: meta})), nil
		}})
	runOracle(t, cases)
}

func TestFormatAndUAParity(t *testing.T) {
	uas := []string{"", "clash-verge/v2.4.3", "ClashForWindows/0.20", "mihomo/1.18", "Surge/5.0 iOS", "Surge Mac/4.2", "surge-mac/1.0",
		"Shadowrocket/2.2", "Hiddify/2.0 ClashMeta sing-box", "Stash/2.4", "Quantumult%20X/1.0", "Loon/3.2", "sing-box 1.10",
		"v2rayN/7.0", "Egern/1.0", "Mozilla/5.0 (Windows NT 10.0) Chrome/120 Safari/537", "Mozilla/5.0 curl", "clash.for.windows",
		"FlClash/0.8", "nekobox", "月兔", "Clash Party"}
	params := []string{"", "target=clash", "target=Surge&ver=3", "surge&ver=x", "target=sing-box", "v2ray", "trojan=1", "nodes",
		"target=surge%26ver%3D5", "meta=1", "quanx&target="}
	var cases []oracleCase
	for _, ua := range uas {
		ua := ua
		cases = append(cases, oracleCase{Name: "isBrowserAgent/" + ua, Mod: "modules/subscription/user-agent-utils.js", Fn: "isBrowserAgent",
			Args: []any{ua}, Go: func() (any, error) { return IsBrowserAgent(ua), nil }})
		cases = append(cases, oracleCase{Name: "determineFormatByUserAgent/" + ua, Mod: "utils/format-utils.js", Fn: "determineFormatByUserAgent",
			Args: []any{ua}, Go: func() (any, error) { return DetermineFormatByUserAgent(ua), nil }})
		cases = append(cases, oracleCase{Name: "isHiddifyAgent/" + ua, Mod: "modules/subscription/user-agent-utils.js", Fn: "isHiddifyAgent",
			Args: []any{ua}, Go: func() (any, error) { return IsHiddifyAgent(ua), nil }})
		for _, p := range params {
			p := p
			usp := js.RawJSON(`{"$usp":` + js.MustStringify(p) + `}`)
			cases = append(cases, oracleCase{Name: "determineTargetFormat/" + ua + "?" + p, Mod: "modules/subscription/user-agent-utils.js",
				Fn: "determineTargetFormat", Args: []any{ua, usp}, Go: func() (any, error) { return DetermineTargetFormat(ua, js.ParseSearchParams(p)), nil }})
			cases = append(cases, oracleCase{Name: "isMetaCore/" + ua + "?" + p, Mod: "modules/subscription/user-agent-utils.js",
				Fn: "isMetaCore", Args: []any{ua, usp}, Go: func() (any, error) { return IsMetaCore(ua, js.ParseSearchParams(p)), nil }})
		}
	}
	for _, p := range params {
		p := p
		u := "https://sub.example.com/s?" + p
		cases = append(cases, oracleCase{Name: "determineFormatByUrl/" + p, Mod: "utils/format-utils.js", Fn: "determineFormatByUrl",
			Args: []any{js.RawJSON(`{"$url":` + js.MustStringify(u) + `}`)},
			Go: func() (any, error) {
				pu, _ := js.ParseURL(u)
				return DetermineFormatByURL(pu.SearchParams()), nil
			}})
		cases = append(cases, oracleCase{Name: "getProcessedUserAgent/" + p, Mod: "utils/format-utils.js", Fn: "getProcessedUserAgent",
			Args: []any{"Mozilla", u}, Go: func() (any, error) { return GetProcessedUserAgent("Mozilla", u), nil }})
	}
	for _, s := range []any{"", "abc", "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo=", "YWJj ZGVm\nZ2hpamtsbW5vcHFyc3R1-_", 5.0, "aGVsbG8gd29ybGQgdGhpcyBpcyBsb25n!"} {
		s := s
		cases = append(cases, oracleCase{Name: "isValidBase64/" + js.ToString(s), Mod: "utils/format-utils.js", Fn: "isValidBase64",
			Args: []any{s}, Go: func() (any, error) { return IsValidBase64(s), nil }})
	}
	cases = append(cases, oracleCase{Name: "clashFix", Mod: "utils/format-utils.js", Fn: "clashFix", Args: []any{"type: wireguard\n"},
		Go: func() (any, error) { return ClashFix("type: wireguard\n"), nil }})
	runOracle(t, cases)
}
