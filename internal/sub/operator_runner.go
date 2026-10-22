package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/utils/operator-runner.js：Sub-Store 风格的算子链（过滤、重命名、受限 DSL、排序、去重）。
// 上游的 opScript 已禁用任意脚本，只接受 params.dsl，这里同样不执行任何代码。

var reLineBreak = js.MustRegExp(`\r?\n`, "")

func normalizeRules(rules any) []any {
	switch x := rules.(type) {
	case nil:
		return nil
	case string:
		var out []any
		for _, l := range reLineBreak.Split(x, -1) {
			if l = js.Trim(l); l != "" {
				out = append(out, l)
			}
		}
		return out
	case *js.Array:
		var out []any
		for _, rule := range x.Items {
			if s, ok := rule.(string); ok {
				for _, p := range reLineBreak.Split(s, -1) {
					if p = js.Trim(p); p != "" {
						out = append(out, p)
					}
				}
			} else if js.Truthy(rule) && js.Truthy(objGet(rule, "pattern")) {
				out = append(out, rule)
			}
		}
		return out
	}
	return nil
}

func matchRecordRules(r *js.Object, rules []any) bool {
	enriched := EnsureRegionInfo(r, true)
	if MatchesRegexRules(r.Get("name"), rules) {
		return true
	}
	if clean := objGet(r.Get("metadata"), "cleanName"); js.Truthy(clean) && MatchesRegexRules(clean, rules) {
		return true
	}
	return MatchesRegexRules(enriched.Get("regionZh"), rules) || MatchesRegexRules(enriched.Get("region"), rules) ||
		MatchesRegexRules(enriched.Get("regionCode"), rules)
}

func filterObjects(list []*js.Object, pred func(*js.Object, int) bool) []*js.Object {
	out := []*js.Object{}
	for i, r := range list {
		if pred(r, i) {
			out = append(out, r)
		}
	}
	return out
}

func opFilter(nodes []*js.Object, paramsVal any) []*js.Object {
	if !js.Truthy(paramsVal) {
		return nodes
	}
	result := append([]*js.Object(nil), nodes...)
	include, exclude := objGet(paramsVal, "include"), objGet(paramsVal, "exclude")
	protocols, regions := objGet(paramsVal, "protocols"), objGet(paramsVal, "regions")
	if js.Truthy(objGet(include, "enabled")) {
		if rules := normalizeRules(objGet(include, "rules")); len(rules) > 0 {
			result = filterObjects(result, func(r *js.Object, _ int) bool { return matchRecordRules(r, rules) })
		}
	}
	if js.Truthy(objGet(exclude, "enabled")) {
		if rules := normalizeRules(objGet(exclude, "rules")); len(rules) > 0 {
			result = filterObjects(result, func(r *js.Object, _ int) bool { return !matchRecordRules(r, rules) })
		}
	}
	if values, ok := objGet(protocols, "values").(*js.Array); js.Truthy(objGet(protocols, "enabled")) && ok {
		allowed := map[string]bool{}
		for _, p := range values.Items {
			allowed[js.ToLower(js.ToString(p))] = true
		}
		result = filterObjects(result, func(r *js.Object, _ int) bool { return allowed[js.ToLower(js.ToString(r.Get("protocol")))] })
	}
	if values, ok := objGet(regions, "values").(*js.Array); js.Truthy(objGet(regions, "enabled")) && ok {
		for i, r := range result {
			result[i] = EnsureRegionInfo(r, true)
		}
		has := func(v any) bool {
			for _, x := range values.Items {
				if sameValueZero(x, v) {
					return true
				}
			}
			return false
		}
		result = filterObjects(result, func(r *js.Object, _ int) bool { return has(r.Get("regionZh")) || has(r.Get("region")) })
	}
	return result
}

func templateOffset(template any) float64 {
	n := js.ToNumber(js.Or(objGet(template, "offset"), objGet(template, "indexStart")))
	if !js.Truthy(n) {
		return 1
	}
	return n
}

