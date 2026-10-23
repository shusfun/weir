package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/template-processor.js：模板模型的智能优化（正则成员展开、DNS / AI 分组注入、地区分组、清理）。

func proxyNamesFromModel(model *js.Object) []any {
	var out []any
	for _, p := range modelArray(model, "proxies").Items {
		if n := js.Or(objGet(p, "name"), objGet(p, "tag")); js.Truthy(n) {
			out = append(out, n)
		}
	}
	return out
}

// orderedSet 对应 JS Set（SameValueZero，保持插入顺序）。
type orderedSet struct {
	items []any
	keys  map[string]bool
}

func newOrderedSet(items ...any) *orderedSet {
	s := &orderedSet{keys: map[string]bool{}}
	for _, it := range items {
		s.add(it)
	}
	return s
}

func setKey(v any) string {
	if s, ok := v.(string); ok {
		return "s:" + s
	}
	return "v:" + js.Describe(v)
}

func (s *orderedSet) add(v any) {
	k := setKey(v)
	if !s.keys[k] {
		s.keys[k] = true
		s.items = append(s.items, v)
	}
}

func (s *orderedSet) has(v any) bool { return s.keys[setKey(v)] }

func (s *orderedSet) array() *js.Array { return js.Arr(append([]any(nil), s.items...)...) }

func groupObjects(model *js.Object) []*js.Object {
	var out []*js.Object
	for _, g := range modelArray(model, "groups").Items {
		out = append(out, objOrEmpty(g))
	}
	return out
}

func resolveGroupFilters(model *js.Object) {
	proxyNames := proxyNamesFromModel(model)
	if len(proxyNames) == 0 {
		return
	}
	for _, group := range groupObjects(model) {
		filters, ok := group.Get("filters").(*js.Array)
		if !ok || filters.Len() == 0 {
			continue
		}
		if !js.Truthy(group.Get("members")) {
			group.Set("members", js.Arr())
		}
		current := newOrderedSet(arrayOrEmpty(group.Get("members")).Items...)
		for _, f := range filters.Items {
			if js.StrictEquals(f, ".*") {
				for _, n := range proxyNames {
					current.add(n)
				}
				continue
			}
			re, err := js.NewRegExp(js.ToString(f), "i")
			if err != nil {
				continue
			}
			for _, n := range proxyNames {
				if re.Test(js.ToString(n)) {
					current.add(n)
				}
			}
		}
		group.Set("members", current.array())
	}
}

func pruneEmptyGroups(model *js.Object) {
	for {
		changed := false
		empty := newOrderedSet()
		for _, g := range groupObjects(model) {
			if m, ok := g.Get("members").(*js.Array); !ok || m.Len() == 0 {
				empty.add(g.Get("name"))
			}
		}
		if len(empty.items) == 0 {
			break
		}
		groups := modelArray(model, "groups")
		kept := js.Arr()
		for _, g := range groups.Items {
			if !empty.has(objGet(g, "name")) {
				kept.Push(g)
			}
		}
		if kept.Len() != groups.Len() {
			changed = true
		}
		model.Set("groups", kept)
		for _, g := range groupObjects(model) {
			if m, ok := g.Get("members").(*js.Array); ok {
				nm := js.Arr()
				for _, x := range m.Items {
					if !empty.has(x) {
						nm.Push(x)
					}
				}
				if nm.Len() != m.Len() {
					g.Set("members", nm)
					changed = true
				}
			}
		}
		rules := modelArray(model, "rules")
		keptRules := js.Arr()
		for _, r := range rules.Items {
			if !empty.has(objGet(r, "policy")) {
				keptRules.Push(r)
			}
		}
		if keptRules.Len() != rules.Len() {
			changed = true
		}
		model.Set("rules", keptRules)
		if !changed {
			break
		}
	}
}

var (
	reSemanticPrefix = js.MustRegExp(`^[^一-龥A-Za-z0-9]+`, "")
	reSemanticSep    = js.MustRegExp(`[\s_-]+`, "g")
	reSemanticNode   = js.MustRegExp(`节点`, "g")
)

