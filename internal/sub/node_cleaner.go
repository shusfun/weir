package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/modules/utils/node-cleaner.js

func decodeRepeatedly(value string, maxRounds int) string {
	current := value
	for i := 0; i < maxRounds; i++ {
		next := js.SafeDecodeURIComponent(current)
		if next == current {
			break
		}
		current = next
	}
	return current
}

// CleanerFixNodeURLEncoding 对应 node-cleaner.js 的 fixNodeUrlEncoding（与 node-utils.js 的同名函数逻辑不同）。
func CleanerFixNodeURLEncoding(nodeURL string, options FixOptions) string {
	if nodeURL == "" {
		return ""
	}
	normalizeFragment := func(u string) string {
		hashIndex := strings.LastIndex(u, "#")
		if hashIndex == -1 {
			return u
		}
		base := u[:hashIndex+1]
		raw := u[hashIndex+1:]
		if raw == "" {
			return u
		}
		if options.PlusAsSpace {
			raw = strings.ReplaceAll(raw, "+", " ")
		}
		return base + js.EncodeURIComponent(decodeRepeatedly(raw, 2))
	}
	if strings.HasPrefix(nodeURL, "vmess://") {
		return nodeURL
	}
	if strings.HasPrefix(nodeURL, "ss://") {
		return FixSSEncoding(nodeURL, options)
	}
	if strings.HasPrefix(nodeURL, "trojan://") || strings.HasPrefix(nodeURL, "vless://") ||
		strings.HasPrefix(nodeURL, "hy2://") || strings.HasPrefix(nodeURL, "hysteria2://") {
		u, err := js.ParseURL(nodeURL)
		if err != nil {
			return normalizeFragment(nodeURL)
		}
		if h := u.Hash(); h != "" {
			raw := h[1:]
			if options.PlusAsSpace {
				raw = strings.ReplaceAll(raw, "+", " ")
			}
			u.SetHash("#" + js.EncodeURIComponent(decodeRepeatedly(raw, 2)))
		}
		return u.Href()
	}
	return nodeURL
}

// FixSSEncoding 对应 fixSSEncoding。
func FixSSEncoding(nodeURL string, options FixOptions) string {
	if !strings.HasPrefix(nodeURL, "ss://") {
		return nodeURL
	}
	payload := nodeURL[len("ss://"):]
	hashIndex := strings.Index(payload, "#")
	beforeHash, rawHash := payload, ""
	if hashIndex != -1 {
		beforeHash, rawHash = payload[:hashIndex], payload[hashIndex+1:]
	}
	var normalized string
	if atIndex := strings.Index(beforeHash, "@"); atIndex != -1 {
		normalized = decodeRepeatedly(beforeHash[:atIndex], 2) + beforeHash[atIndex:]
	} else {
		normalized = decodeRepeatedly(beforeHash, 2)
	}
	if rawHash == "" {
		return "ss://" + normalized
	}
	if options.PlusAsSpace {
		rawHash = strings.ReplaceAll(rawHash, "+", " ")
	}
	return "ss://" + normalized + "#" + js.EncodeURIComponent(decodeRepeatedly(rawHash, 2))
}

// EncodeArrayBufferToBase64 对应 encodeArrayBufferToBase64。
func EncodeArrayBufferToBase64(b []byte) string { return js.BtoaBytes(b) }

// RuleSet 对应 buildRuleSet 的返回值。
type RuleSet struct {
	Protocols []string // 保持插入顺序的集合
	NameRegex *js.RegExp
	HasRules  bool
}

// Has 判断协议是否在集合中。
func (r *RuleSet) Has(p string) bool {
	for _, x := range r.Protocols {
		if x == p {
			return true
		}
	}
	return false
}

// Value 转换为与上游一致的 JS 对象（Set / RegExp / null）。
func (r *RuleSet) Value() *js.Object {
	set := &js.JSSet{}
	for _, p := range r.Protocols {
		set.Items = append(set.Items, p)
	}
	var re any = js.Null
	if r.NameRegex != nil {
		re = r.NameRegex
	}
	return js.Obj("protocols", set, "nameRegex", re, "hasRules", r.HasRules)
}