func opRename(nodes []*js.Object, paramsVal any) []*js.Object {
	if !js.Truthy(paramsVal) {
		return nodes
	}
	result := append([]*js.Object(nil), nodes...)
	regex, template := objGet(paramsVal, "regex"), objGet(paramsVal, "template")
	if js.Truthy(objGet(regex, "enabled")) {
		if rules := normalizeRules(objGet(regex, "rules")); len(rules) > 0 {
			for i, r := range result {
				enriched := EnsureRegionInfo(r, true)
				vars := js.Obj("name", r.Get("name"), "protocol", r.Get("protocol"), "region", enriched.Get("region"),
					"regionZh", enriched.Get("regionZh"), "emoji", enriched.Get("emoji"), "server", r.Get("server"), "port", r.Get("port"))
				processed := js.Arr()
				for _, rule := range rules {
					ro, isObj := rule.(*js.Object)
					if isObj {
						if rep, ok := ro.Get("replacement").(string); ok && rep != "" && strings.Contains(rep, "{") {
							c := ro.Clone()
							c.Set("replacement", RenderTemplate(rep, vars, r))
							processed.Push(c)
							continue
						}
					}
					processed.Push(rule)
				}
				newName := ApplyRegexRename(r.Get("name"), processed, r)
				if !js.StrictEquals(newName, r.Get("name")) {
					n := r.Clone()
					n.Set("name", newName)
					n.Set("url", SetNodeName(r.Get("url"), r.Get("protocol"), newName))
					result[i] = n
				}
			}
		}
	}
	if js.Truthy(objGet(template, "enabled")) && js.Truthy(objGet(template, "template")) {
		counters := map[string]float64{}
		scope := js.ToString(js.Or(objGet(template, "indexScope"), objGet(template, "scope"), "region"))
		offset := templateOffset(template)
		for index, r := range result {
			enriched := EnsureRegionInfo(r, true)
			groupKey := "global"
			switch scope {
			case "region":
				groupKey = "r:" + js.ToString(js.Or(enriched.Get("regionZh"), "Other"))
			case "protocol":
				groupKey = "p:" + js.ToString(r.Get("protocol"))
			case "regionProtocol":
				groupKey = "rp:" + js.ToString(js.Or(enriched.Get("regionZh"), "Other")) + "|" + js.ToString(r.Get("protocol"))
			}
			groupIndex := counters[groupKey] + 1
			counters[groupKey] = groupIndex
			vars := js.Obj("name", r.Get("name"), "protocol", r.Get("protocol"), "region", enriched.Get("region"),
				"regionZh", enriched.Get("regionZh"), "emoji", enriched.Get("emoji"), "server", r.Get("server"), "port", r.Get("port"),
				"index", groupIndex+offset-1, "globalIndex", float64(index)+offset)
			newName := RenderTemplate(objGet(template, "template"), vars, r)
			if !js.StrictEquals(newName, r.Get("name")) {
				n := r.Clone()
				n.Set("name", newName)
				n.Set("url", SetNodeName(r.Get("url"), r.Get("protocol"), newName))
				result[index] = n
			}
		}
	}
	return result
}

func opScript(nodes []*js.Object, paramsVal any, target string) []*js.Object {
	params := paramsVal
	if params == nil {
		params = js.NewObject()
	}
	var dsl []any
	if a, ok := objGet(params, "dsl").(*js.Array); ok {
		dsl = a.Items
	} else if js.Truthy(objGet(params, "action")) {
		dsl = []any{params}
	}
	if len(dsl) == 0 {
		return nodes
	}
	result := make([]*js.Object, len(nodes))
	for i, r := range nodes {
		result[i] = EnsureRegionInfo(r, true)
	}
	for _, step := range dsl {
		action := js.ToLower(strOr(objGet(step, "action")))
		if action == "filter" {
			result = filterObjects(result, func(node *js.Object, index int) bool {
				rec := node.Clone()
				rec.Set("index", float64(index+1))
				return MatchesDslCondition(rec, step)
			})
			continue
		}
		if action == "rename" {
			template := js.Or(objGet(step, "template"), objGet(step, "expression"))
			if !js.Truthy(template) {
				continue
			}
			for index, node := range result {
				ctx := node.Clone()
				ctx.Set("index", float64(index+1))
				ctx.Set("target", target)
				var nextName any = RenderDslTemplate(template, ctx)
				if nextName == "" {
					nextName = node.Get("name")
				}
				if js.StrictEquals(nextName, node.Get("name")) {
					continue
				}
				n := node.Clone()
				n.Set("name", nextName)
				n.Set("url", SetNodeName(node.Get("url"), node.Get("protocol"), nextName))
				if meta := node.Get("metadata"); js.Truthy(meta) {
					m := objOrEmpty(meta).Clone()
					m.Set("cleanName", nextName)
					n.Set("metadata", m)
				} else {
					n.Set("metadata", meta)
				}
				result[index] = n
			}
		}
	}
	return result
}

