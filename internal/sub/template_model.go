package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 template-model.js、mihomo-chain-proxy.js、template-compatibility.js

// CreateUnifiedTemplateModel 对应 createUnifiedTemplateModel：统一模板模型，各渲染器的共同输入。
// 注意与上游一样，proxies / groups / rules 数组按引用保留。
func CreateUnifiedTemplateModel(inputVal any) *js.Object {
	input := objOrEmpty(inputVal)
	meta := input.Get("meta")
	settings := input.Get("settings")
	arrOr := func(v any) any {
		if a, ok := v.(*js.Array); ok {
			return a
		}
		return js.Arr()
	}
	extras := input.Get("extras")
	if !js.IsObjectLike(extras) {
		extras = js.NewObject()
	}
	return js.Obj(
		"meta", js.Obj(
			"name", js.Or(objGet(meta, "name"), "MiSub"),
			"source", js.Or(objGet(meta, "source"), "builtin"),
			"target", js.Or(objGet(meta, "target"), "clash"),
			"ruleLevel", js.Or(objGet(meta, "ruleLevel"), "std"),
			"isMeta", js.Truthy(objGet(meta, "isMeta")),
		),
		"proxies", arrOr(input.Get("proxies")),
		"groups", arrOr(input.Get("groups")),
		"rules", arrOr(input.Get("rules")),
		"settings", js.Obj(
			"managedConfigUrl", js.Or(objGet(settings, "managedConfigUrl"), ""),
			"interval", js.Or(objGet(settings, "interval"), 86400.0),
			"skipCertVerify", js.Truthy(objGet(settings, "skipCertVerify")),
			"enableUdp", js.Truthy(objGet(settings, "enableUdp")),
			"customDnsOverride", js.Or(objGet(settings, "customDnsOverride"), ""),
			"dnsMode", js.Or(objGet(settings, "dnsMode"), "clean"),
		),
		"extras", extras,
	)
}

// NormalizeUnifiedTemplateModel 对应 normalizeUnifiedTemplateModel。
func NormalizeUnifiedTemplateModel(model any) *js.Object { return CreateUnifiedTemplateModel(model) }

func modelArray(model *js.Object, key string) *js.Array {
	a, _ := model.Get(key).(*js.Array)
	if a == nil {
		a = js.Arr()
		model.Set(key, a)
	}
	return a
}

func findGroup(groups *js.Array, name string) *js.Object {
	for _, g := range groups.Items {
		if js.StrictEquals(objGet(g, "name"), name) {
			return js.AsObject(g)
		}
	}
	return nil
}

func ensureGroup(model *js.Object, name string, members []any) *js.Object {
	groups := modelArray(model, "groups")
	if g := findGroup(groups, name); g != nil {
		if !js.IsArray(g.Get("members")) {
			g.Set("members", js.Arr())
		}
		return g
	}
	g := js.Obj("name", name, "type", "select", "members", js.Arr(append([]any(nil), members...)...), "options", js.NewObject())
	groups.Push(g)
	return g
}

var dynamicMemberOptionKeys = []string{"include-all", "include-all-proxies", "use", "filter", "exclude-filter"}

func removeDynamicMemberOptions(group *js.Object) {
	group.Set("filters", js.Arr())
	if o, ok := group.Get("options").(*js.Object); ok {
		for _, k := range dynamicMemberOptionKeys {
			o.Delete(k)
		}
	}
}