// linesOf 对应 `Array.isArray(lines) ? lines : typeof lines === 'string' ? lines.split('\n') : []`。
func linesOf(lines any) []any {
	switch x := lines.(type) {
	case *js.Array:
		return x.Items
	case []string:
		return js.StrArr(x).Items
	case string:
		return js.StrArr(strings.Split(x, "\n")).Items
	}
	return nil
}

// BuildRuleSet 对应 buildRuleSet。
func BuildRuleSet(lines any, stripKeepPrefix bool) *RuleSet {
	rs := &RuleSet{Protocols: []string{}}
	var patterns []string
	for _, raw := range linesOf(lines) {
		line := js.Trim(js.ToString(raw))
		if line == "" || line == "---" {
			continue
		}
		if stripKeepPrefix && strings.HasPrefix(js.ToLower(line), "keep:") {
			line = js.Trim(line[len("keep:"):])
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(js.ToLower(line), "proto:") {
			for _, p := range strings.Split(line[len("proto:"):], ",") {
				p = js.ToLower(js.Trim(p))
				if p != "" && !rs.Has(p) {
					rs.Protocols = append(rs.Protocols, p)
				}
			}
			continue
		}
		patterns = append(patterns, line)
	}
	rs.NameRegex = BuildSafeRegex(patterns)
	rs.HasRules = len(rs.Protocols) > 0 || rs.NameRegex != nil
	return rs
}

// BuildSafeRegex 对应 buildSafeRegex：非法正则返回 nil。
func BuildSafeRegex(patterns []string) *js.RegExp {
	if len(patterns) == 0 {
		return nil
	}
	re, err := js.NewRegExp(strings.Join(patterns, "|"), "i")
	if err != nil {
		return nil
	}
	return re
}

// FilterRules 对应 parseFilterRuleText 的返回值。
type FilterRules struct {
	IncludeLines []string
	ExcludeLines []string
	IncludeRules *RuleSet
	ExcludeRules *RuleSet
}

// Value 转换为与上游一致的 JS 对象。
func (f *FilterRules) Value() *js.Object {
	return js.Obj("includeLines", js.StrArr(f.IncludeLines), "excludeLines", js.StrArr(f.ExcludeLines),
		"includeRules", f.IncludeRules.Value(), "excludeRules", f.ExcludeRules.Value())
}

// ParseFilterRuleText 对应 parseFilterRuleText。
func ParseFilterRuleText(ruleText any) *FilterRules {
	var lines []string
	if a, ok := ruleText.(*js.Array); ok {
		for _, l := range a.Items {
			if s := js.Trim(strOr(l)); s != "" {
				lines = append(lines, s)
			}
		}
	} else {
		for _, l := range strings.Split(strOr(ruleText), "\n") {
			if s := js.Trim(l); s != "" {
				lines = append(lines, s)
			}
		}
	}
	f := &FilterRules{IncludeLines: []string{}, ExcludeLines: []string{}}
	afterDivider := false
	for _, line := range lines {
		if line == "---" {
			afterDivider = true
			continue
		}
		if strings.HasPrefix(js.ToLower(line), "keep:") {
			f.IncludeLines = append(f.IncludeLines, line)
			continue
		}
		if afterDivider {
			f.IncludeLines = append(f.IncludeLines, line)
		} else {
			f.ExcludeLines = append(f.ExcludeLines, line)
		}
	}
	f.IncludeRules = BuildRuleSet(js.StrArr(f.IncludeLines), true)
	f.ExcludeRules = BuildRuleSet(js.StrArr(f.ExcludeLines), false)
	return f
}

// FilterNodeObjects 对应 filterNodeObjects；节点需含 protocol 与 name。
func FilterNodeObjects(nodes []*js.Object, rules *RuleSet, mode string) []*js.Object {
	if rules == nil || !rules.HasRules {
		return nodes
	}
	include := mode == "include"
	out := []*js.Object{}
	for _, n := range nodes {
		protocol := js.ToLower(strOr(n.Get("protocol")))
		name := js.ToString(js.Or(n.Get("name"), ""))
		protocolHit := protocol != "" && rules.Has(protocol)
		nameHit := rules.NameRegex != nil && rules.NameRegex.Test(name)
		if include == (protocolHit || nameHit) {
			out = append(out, n)
		}
	}
	return out
}

// FilterNodeURLs 对应 filterNodeUrls。
func FilterNodeURLs(nodeURLs []string, rules *RuleSet, mode string) []string {
	if rules == nil || !rules.HasRules || nodeURLs == nil {
		return nodeURLs
	}
	nodes := make([]*js.Object, len(nodeURLs))
	for i, u := range nodeURLs {
		protocol := ""
		if m := reProtocolLazy.MatchFirst(u); m != nil {
			protocol = js.ToLower(m.Str(1))
		}
		nodes[i] = js.Obj("url", u, "protocol", protocol, "name", parseNodeURLName(u))
	}
	kept := map[string]bool{}
	for _, n := range FilterNodeObjects(nodes, rules, mode) {
		kept[js.ToString(n.Get("url"))] = true
	}
	out := []string{}
	for _, u := range nodeURLs {
		if kept[u] {
			out = append(out, u)
		}
	}
	return out
}

func parseNodeURLName(nodeURL string) string {
	if nodeURL == "" {
		return ""
	}
	hashIndex := strings.LastIndex(nodeURL, "#")
	if hashIndex == -1 {
		return nodeURL
	}
	return js.SafeDecodeURIComponent(nodeURL[hashIndex+1:])
}

// ApplyManualNodeName 对应 applyManualNodeName。
func ApplyManualNodeName(nodeURL, customName string) string {
	if customName == "" || nodeURL == "" {
		return nodeURL
	}
	encodedName := js.EncodeURIComponent(customName)
	cut := func() string {
		if i := strings.Index(nodeURL, "#"); i != -1 {
			return nodeURL[:i]
		}
		return nodeURL
	}
	if strings.HasPrefix(nodeURL, "vmess://") {
		jsonStr, err := js.Atob(nodeURL[8:])
		if err != nil {
			return nodeURL
		}
		cfgVal, err := js.JSONParse(jsonStr)
		if err != nil {
			return nodeURL
		}
		switch cfg := cfgVal.(type) {
		case *js.Object:
			cfg.Set("ps", customName)
		case *js.Array:
		default:
			// 在严格模式下给原始值或 null 赋属性会抛出异常
			return nodeURL
		}
		out, err := js.Btoa(js.MustStringify(cfgVal))
		if err != nil {
			return nodeURL
		}
		return "vmess://" + out
	}
	if strings.HasPrefix(nodeURL, "ss://") {
		return cut() + "#" + encodedName
	}
	if strings.HasPrefix(nodeURL, "trojan://") || strings.HasPrefix(nodeURL, "vless://") ||
		strings.HasPrefix(nodeURL, "hysteria2://") || strings.HasPrefix(nodeURL, "hy2://") || strings.HasPrefix(nodeURL, "tuic://") {
		u, err := js.ParseURL(nodeURL)
		if err != nil {
			return cut() + "#" + encodedName
		}
		u.SetHash("#" + encodedName)
		return u.Href()
	}
	return cut() + "#" + encodedName
}

// ApplyFilterRules 对应 applyFilterRules；sub 需含 filterInclude / filterExclude。
func ApplyFilterRules(validNodes []string, sub *js.Object) []string {
	if sub == nil || validNodes == nil {
		return validNodes
	}
	filtered := append([]string(nil), validNodes...)
	for _, key := range []string{"filterInclude", "filterExclude"} {
		v := sub.Get(key)
		if !js.Truthy(v) || js.Trim(js.ToString(v)) == "" {
			continue
		}
		rules := ParseFilterRuleText(v)
		if rules.IncludeRules.HasRules {
			filtered = FilterNodeURLs(filtered, rules.IncludeRules, "include")
		}
		if rules.ExcludeRules.HasRules {
			filtered = FilterNodeURLs(filtered, rules.ExcludeRules, "exclude")
		}
	}
	return filtered
}
