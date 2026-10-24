package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/region-groups.js、protocol-groups.js、name-utils.js

type regionDisplay struct{ flag, name string }

var regionDisplayConfig = map[string]regionDisplay{
	"香港": {"🇭🇰", "香港节点"}, "台湾": {"🇹🇼", "台湾节点"}, "日本": {"🇯🇵", "日本节点"}, "新加坡": {"🇸🇬", "狮城节点"},
	"美国": {"🇺🇸", "美国节点"}, "韩国": {"🇰🇷", "韩国节点"}, "英国": {"🇬🇧", "英国节点"}, "德国": {"🇩🇪", "德国节点"},
	"法国": {"🇫🇷", "法国节点"}, "加拿大": {"🇨🇦", "加拿大节点"}, "澳大利亚": {"🇦🇺", "澳洲节点"}, "巴西": {"🇧🇷", "巴西节点"},
	"西班牙": {"🇪🇸", "西班牙节点"}, "荷兰": {"🇳🇱", "荷兰节点"}, "俄罗斯": {"🇷🇺", "俄国节点"}, "印度": {"🇮🇳", "印度节点"},
	"土耳其": {"🇹🇷", "土耳其节点"},
}

// RegionGroupPattern 是 REGION_GROUP_PATTERNS 的一项。
type RegionGroupPattern struct {
	Name    string
	Pattern *js.RegExp
}

// RegionGroupPatterns 对应 REGION_GROUP_PATTERNS。
var RegionGroupPatterns = []RegionGroupPattern{
	{"🇭🇰 香港节点", js.MustRegExp(`港|HK|Hong Kong`, "i")},
	{"🇹🇼 台湾节点", js.MustRegExp(`台|TW|Taiwan`, "i")},
	{"🇯🇵 日本节点", js.MustRegExp(`日|JP|Japan`, "i")},
	{"🇸🇬 狮城节点", js.MustRegExp(`狮城|新|SG|Singapore`, "i")},
	{"🇺🇸 美国节点", js.MustRegExp(`美|US|America`, "i")},
	{"🇰🇷 韩国节点", js.MustRegExp(`韩|KR|Korea`, "i")},
	{"🇬🇧 英国节点", js.MustRegExp(`英|UK|England`, "i")},
	{"🇩🇪 德国节点", js.MustRegExp(`德|DE|Germany`, "i")},
	{"🇫🇷 法国节点", js.MustRegExp(`法|FR|France`, "i")},
	{"🇧🇷 巴西节点", js.MustRegExp(`巴西|BR|Brazil`, "i")},
	{"🇪🇸 西班牙节点", js.MustRegExp(`西班牙|ES|Spain`, "i")},
	{"🇳🇱 荷兰节点", js.MustRegExp(`荷兰|NL|Netherlands`, "i")},
	{"🇦🇺 澳洲节点", js.MustRegExp(`澳|AU|Australia`, "i")},
}

var reHanRun = js.MustRegExp(`[一-龥]+`, "")

type regionOverride struct {
	pattern *js.RegExp
	region  string
}

func normalizeRegionOverrides(overrides any) []regionOverride {
	arr, ok := overrides.(*js.Array)
	if !ok {
		return nil
	}
	var out []regionOverride
	for _, rule := range arr.Items {
		if !js.Truthy(rule) || !js.Truthy(objGet(rule, "pattern")) || !js.Truthy(objGet(rule, "region")) {
			continue
		}
		re, err := js.NewRegExp(js.ToString(objGet(rule, "pattern")), js.ToString(js.Or(objGet(rule, "flags"), "i")))
		if err != nil {
			continue
		}
		out = append(out, regionOverride{re, js.Trim(js.ToString(objGet(rule, "region")))})
	}
	return out
}