// ApplyMihomoChainProxy 对应 applyMihomoChainProxy：把 custom_chain_proxy 展开为带 dialer-proxy 的落地副本。
func ApplyMihomoChainProxy(model *js.Object, enableDialerProxy bool) *js.Object {
	chainConfigs, ok := objGet(model.Get("extras"), "chainProxy").(*js.Array)
	if !ok || chainConfigs.Len() == 0 {
		return model
	}
	proxiesArr, okP := model.Get("proxies").(*js.Array)
	groupsArr, okG := model.Get("groups").(*js.Array)
	if !okP || !okG {
		return model
	}
	baseProxies := js.Arr()
	for _, p := range proxiesArr.Items {
		if js.Truthy(p) {
			if o, isObj := p.(*js.Object); isObj {
				baseProxies.Push(o.Clone())
			} else {
				baseProxies.Push(js.NewObject())
			}
		}
	}
	var named []*js.Object
	var baseNames []any
	seenName := map[string]bool{}
	for _, p := range baseProxies.Items {
		o := p.(*js.Object)
		n := js.Or(o.Get("name"), o.Get("tag"))
		if !js.Truthy(n) {
			continue
		}
		named = append(named, o)
		if k := js.Describe(n); !seenName[k] {
			seenName[k] = true
			baseNames = append(baseNames, n)
		}
	}
	if len(baseNames) == 0 {
		return model
	}
	model.Set("proxies", baseProxies)
	newGroups := js.Arr()
	for _, g := range groupsArr.Items {
		if !js.Truthy(g) {
			continue
		}
		o := objOrEmpty(g).Clone()
		if a, isArr := o.Get("members").(*js.Array); isArr {
			o.Set("members", a.Copy())
		} else {
			o.Set("members", js.Arr())
		}
		if a, isArr := o.Get("filters").(*js.Array); isArr {
			o.Set("filters", a.Copy())
		} else {
			o.Set("filters", js.Arr())
		}
		if opts, isObj := o.Get("options").(*js.Object); isObj {
			o.Set("options", opts.Clone())
		} else {
			o.Set("options", js.NewObject())
		}
		newGroups.Push(o)
	}
	model.Set("groups", newGroups)
	usedNames := map[string]bool{}
	for _, n := range baseNames {
		usedNames[js.ToString(n)] = true
	}
	reserved := map[string]bool{}
	handled := map[string]bool{}
	landingNames := map[string]bool{}
	for _, c := range chainConfigs.Items {
		if n := js.Trim(strOr(objGet(c, "group"))); n != "" {
			landingNames[n] = true
		}
	}
	for _, c := range chainConfigs.Items {
		landingName := js.Trim(strOr(objGet(c, "group")))
		entryName := js.Trim(strOr(objGet(c, "entry")))
		if landingName == "" || entryName == "" || landingName == entryName || handled[landingName] || landingNames[entryName] {
			continue
		}
		handled[landingName] = true
		prefix := landingName + " - "
		if p, isStr := objGet(c, "prefix").(string); isStr && p != "" {
			prefix = p
		}
		entryGroup := ensureGroup(model, entryName, baseNames)
		landingGroup := ensureGroup(model, landingName, nil)
		if entryGroup.Get("members").(*js.Array).Len() == 0 {
			entryGroup.Set("members", js.Arr(append([]any(nil), baseNames...)...))
		}
		if !enableDialerProxy {
			if landingGroup.Get("members").(*js.Array).Len() == 0 {
				landingGroup.Set("members", js.Arr(append([]any(nil), baseNames...)...))
			}
			continue
		}
		copies := js.Arr()
		copyNames := js.Arr()
		for _, p := range named {
			baseName := js.ToString(js.Or(p.Get("name"), p.Get("tag")))
			name := prefix + baseName
			suffix := 2
			for usedNames[name] {
				name = prefix + baseName + " (" + js.NumberToString(float64(suffix)) + ")"
				suffix++
			}
			usedNames[name] = true
			reserved[name] = true
			cp := p.Clone()
			cp.Set("name", name)
			cp.Set("dialer-proxy", entryName)
			copies.Push(cp)
			copyNames.Push(name)
		}
		model.Get("proxies").(*js.Array).Push(copies.Items...)
		landingGroup.Set("type", "select")
		landingGroup.Set("members", copyNames)
		removeDynamicMemberOptions(landingGroup)
		kept := js.Arr()
		for _, m := range entryGroup.Get("members").(*js.Array).Items {
			if !js.StrictEquals(m, landingName) && !reserved[js.ToString(m)] {
				kept.Push(m)
			}
		}
		entryGroup.Set("members", kept)
		removeDynamicMemberOptions(entryGroup)
	}
	return model
}

// TemplateCompatibility 对应 TEMPLATE_COMPATIBILITY 的一项。
type TemplateCompatibility struct {
	AllowExternalTemplate bool
	ExternalTemplateTypes []string
	Strategy, Description string
}

var templateCompatibility = map[string]TemplateCompatibility{
	"clash":   {true, []string{"ini"}, "model-driven", "Clash 系列通过统一模板模型接入，当前内置引擎支持将 ini 模板转译为 Clash 配置。"},
	"surge":   {true, []string{"ini"}, "model-driven", "Surge 通过统一模板模型接入，支持将 ini 模板转译为 Surge 配置。"},
	"loon":    {true, []string{"ini"}, "model-driven", "Loon 通过统一模板模型接入，支持将 ini 模板转译为 Loon 配置。"},
	"quanx":   {true, []string{"ini"}, "model-driven", "Quantumult X 通过统一模板模型接入，支持将 ini 模板转译为 Quantumult X 配置。"},
	"singbox": {true, []string{"ini"}, "model-driven", "Sing-Box 通过统一模板模型接入，当前内置引擎支持将 ini 模板转译为 JSON 配置。"},
}

// Value 转换为 JS 对象。
func (c TemplateCompatibility) Value() *js.Object {
	return js.Obj("allowExternalTemplate", c.AllowExternalTemplate, "externalTemplateTypes", js.StrArr(c.ExternalTemplateTypes),
		"strategy", c.Strategy, "description", c.Description)
}

func lastSegment(s, sep string) string {
	parts := strings.Split(s, sep)
	return parts[len(parts)-1]
}

func getTemplateExtension(templateURL any) string {
	u, _ := templateURL.(string)
	u = js.Trim(u)
	if u == "" {
		return ""
	}
	if pu, err := js.ParseURL(u); err == nil {
		return js.ToLower(lastSegment(lastSegment(pu.Pathname(), "/"), "."))
	}
	clean := strings.Split(strings.Split(u, "#")[0], "?")[0]
	return js.ToLower(lastSegment(lastSegment(clean, "/"), "."))
}

// NormalizeTemplateTarget 对应 normalizeTemplateTarget。
func NormalizeTemplateTarget(targetFormat any) string {
	t, _ := targetFormat.(string)
	t = js.ToLower(t)
	if strings.HasPrefix(t, "surge&ver=") {
		return "surge"
	}
	if t == "sing-box" {
		return "singbox"
	}
	return t
}

// GetTemplateCompatibility 对应 getTemplateCompatibility；不支持返回 nil。
func GetTemplateCompatibility(targetFormat any) *TemplateCompatibility {
	if c, ok := templateCompatibility[NormalizeTemplateTarget(targetFormat)]; ok {
		return &c
	}
	return nil
}

// ShouldApplyExternalTemplateForTarget 对应 shouldApplyExternalTemplateForTarget。
func ShouldApplyExternalTemplateForTarget(targetFormat, templateURL any) bool {
	u, _ := templateURL.(string)
	if js.Trim(u) == "" {
		return false
	}
	rule := GetTemplateCompatibility(targetFormat)
	if rule == nil || !rule.AllowExternalTemplate {
		return false
	}
	ext := getTemplateExtension(js.Trim(u))
	for _, t := range rule.ExternalTemplateTypes {
		if t == ext {
			return true
		}
	}
	return false
}