func normalizeGroupSemanticName(name any) string {
	s := ""
	if name != nil {
		s = js.ToString(name)
	}
	s = reSemanticPrefix.Replace(s, "")
	s = reSemanticSep.Replace(s, "")
	s = reSemanticNode.Replace(s, "")
	return js.ToLower(s)
}

func hasEquivalentRegionGroup(model *js.Object, region *js.Object) bool {
	regionTags := newOrderedSet(arrayOrEmpty(js.Or(region.Get("tags"), js.Arr())).Items...)
	normalizedRegion := normalizeGroupSemanticName(region.Get("name"))
	for _, g := range groupObjects(model) {
		if normalizeGroupSemanticName(g.Get("name")) == normalizedRegion {
			return true
		}
		members := arrayOrEmpty(g.Get("members"))
		if members.Len() == 0 {
			continue
		}
		overlap := 0
		for _, m := range members.Items {
			if regionTags.has(m) {
				overlap++
			}
		}
		if overlap > 0 && overlap == members.Len() && overlap == len(regionTags.items) {
			return true
		}
	}
	return false
}

func truthyItems(a *js.Array) []any {
	var out []any
	for _, v := range a.Items {
		if js.Truthy(v) {
			out = append(out, v)
		}
	}
	return out
}

func dedupeGroupsByName(model *js.Object) {
	merged := js.Arr()
	seen := map[string]*js.Object{}
	for _, group := range groupObjects(model) {
		name := js.Trim(strOr(group.Get("name")))
		if name == "" {
			continue
		}
		existing, ok := seen[name]
		if !ok {
			n := group.Clone()
			n.Set("name", name)
			if m, isArr := group.Get("members").(*js.Array); isArr {
				n.Set("members", newOrderedSet(truthyItems(m)...).array())
			} else {
				n.Set("members", js.Arr())
			}
			if f, isArr := group.Get("filters").(*js.Array); isArr {
				n.Set("filters", newOrderedSet(truthyItems(f)...).array())
			} else {
				n.Set("filters", js.Arr())
			}
			if o, isObj := group.Get("options").(*js.Object); isObj {
				n.Set("options", o.Clone())
			} else {
				n.Set("options", js.NewObject())
			}
			seen[name] = n
			merged.Push(n)
			continue
		}
		mem := newOrderedSet(arrayOrEmpty(js.Or(existing.Get("members"), js.Arr())).Items...)
		for _, v := range truthyItems(arrayOrEmpty(js.Or(group.Get("members"), js.Arr()))) {
			mem.add(v)
		}
		existing.Set("members", mem.array())
		fil := newOrderedSet(arrayOrEmpty(js.Or(existing.Get("filters"), js.Arr())).Items...)
		for _, v := range truthyItems(arrayOrEmpty(js.Or(group.Get("filters"), js.Arr()))) {
			fil.add(v)
		}
		existing.Set("filters", fil.array())
		opts := objOrEmpty(js.Or(existing.Get("options"), js.NewObject())).Clone()
		if o, isObj := group.Get("options").(*js.Object); isObj {
			opts.Assign(o)
		}
		existing.Set("options", opts)
		if (!js.Truthy(existing.Get("type")) || js.StrictEquals(existing.Get("type"), "select")) && js.Truthy(group.Get("type")) {
			existing.Set("type", group.Get("type"))
		}
	}
	model.Set("groups", merged)
}

func expandMagicPlaceholders(model *js.Object) {
	regionNames := newOrderedSet()
	for _, g := range groupObjects(model) {
		if js.StrictEquals(g.Get("type"), "url-test") && !strings.Contains(js.ToString(g.Get("name")), "自动") {
			regionNames.add(g.Get("name"))
		}
	}
	protocolNames := newOrderedSet()
	for _, g := range groupObjects(model) {
		if strings.Contains(js.ToString(g.Get("name")), "节点") && !regionNames.has(g.Get("name")) {
			protocolNames.add(g.Get("name"))
		}
	}
	for _, g := range groupObjects(model) {
		members, ok := g.Get("members").(*js.Array)
		if !ok {
			continue
		}
		nm := js.Arr()
		for _, m := range members.Items {
			switch {
			case js.StrictEquals(m, "<%regionStrategyChain%>"):
				nm.Push(regionNames.items...)
			case js.StrictEquals(m, "<%protocolStrategyChain%>"):
				nm.Push(protocolNames.items...)
			default:
				nm.Push(m)
			}
		}
		g.Set("members", nm)
	}
}