// GroupNodeLinesByRegion 对应 groupNodeLinesByRegion：按地区聚合节点（tag 或 name），保持首次出现顺序。
func GroupNodeLinesByRegion(nodes []*js.Object, options *js.Object) []*js.Object {
	overrides := normalizeRegionOverrides(options.Get("regionOverrides"))
	var order []string
	groups := map[string][]*js.Object{}
	for _, node := range nodes {
		tagName := js.Or(node.Get("tag"), node.Get("name"))
		tagStr := js.ToString(tagName)
		region := "其他"
		var override *regionOverride
		for i := range overrides {
			if overrides[i].pattern.Test(tagStr) {
				override = &overrides[i]
				break
			}
		}
		meta := node.Get("metadata")
		if override != nil {
			region = override.region
		} else if js.Truthy(meta) && js.Truthy(objGet(meta, "region")) && !js.StrictEquals(objGet(meta, "region"), "其他") {
			region = js.ToString(objGet(meta, "region"))
		} else {
			for _, p := range RegionGroupPatterns {
				if p.Pattern.Test(tagStr) {
					region = "其他"
					if m := reHanRun.MatchFirst(p.Name); m != nil {
						region = strings.Replace(m.Str(0), "节点", "", 1)
					}
					break
				}
			}
		}
		cfg, ok := regionDisplayConfig[region]
		if !ok {
			cfg = regionDisplay{"🌍", region + "节点"}
		}
		groupName := cfg.flag + " " + cfg.name
		if _, exists := groups[groupName]; !exists {
			order = append(order, groupName)
		}
		groups[groupName] = append(groups[groupName], node)
	}
	out := []*js.Object{}
	for _, name := range order {
		matched := groups[name]
		tags := js.Arr()
		nodesArr := js.Arr()
		for _, n := range matched {
			tags.Push(js.Or(n.Get("tag"), n.Get("name")))
			nodesArr.Push(n)
		}
		out = append(out, js.Obj("name", name, "tags", tags, "nodes", nodesArr, "count", float64(len(matched)),
			"selectorTag", name+" 选择", "urltestTag", name+" 测速", "fallbackTag", name+" 兜底"))
	}
	return out
}

// ProtocolGroupPatterns 对应 PROTOCOL_GROUP_PATTERNS。
var ProtocolGroupPatterns = []RegionGroupPattern{
	{"VLESS 节点", js.MustRegExp(`\bvless\b`, "i")},
	{"Trojan 节点", js.MustRegExp(`\btrojan\b`, "i")},
	{"VMess 节点", js.MustRegExp(`\bvmess\b`, "i")},
	{"Hysteria2 节点", js.MustRegExp(`\b(?:hysteria2|hy2)\b`, "i")},
	{"Shadowsocks 节点", js.MustRegExp(`\b(?:ss|shadowsocks)\b`, "i")},
	{"TUIC 节点", js.MustRegExp(`\btuic\b`, "i")},
	{"AnyTLS 节点", js.MustRegExp(`\banytls\b`, "i")},
	{"WireGuard 节点", js.MustRegExp(`\bwireguard\b`, "i")},
}

// GroupNodeLinesByProtocol 对应 groupNodeLinesByProtocol；输入为 {line} 对象数组。
func GroupNodeLinesByProtocol(nodeLines []*js.Object) []*js.Object {
	out := []*js.Object{}
	for _, p := range ProtocolGroupPatterns {
		lines := js.Arr()
		for _, n := range nodeLines {
			if p.Pattern.Test(js.ToString(n.Get("line"))) {
				lines.Push(n.Get("line"))
			}
		}
		if lines.Len() > 0 {
			out = append(out, js.Obj("name", p.Name, "lines", lines, "count", float64(lines.Len())))
		}
	}
	return out
}

// UniqueNamer 对应 getUniqueName 使用的 usedNames Map。
type UniqueNamer struct{ used map[string]int }

// NewUniqueNamer 创建命名计数器。
func NewUniqueNamer() *UniqueNamer { return &UniqueNamer{used: map[string]int{}} }

// GetUniqueName 对应 getUniqueName：重复名称追加 _1、_2 后缀。
func (u *UniqueNamer) GetUniqueName(baseName string) string {
	count := u.used[baseName] + 1
	u.used[baseName] = count
	if count == 1 {
		return baseName
	}
	return baseName + "_" + js.NumberToString(float64(count-1))
}

// GetUniqueNameValue 与 GetUniqueName 相同，但接受任意值（名称不是字符串时按值计数，首个保持原值）。
func (u *UniqueNamer) GetUniqueNameValue(baseName any) any {
	if s, ok := baseName.(string); ok {
		return u.GetUniqueName(s)
	}
	key := "\x00" + js.Describe(baseName)
	count := u.used[key] + 1
	u.used[key] = count
	if count == 1 {
		return baseName
	}
	return js.ToString(baseName) + "_" + js.NumberToString(float64(count-1))
}