// nameLength 对应 `(name || "").length`；非字符串名称的 length 为 undefined，比较结果恒为 false。
func nameLength(v any) (int, bool) {
	if !js.Truthy(v) {
		return 0, true
	}
	if s, ok := v.(string); ok {
		return js.UTF16Len(s), true
	}
	return 0, false
}

// OperatorContext 对应 runOperatorChain 的 context。
type OperatorContext struct {
	UserAgent         string
	Target            string
	NodeMetadataByURL any // *js.Object 或 *js.JSMap
}

func metadataLookup(src any) func(string) any {
	switch m := src.(type) {
	case *js.JSMap:
		return func(u string) any {
			for i, k := range m.Keys {
				if js.StrictEquals(k, u) {
					return m.Vals[i]
				}
			}
			return nil
		}
	case *js.Object:
		if m.Len() == 0 {
			return nil
		}
		return func(u string) any { return m.Get(u) }
	}
	return nil
}

// RunOperatorChain 对应 runOperatorChain。
func RunOperatorChain(nodeURLs []string, operators any, ctx OperatorContext) []any {
	ops, ok := operators.(*js.Array)
	if !ok || ops.Len() == 0 {
		out := make([]any, len(nodeURLs))
		for i, u := range nodeURLs {
			out[i] = u
		}
		return out
	}
	records := NodeURLsToRecords(nodeURLs, false, false)
	if lookup := metadataLookup(ctx.NodeMetadataByURL); lookup != nil {
		hasEntries := true
		if jm, isMap := ctx.NodeMetadataByURL.(*js.JSMap); isMap && len(jm.Keys) == 0 {
			hasEntries = false
		}
		if hasEntries {
			for i, r := range records {
				n := r.Clone()
				if extra, isObj := lookup(js.ToString(r.Get("url"))).(*js.Object); isObj {
					n.Assign(extra)
				}
				records[i] = n
			}
		}
	}
	// 上游把 context.target || 'base64' 合并进传给 opScript 的上下文
	target := ctx.Target
	if target == "" {
		target = "base64"
	}
	for _, op := range ops.Items {
		typ := objGet(op, "type")
		params := objGet(op, "params")
		if js.StrictEquals(objGet(op, "enabled"), false) {
			continue
		}
		switch js.ToString(typ) {
		case "filter":
			records = opFilter(records, params)
		case "rename":
			records = opRename(records, params)
		case "script":
			records = opScript(records, params, target)
		case "sort":
			keys, isArr := objGet(params, "keys").(*js.Array)
			if !js.Truthy(params) || !isArr {
				break
			}
			needsRegion := false
			for _, k := range keys.Items {
				if js.StrictEquals(objGet(k, "key"), "region") || js.StrictEquals(objGet(k, "key"), "regionZh") {
					needsRegion = true
					break
				}
			}
			if needsRegion {
				for i, r := range records {
					records[i] = EnsureRegionInfo(r, true)
				}
			}
			items := make([]any, len(records))
			for i, r := range records {
				items[i] = r
			}
			js.SortStable(items, MakeComparator(js.Obj("keys", keys)))
			for i, it := range items {
				records[i] = it.(*js.Object)
			}
		case "dedup":
			includeProtocol := !js.StrictEquals(objGet(params, "includeProtocol"), false)
			var order []string
			seen := map[string]*js.Object{}
			for _, r := range records {
				hostPort := js.ToString(r.Get("server")) + ":" + js.ToString(r.Get("port"))
				key := hostPort
				if includeProtocol {
					key = js.ToString(r.Get("protocol")) + "|" + hostPort
				}
				existing, exists := seen[key]
				if !exists {
					order = append(order, key)
					seen[key] = r
					continue
				}
				nl, okN := nameLength(r.Get("name"))
				el, okE := nameLength(existing.Get("name"))
				if okN && okE && nl > el {
					seen[key] = r
				}
			}
			records = records[:0:0]
			for _, k := range order {
				records = append(records, seen[k])
			}
		}
	}
	return RecordsToNodeURLs(records)
}