func pruneInvalidMembers(model *js.Object) {
	valid := newOrderedSet()
	for _, p := range modelArray(model, "proxies").Items {
		valid.add(js.Or(objGet(p, "name"), objGet(p, "tag")))
	}
	for _, g := range groupObjects(model) {
		valid.add(g.Get("name"))
	}
	valid.add("DIRECT")
	valid.add("REJECT")
	for _, g := range groupObjects(model) {
		if members, ok := g.Get("members").(*js.Array); ok {
			nm := js.Arr()
			for _, m := range members.Items {
				if valid.has(m) {
					nm.Push(m)
				}
			}
			g.Set("members", nm)
		}
	}
}

func ensureDNSProxyGroup(model *js.Object) {
	groups := modelArray(model, "groups")
	if findGroup(groups, DNSProxyGroup) != nil {
		return
	}
	names := proxyNamesFromModel(model)
	members := js.Arr(names...)
	if len(names) == 0 {
		members = strs2("REJECT")
	}
	groups.Push(js.Obj("name", DNSProxyGroup, "type", "url-test", "members", members, "filters", js.Arr(), "options", urlTestOptions()))
}

var (
	reAIGroupName = js.MustRegExp(`人工智能|智能\s*ai|(?:^|[^a-z])(ai|claude|openai|gemini|grok|mistral|deepseek|perplexity|copilot)(?=$|[^a-z])`, "i")
	reMainGroupAI = js.MustRegExp(`选择|proxy|default|global|main`, "i")
	reMainGroup   = js.MustRegExp(`选择|Proxy|Default|Global|Main|select`, "i")
)

func isAIGroupName(name any) bool { return reAIGroupName.Test(strOr(name)) }

func proxyOnlyMembers(members any) []any {
	set := newOrderedSet()
	for _, m := range arrayOrEmpty(members).Items {
		switch js.ToUpper(js.ToString(m)) {
		case "DIRECT", "REJECT-DROP", "PASS":
			continue
		}
		set.add(m)
	}
	return set.items
}

func ensureAIPolicy(model *js.Object) {
	groups := groupObjects(model)
	var aiGroups []*js.Object
	var mainGroup *js.Object
	for _, g := range groups {
		if isAIGroupName(g.Get("name")) {
			aiGroups = append(aiGroups, g)
		} else if mainGroup == nil && reMainGroupAI.Test(js.ToString(g.Get("name"))) {
			mainGroup = g
		}
	}
	var fallbackMembers, preferredMembers []any
	if mainGroup != nil {
		fallbackMembers = proxyOnlyMembers(mainGroup.Get("members"))
	}
	if len(aiGroups) > 0 {
		preferredMembers = proxyOnlyMembers(aiGroups[0].Get("members"))
	}
	nodeMembers := proxyNamesFromModel(model)
	set := newOrderedSet()
	if len(preferredMembers) > 0 {
		for _, m := range preferredMembers {
			set.add(m)
		}
	} else {
		for _, m := range fallbackMembers {
			set.add(m)
		}
	}
	if len(preferredMembers) == 0 && len(fallbackMembers) == 0 {
		for _, m := range nodeMembers {
			set.add(m)
		}
	}
	candidates := set.array()
	if candidates.Len() == 0 {
		candidates = strs2("REJECT")
	}
	for _, g := range aiGroups {
		ms := js.Arr(proxyOnlyMembers(g.Get("members"))...)
		if ms.Len() == 0 {
			ms = strs2("REJECT")
		}
		g.Set("members", ms)
	}
	groupsArr := modelArray(model, "groups")
	existing := newOrderedSet()
	for _, g := range groupObjects(model) {
		existing.add(g.Get("name"))
	}
	if !existing.has("🤖 AI 自动") {
		groupsArr.Push(js.Obj("name", "🤖 AI 自动", "type", "url-test", "members", candidates, "filters", js.Arr(), "options", urlTestOptions()))
		existing.add("🤖 AI 自动")
	}
	if !existing.has("🤖 AI 故障转移") {
		groupsArr.Push(js.Obj("name", "🤖 AI 故障转移", "type", "fallback", "members", candidates, "filters", js.Arr(), "options", urlTestOptions()))
		existing.add("🤖 AI 故障转移")
	}
	if !existing.has("🤖 智能 AI") {
		groupsArr.Push(js.Obj("name", "🤖 智能 AI", "type", "select", "members", strs2("🤖 AI 自动", "🤖 AI 故障转移")))
		existing.add("🤖 智能 AI")
	}
	for _, s := range AIServiceRules {
		name := "🤖 " + s.Name
		if !existing.has(name) {
			groupsArr.Push(js.Obj("name", name, "type", "select", "members", strs2("🤖 AI 自动", "🤖 AI 故障转移"), "filters", js.Arr(), "options", js.NewObject()))
			existing.add(name)
		}
	}
	rules := modelArray(model, "rules")
	existingRules := map[string]bool{}
	for _, r := range rules.Items {
		existingRules[js.ToString(objGet(r, "type"))+"|"+js.ToString(objGet(r, "value"))+"|"+js.ToString(objGet(r, "policy"))] = true
	}
	merged := rules.Copy()
	for _, s := range AIServiceRules {
		for _, d := range s.Domains {
			key := "domain-suffix|" + d + "|🤖 " + s.Name
			if !existingRules[key] {
				merged.Push(js.Obj("type", "domain-suffix", "value", d, "policy", "🤖 "+s.Name, "source", "inline", "extras", js.Arr()))
			}
		}
	}
	model.Set("rules", merged)
}

// ApplySmartModelOptimizations 对应 applySmartModelOptimizations（模板模型优化主入口）。
func ApplySmartModelOptimizations(model *js.Object) *js.Object {
	normalizedLevel := js.ToLower(strOr(objGet(model.Get("meta"), "ruleLevel")))
	isCustomOrNone := normalizedLevel == "" || normalizedLevel == "none"
	resolveGroupFilters(model)
	custom := objGet(model.Get("settings"), "customDnsOverride")
	hasCustomDNS := js.Truthy(custom) && js.Trim(js.ToString(custom)) != ""
	if !hasCustomDNS {
		ensureDNSProxyGroup(model)
	}
	if !isCustomOrNone {
		ensureAIPolicy(model)
	}
	if !isCustomOrNone && normalizedLevel != "base" {
		names := proxyNamesFromModel(model)
		if len(names) > 0 {
			entries := make([]*js.Object, len(names))
			for i, n := range names {
				entries[i] = js.Obj("tag", n)
			}
			for _, region := range GroupNodeLinesByRegion(entries, nil) {
				if hasEquivalentRegionGroup(model, region) {
					continue
				}
				modelArray(model, "groups").Push(js.Obj("name", region.Get("name"), "type", "url-test", "members", region.Get("tags"),
					"options", js.Obj("url", gstaticURL, "interval", "300", "tolerance", "50")))
			}
		}
	}
	expandMagicPlaceholders(model)
	if normalizedLevel != "none" && normalizedLevel != "base" && normalizedLevel != "" {
		var target *js.Object
		for _, g := range groupObjects(model) {
			if reMainGroup.Test(js.ToString(g.Get("name"))) {
				target = g
				break
			}
		}
		if target != nil {
			if members, ok := target.Get("members").(*js.Array); ok && members.Len() == 0 {
				for _, g := range groupObjects(model) {
					if !js.StrictEquals(g.Get("name"), target.Get("name")) &&
						(js.StrictEquals(g.Get("type"), "url-test") || js.StrictEquals(g.Get("type"), "fallback")) {
						members.Push(g.Get("name"))
					}
				}
			}
		}
	}
	dedupeGroupsByName(model)
	pruneInvalidMembers(model)
	pruneEmptyGroups(model)
	return model
}
