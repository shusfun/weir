package sub

import (
	"math"
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/utils/node-transformer.js：正则过滤、正则重命名、模板重命名、去重与排序。

func defaultSortKeys() *js.Array {
	return js.Arr(
		js.Obj("key", "region", "order", "asc", "customOrder", strs2("香港", "台湾", "日本", "新加坡", "美国", "韩国", "英国", "德国", "法国", "加拿大")),
		js.Obj("key", "protocol", "order", "asc", "customOrder", strs2("vless", "trojan", "vmess", "hysteria2", "ss", "ssr", "anytls")),
		js.Obj("key", "name", "order", "asc"),
	)
}

func strs2(items ...string) *js.Array { return js.StrArr(items) }

const virtualInfoNodeUUID = "00000000-0000-0000-0000-000000000000"

var (
	regionCodeToZh = buildRegionCodeToZhMap()
	regionZhToCode = buildZhToCodeMap()
	reRegionCode   = js.MustRegExp(`^[A-Za-z]{2,3}$`, "")
	reLeadingFlag  = js.MustRegExp(`^[\u{1F1E6}-\u{1F1FF}][\u{1F1E6}-\u{1F1FF}]\s*`, "gu")
)

func normalizeProtocol(proto any) string {
	p := js.ToLower(js.ToString(js.Or(proto, "unknown")))
	if p == "hy" || p == "hy2" || p == "hysteria" {
		return "hysteria2"
	}
	return p
}

// stripLeadingEmoji 去掉开头的一面旗帜（上游用 UTF-16 代理对正则 /^[\uD83C][\uDDE6-\uDDFF]{2}/ 表达同一含义）。
func stripLeadingEmoji(name any) string {
	return js.Trim(reLeadingFlag.Replace(strOr(name), ""))
}

func objOrEmpty(v any) *js.Object {
	if o, ok := v.(*js.Object); ok && o != nil {
		return o
	}
	return js.NewObject()
}

// truthyObj 对应 `v || {}`：v 为真值但不是对象时，按 JS 规则其属性读取均为 undefined。
func truthyObj(v any) *js.Object {
	if js.Truthy(v) {
		if o, ok := v.(*js.Object); ok {
			return o
		}
	}
	return js.NewObject()
}

func arrayOrEmpty(v any) *js.Array {
	if a, ok := v.(*js.Array); ok && a != nil {
		return a
	}
	return js.Arr()
}

func isFiniteNumber(v any) bool { return js.IsFinite(v) }

// transformConfig 是 normalizeConfig 的结果。
type transformConfig struct {
	Enabled, EnableEmoji bool
	IncludeEnabled       bool
	IncludeRules         *js.Array
	ExcludeEnabled       bool
	ExcludeRules         *js.Array
	ProtocolsEnabled     bool
	ProtocolValues       []string
	RegionsEnabled       bool
	RegionValues         []string
	FilterScriptEnabled  bool
	FilterScriptExpr     string
	UselessEnabled       bool
	RegexEnabled         bool
	RegexRules           *js.Array
	RenameScriptEnabled  bool
	RenameScriptExpr     string
	TemplateEnabled      bool
	Template             string
	IndexStart           any
	IndexPad             any
	IndexScope           any
	RegionAlias          any
	ProtocolAlias        any
	DedupEnabled         bool
	DedupMode            string
	DedupIncludeProtocol bool
	ProtocolOrder        []string
	SortEnabled          bool
	NameIgnoreEmoji      bool
	SortKeys             *js.Array
}

func normalizeTransformConfig(cfgVal any) *transformConfig {
	config := objOrEmpty(cfgVal)
	filter := truthyObj(config.Get("filter"))
	rename := truthyObj(config.Get("rename"))
	regex := truthyObj(rename.Get("regex"))
	template := truthyObj(rename.Get("template"))
	dedup := truthyObj(config.Get("dedup"))
	sortCfg := truthyObj(config.Get("sort"))
	fi := func(k string) any { return filter.Get(k) }
	c := &transformConfig{
		Enabled:              js.Truthy(config.Get("enabled")),
		EnableEmoji:          !js.StrictEquals(config.Get("enableEmoji"), false),
		IncludeEnabled:       js.Truthy(objGet(fi("include"), "enabled")),
		IncludeRules:         arrayOrEmpty(objGet(fi("include"), "rules")),
		ExcludeEnabled:       js.Truthy(objGet(fi("exclude"), "enabled")),
		ExcludeRules:         arrayOrEmpty(objGet(fi("exclude"), "rules")),
		ProtocolsEnabled:     js.Truthy(objGet(fi("protocols"), "enabled")),
		RegionsEnabled:       js.Truthy(objGet(fi("regions"), "enabled")),
		FilterScriptEnabled:  js.Truthy(objGet(fi("script"), "enabled")),
		FilterScriptExpr:     js.Trim(strOr(objGet(fi("script"), "expression"))),
		UselessEnabled:       js.Truthy(objGet(fi("useless"), "enabled")),
		RegexEnabled:         js.Truthy(regex.Get("enabled")),
		RegexRules:           arrayOrEmpty(regex.Get("rules")),
		RenameScriptEnabled:  js.Truthy(objGet(rename.Get("script"), "enabled")),
		RenameScriptExpr:     js.Trim(strOr(objGet(rename.Get("script"), "expression"))),
		TemplateEnabled:      js.Truthy(template.Get("enabled")),
		Template:             js.ToString(js.Or(template.Get("template"), "{emoji}{region}-{protocol}-{index}")),
		IndexScope:           js.Or(template.Get("indexScope"), "regionProtocol"),
		RegionAlias:          js.Or(template.Get("regionAlias"), js.NewObject()),
		ProtocolAlias:        js.Or(template.Get("protocolAlias"), js.NewObject()),
		DedupEnabled:         js.Truthy(dedup.Get("enabled")),
		DedupMode:            "serverPort",
		DedupIncludeProtocol: js.Truthy(dedup.Get("includeProtocol")),
		SortEnabled:          js.Truthy(sortCfg.Get("enabled")),
		NameIgnoreEmoji:      !js.StrictEquals(sortCfg.Get("nameIgnoreEmoji"), false),
	}
	if a, ok := objGet(fi("protocols"), "values").(*js.Array); ok {
		for _, v := range a.Items {
			if p := normalizeProtocol(v); p != "" {
				c.ProtocolValues = append(c.ProtocolValues, p)
			}
		}
	}
	if a, ok := objGet(fi("regions"), "values").(*js.Array); ok {
		for _, v := range a.Items {
			if r := toRegionZh(v); r != "" {
				c.RegionValues = append(c.RegionValues, r)
			}
		}
	}
	if v := template.Get("indexStart"); isFiniteNumber(v) {
		c.IndexStart = v
	} else {
		c.IndexStart = 1.0
	}
	if v := template.Get("indexPad"); isFiniteNumber(v) {
		c.IndexPad = v
	} else {
		c.IndexPad = 2.0
	}
	if js.StrictEquals(dedup.Get("mode"), "url") {
		c.DedupMode = "url"
	}
	if a, ok := objGet(dedup.Get("prefer"), "protocolOrder").(*js.Array); ok {
		for _, v := range a.Items {
			c.ProtocolOrder = append(c.ProtocolOrder, js.ToLower(js.ToString(v)))
		}
	}
	if a, ok := sortCfg.Get("keys").(*js.Array); ok && a.Len() > 0 {
		c.SortKeys = a
	} else {
		c.SortKeys = defaultSortKeys()
	}
	return c
}

// ruleParts 解析规则：字符串或 {pattern, flags, replacement}。
func ruleParts(rule any, defaultFlags string) (pattern, flags, replacement string, ok bool) {
	if s, isStr := rule.(string); isStr {
		return s, defaultFlags, "", s != ""
	}
	pattern = strOr(objGet(rule, "pattern"))
	if p := objGet(rule, "pattern"); js.Truthy(p) {
		pattern = js.ToString(p)
	}
	flags = js.ToString(js.Or(objGet(rule, "flags"), defaultFlags))
	replacement = js.ToString(js.Or(objGet(rule, "replacement"), ""))
	return pattern, flags, replacement, js.Truthy(objGet(rule, "pattern"))
}

// MatchesRegexRules 对应 matchesRegexRules。
func MatchesRegexRules(name any, rules []any) bool {
	value := strOr(name)
	for _, rule := range rules {
		if !js.Truthy(rule) {
			continue
		}
		pattern, flags, _, ok := ruleParts(rule, "i")
		if !ok {
			continue
		}
		re, err := js.NewRegExp(pattern, flags)
		if err != nil {
			continue
		}
		if re.Test(value) {
			return true
		}
	}
	return false
}

// EnsureRegionInfo 对应 ensureRegionInfo：缺少 regionZh 时补全 region / regionZh / emoji。
func EnsureRegionInfo(record *js.Object, enableEmoji bool) *js.Object {
	if js.Truthy(record.Get("regionZh")) {
		return record
	}
	meta := record.Get("metadata")
	regionZh := js.Or(objGet(meta, "regionZh"), ExtractNodeRegion(record.Get("name")))
	regionCode := js.Or(objGet(meta, "region"), "")
	if js.StrictEquals(regionZh, "其他") && js.Truthy(record.Get("server")) {
		regionZh = ExtractNodeRegion(record.Get("server"))
	}
	if !js.Truthy(regionCode) {
		regionCode = toRegionCode(regionZh)
	}
	var emoji any = ""
	if enableEmoji {
		emoji = js.Or(objGet(meta, "flag"), GetRegionEmoji(regionZh))
	}
	out := record.Clone()
	out.Set("region", regionCode)
	out.Set("regionZh", regionZh)
	out.Set("emoji", emoji)
	return out
}

var (
	reVirtualName  = js.MustRegExp(`(?:流量剩余|到期时间|您的订阅已到期)`, "")
	reUselessLocal = js.MustRegExp(`(?:流量剩余|剩余流量|订阅已失效|订阅已到期|已过期|到期提醒|到期时间|套餐到期|过期时间)`, "i")
	reUselessName  = js.MustRegExp(`(?:流量剩余|剩余流量|已用流量|总流量|套餐到期|到期时间|过期时间|订阅已失效|订阅已到期|已过期|官网|群组|频道|联系客服|测试节点|回车更新|点击订阅|剩余套餐|订阅信息)`, "i")
)

func isUselessNode(record *js.Object) bool {
	name := js.Trim(strOr(record.Get("name")))
	protocol := normalizeProtocol(record.Get("protocol"))
	server := js.ToLower(js.Trim(strOr(record.Get("server"))))
	isVirtual := protocol == "trojan" && server == "127.0.0.1" && js.ToNumber(record.Get("port")) == 443 &&
		strings.Contains(strOr(record.Get("url")), "trojan://"+virtualInfoNodeUUID+"@127.0.0.1:443#") &&
		reVirtualName.Test(name)
	if isVirtual {
		return false
	}
	if name == "" {
		return true
	}
	if protocol == "trojan" && server == "127.0.0.1" && reUselessLocal.Test(name) {
		return true
	}
	return reUselessName.Test(name)
}

// SetNodeName 对应 setNodeName：把新名称写回节点链接（VMess 写入 JSON 的 ps）。
func SetNodeName(u any, protocol any, newName any) any {
	if !js.Truthy(u) || !js.Truthy(newName) {
		return u
	}
	s, ok := u.(string)
	if !ok {
		// 上游对非字符串调用字符串方法会抛出异常，被捕获后返回原值
		return u
	}
	proto := js.ToLower(strOr(protocol))
	if proto == "vmess" {
		base64Part := strings.Replace(s, "vmess://", "", 1)
		b, err := js.AtobBytes(padBase64(urlSafeToStd(base64Part)))
		if err != nil {
			return u
		}
		cfgVal, err := js.JSONParse(js.TextDecode(b))
		if err != nil {
			return u
		}
		switch cfg := cfgVal.(type) {
		case *js.Object:
			cfg.Set("ps", newName)
		case *js.Array:
		default:
			return u
		}
		return "vmess://" + base64EncodeUtf8(js.MustStringify(cfgVal))
	}
	hashIndex := strings.LastIndex(s, "#")
	base := s
	if hashIndex != -1 {
		base = s[:hashIndex]
	}
	return base + "#" + enc(newName)
}

// base64EncodeUtf8 对应 modules/utils.js 的 base64EncodeUtf8。
func base64EncodeUtf8(s string) string {
	if s == "" {
		return ""
	}
	return js.Base64EncodeUTF8(s)
}

// ApplyRegexRename 对应 applyRegexRename；record 非 nil 时会写入 regexGroups（与上游一样就地修改）。
func ApplyRegexRename(name any, rulesVal any, record *js.Object) string {
	result := strOr(name)
	rules, ok := rulesVal.(*js.Array)
	if !ok {
		return result
	}
	var regexGroups *js.Object
	if record != nil {
		regexGroups, _ = record.Get("regexGroups").(*js.Object)
		if !js.Truthy(record.Get("regexGroups")) {
			regexGroups = js.NewObject()
		}
	}
	if regexGroups == nil {
		regexGroups = js.NewObject()
	}
	for _, rule := range rules.Items {
		if !js.Truthy(rule) {
			continue
		}
		pattern, flags, replacement, ok := ruleParts(rule, "gi")
		if !ok {
			continue
		}
		re, err := js.NewRegExp(pattern, flags)
		if err != nil {
			continue
		}
		firstRe, err := js.NewRegExp(pattern, strings.ReplaceAll(flags, "g", ""))
		if err != nil {
			continue
		}
		if m := firstRe.MatchFirst(result); m != nil {
			for i := 1; i < len(m.Groups); i++ {
				regexGroups.Set(js.NumberToString(float64(i)), js.Or(m.Group(i), ""))
			}
			if m.Names != nil {
				for _, gName := range sortedGroupNames(m.Names) {
					regexGroups.Set(gName, js.Or(m.Group(m.Names[gName]), ""))
				}
			}
		}
		result = re.Replace(result, replacement)
	}
	if record != nil {
		record.Set("regexGroups", regexGroups)
	}
	return js.Trim(result)
}

// sortedGroupNames 按分组出现顺序返回命名分组名（与 match.groups 的属性顺序一致）。
func sortedGroupNames(names map[string]int) []string {
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && names[out[j]] < names[out[j-1]]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func safeRegionAlias(regionValue any) string {
	region := toRegionZh(regionValue)
	aliases := map[string]string{"香港": "HK", "台湾": "TW", "日本": "JP", "新加坡": "SG", "美国": "US", "韩国": "KR", "英国": "UK"}
	if a, ok := aliases[region]; ok {
		return a
	}
	return toRegionCode(region)
}

func safeProtocolAlias(protocolValue any) string {
	protocol := normalizeProtocol(protocolValue)
	switch protocol {
	case "hysteria2":
		return "hy2"
	case "shadowsocks":
		return "ss"
	}
	return protocol
}

func buildDslContext(record *js.Object) *js.Object {
	return js.Obj(
		"name", record.Get("name"),
		"originalName", record.Get("originalName"),
		"protocol", record.Get("protocol"),
		"region", record.Get("region"),
		"regionZh", record.Get("regionZh"),
		"emoji", record.Get("emoji"),
		"server", record.Get("server"),
		"port", record.Get("port"),
		"index", js.Coalesce(record.Get("index"), ""),
		"regionAlias", safeRegionAlias(js.Or(record.Get("regionZh"), record.Get("region"))),
		"protocolAlias", safeProtocolAlias(record.Get("protocol")),
	)
}

func applyScriptRename(record *js.Object, expression string) any {
	if expression == "" {
		return record.Get("name")
	}
	tpl := expression
	if !strings.Contains(expression, "{") {
		tpl = "{" + expression + "}"
	}
	if r := js.Trim(RenderDslTemplate(tpl, buildDslContext(record))); r != "" {
		return r
	}
	return record.Get("name")
}

func evaluateScriptExpression(record *js.Object, expression string) bool {
	if expression == "" {
		return true
	}
	return EvaluateDslExpression(expression, buildDslContext(record))
}

func makeDedupKey(record *js.Object, cfg *transformConfig) string {
	server := js.ToLower(js.Trim(strOr(record.Get("server"))))
	port := js.Trim(strOr(record.Get("port")))
	if server == "" || port == "" {
		return ""
	}
	base := server + ":" + port
	if cfg.DedupIncludeProtocol {
		return js.ToString(record.Get("protocol")) + "|" + base
	}
	return base
}

func choosePreferred(existing, candidate *js.Object, protocolOrder []string) *js.Object {
	if existing == nil {
		return candidate
	}
	if len(protocolOrder) == 0 {
		return existing
	}
	rank := func(p any) int {
		s := js.ToLower(strOr(p))
		for i, x := range protocolOrder {
			if x == s {
				return i
			}
		}
		return math.MaxInt
	}
	if rank(candidate.Get("protocol")) < rank(existing.Get("protocol")) {
		return candidate
	}
	return existing
}

func buildRegionCodeToZhMap() map[string]string {
	m := map[string]string{}
	for _, e := range RegionKeywords {
		for _, k := range e.Keywords {
			code := js.Trim(k)
			if reRegionCodeStatic(code) {
				upper := strings.ToUpper(code)
				if _, exists := m[upper]; !exists {
					m[upper] = e.Region
				}
			}
		}
	}
	if gb, ok := m["GB"]; ok {
		if _, has := m["UK"]; !has {
			m["UK"] = gb
		}
	}
	return m
}

func buildZhToCodeMap() map[string]string {
	m := map[string]string{}
	for _, e := range RegionKeywords {
		for _, k := range e.Keywords {
			code := js.Trim(k)
			if reRegionCodeStatic(code) {
				m[e.Region] = strings.ToUpper(code)
				break
			}
		}
	}
	return m
}

// reRegionCodeStatic 在包初始化阶段使用，等价于 /^[A-Za-z]{2,3}$/。
func reRegionCodeStatic(s string) bool {
	if len(s) < 2 || len(s) > 3 {
		return false
	}
	for _, r := range s {
		if !isASCIILetterByte(r) {
			return false
		}
	}
	return true
}

func toRegionCode(zhRegion any) string {
	region := js.Trim(strOr(zhRegion))
	if region == "" {
		return ""
	}
	if reRegionCode.Test(region) {
		return strings.ToUpper(region)
	}
	if c, ok := regionZhToCode[region]; ok {
		return c
	}
	return region
}

func toRegionZh(value any) string {
	region := js.Trim(strOr(value))
	if region == "" {
		return ""
	}
	if js.ContainsHan(region) {
		return region
	}
	if zh, ok := regionCodeToZh[js.ToUpper(region)]; ok {
		return zh
	}
	return region
}

var reAllDigitsMod = js.MustRegExp(`^\d+$`, "")

func applyModifier(key string, value any, modifier string, record *js.Object) any {
	val := ""
	if !js.IsNullish(value) {
		val = js.ToString(value)
	}
	switch modifier {
	case "UPPER":
		return js.ToUpper(val)
	case "lower":
		return js.ToLower(val)
	case "Title":
		return js.CharAtUpperFirst(val)
	case "zh":
		if key == "region" && record != nil && js.Truthy(record.Get("regionZh")) {
			return record.Get("regionZh")
		}
		if key == "region" {
			return toRegionZh(val)
		}
		return val
	}
	if key == "index" && reAllDigitsMod.Test(modifier) {
		return js.PadStart(js.ToString(value), int(js.ParseInt(modifier, 0)), "0")
	}
	return val
}

var reTemplateVar = js.MustRegExp(`\{([a-zA-Z0-9_]+)(?::([a-zA-Z0-9]+))?\}`, "g")

// RenderTemplate 对应 renderTemplate：{key} / {key:modifier} 变量与 {g1} / {g_name} 捕获组变量。
func RenderTemplate(template any, vars *js.Object, record *js.Object) string {
	out := reTemplateVar.ReplaceFunc(strOr(template), func(m *js.Match) string {
		key := m.Str(1)
		modifier := m.Group(2)
		var v any
		if vars.Has(key) {
			v = vars.Get(key)
		} else if strings.HasPrefix(key, "g") && record != nil && js.Truthy(record.Get("regexGroups")) {
			gKey := key[1:]
			if strings.HasPrefix(gKey, "_") {
				gKey = gKey[1:]
			}
			v = objGet(record.Get("regexGroups"), gKey)
		}
		if v == nil {
			return ""
		}
		if js.Truthy(modifier) {
			v = applyModifier(key, v, js.ToString(modifier), record)
		}
		if js.IsNullish(v) {
			return ""
		}
		return js.ToString(v)
	})
	return js.Trim(out)
}

func padIndex(n float64, width any) string {
	if w := js.ToNumber(width); w > 0 {
		return js.PadStart(js.NumberToString(n), int(w), "0")
	}
	return js.NumberToString(n)
}

func getIndexGroupKey(record *js.Object, scope any) string {
	switch js.ToString(scope) {
	case "region":
		return "r:" + js.ToString(record.Get("region"))
	case "protocol":
		return "p:" + js.ToString(record.Get("protocol"))
	case "regionProtocol":
		return "rp:" + js.ToString(record.Get("region")) + "|" + js.ToString(record.Get("protocol"))
	}
	return "global"
}

var reIPv4Sort = js.MustRegExp(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$`, "")

// MakeComparator 对应 makeComparator；返回值与 JS 比较函数一致（负数 / 0 / 正数）。
func MakeComparator(sortCfg *js.Object) func(a, b any) float64 {
	keys := arrayOrEmpty(js.Or(sortCfg.Get("keys"), js.Arr()))
	nameIgnoreEmoji := !js.StrictEquals(sortCfg.Get("nameIgnoreEmoji"), false)
	orderMaps := make([]map[string]int, keys.Len())
	for i, k := range keys.Items {
		orderList, isArr := objGet(k, "customOrder").(*js.Array)
		if !isArr || orderList.Len() == 0 {
			switch {
			case js.StrictEquals(objGet(k, "key"), "region"):
				orderList, isArr = strs2("香港", "台湾", "日本", "新加坡", "美国", "韩国", "英国", "德国", "法国", "加拿大"), true
			case js.StrictEquals(objGet(k, "key"), "protocol"):
				orderList, isArr = strs2("vless", "trojan", "vmess", "hysteria2", "ss", "ssr", "anytls"), true
			}
		}
		if !isArr {
			continue
		}
		m := map[string]int{}
		for j, v := range orderList.Items {
			m[js.ToString(v)] = j
		}
		orderMaps[i] = m
	}
	cmpStr := func(a, b any) float64 { return float64(js.LocaleCompareNumericBase(strOr(a), strOr(b))) }
	cmpNum := func(a, b any) float64 {
		an, bn := js.ToNumber(a), js.ToNumber(b)
		switch {
		case math.IsNaN(an) && math.IsNaN(bn):
			return 0
		case math.IsNaN(an):
			return 1
		case math.IsNaN(bn):
			return -1
		}
		return an - bn
	}
	cmpIP := func(a, b any) float64 {
		ma := reIPv4Sort.MatchFirst(js.ToString(a))
		mb := reIPv4Sort.MatchFirst(js.ToString(b))
		if ma != nil && mb != nil {
			for i := 1; i <= 4; i++ {
				if d := js.ParseInt(ma.Str(i), 0) - js.ParseInt(mb.Str(i), 0); d != 0 {
					return d
				}
			}
			return 0
		}
		if ma != nil {
			return -1
		}
		if mb != nil {
			return 1
		}
		return cmpStr(a, b)
	}
	byOrder := func(orderMap map[string]int, va, vb any) (float64, bool) {
		if orderMap == nil {
			return 0, false
		}
		ia, okA := orderMap[strOr(va)]
		ib, okB := orderMap[strOr(vb)]
		ra, rb := float64(ia), float64(ib)
		if !okA {
			ra = 9007199254740991
		}
		if !okB {
			rb = 9007199254740991
		}
		if ra != rb {
			return ra - rb, true
		}
		return 0, false
	}
	return func(av, bv any) float64 {
		ra, rb := objOrEmpty(av), objOrEmpty(bv)
		for i, k := range keys.Items {
			if !js.Truthy(objGet(k, "key")) {
				continue
			}
			key := js.ToString(objGet(k, "key"))
			order := 1.0
			if js.ToLower(js.ToString(js.Or(objGet(k, "order"), "asc"))) == "desc" {
				order = -1
			}
			orderMap := orderMaps[i]
			switch key {
			case "name":
				var va, vb any = ra.Get("name"), rb.Get("name")
				if nameIgnoreEmoji {
					va, vb = stripLeadingEmoji(va), stripLeadingEmoji(vb)
				}
				if r := cmpStr(va, vb); r != 0 {
					return r * order
				}
				continue
			case "region":
				va := js.Or(ra.Get("regionZh"), ra.Get("region"))
				vb := js.Or(rb.Get("regionZh"), rb.Get("region"))
				if d, ok := byOrder(orderMap, va, vb); ok {
					return d * order
				}
				if r := cmpStr(va, vb); r != 0 {
					return r * order
				}
				continue
			case "group":
				if r := cmpStr(js.Or(ra.Get("group"), ""), js.Or(rb.Get("group"), "")); r != 0 {
					return r * order
				}
				continue
			case "port":
				if r := cmpNum(ra.Get("port"), rb.Get("port")); r != 0 {
					return r * order
				}
				continue
			case "server":
				if r := cmpIP(ra.Get("server"), rb.Get("server")); r != 0 {
					return r * order
				}
				continue
			}
			va, vb := ra.Get(key), rb.Get(key)
			if d, ok := byOrder(orderMap, va, vb); ok {
				return d * order
			}
			if r := cmpStr(va, vb); r != 0 {
				return r * order
			}
		}
		return 0
	}
}

// NodeURLsToRecords 对应 nodeUrlsToRecords。
func NodeURLsToRecords(nodeURLs []string, ensureRegion, enableEmoji bool) []*js.Object {
	out := []*js.Object{}
	for _, raw := range nodeURLs {
		u := js.Trim(raw)
		if u == "" {
			continue
		}
		info := ParseNodeInfo(u)
		record := js.Obj(
			"url", u,
			"protocol", info.Protocol,
			"name", info.Name,
			"originalName", info.Name,
			"region", "",
			"emoji", "",
			"server", js.Or(info.Server, ""),
			"port", js.Or(info.Port, ""),
			"metadata", ExtractNodeMetadata(info.Name),
		)
		if ensureRegion {
			record = EnsureRegionInfo(record, enableEmoji)
		}
		out = append(out, record)
	}
	return out
}

// RecordsToNodeURLs 对应 recordsToNodeUrls。
func RecordsToNodeURLs(records []*js.Object) []any {
	out := make([]any, 0, len(records))
	for _, r := range records {
		if js.Truthy(r.Get("name")) && !js.StrictEquals(r.Get("name"), r.Get("originalName")) {
			out = append(out, SetNodeName(r.Get("url"), r.Get("protocol"), r.Get("name")))
		} else {
			out = append(out, r.Get("url"))
		}
	}
	return out
}

// ApplyNodeTransformPipeline 对应 applyNodeTransformPipeline；未启用时原样返回。
func ApplyNodeTransformPipeline(nodeURLs []string, transformConfig any) []any {
	cfg := normalizeTransformConfig(transformConfig)
	if !cfg.Enabled {
		out := make([]any, len(nodeURLs))
		for i, u := range nodeURLs {
			out[i] = u
		}
		return out
	}
	sortKeySet := map[string]bool{}
	if cfg.SortEnabled {
		for _, k := range cfg.SortKeys.Items {
			sortKeySet[js.ToString(js.Or(objGet(k, "key"), ""))] = true
		}
	}
	records := NodeURLsToRecords(nodeURLs, false, false)
	needRegionEmoji := cfg.TemplateEnabled ||
		(cfg.RegionsEnabled && len(cfg.RegionValues) > 0) ||
		(cfg.SortEnabled && sortKeySet["region"]) ||
		(cfg.FilterScriptEnabled && cfg.FilterScriptExpr != "")
	filterRecords := func(pred func(*js.Object) bool) {
		out := records[:0:0]
		for _, r := range records {
			if pred(r) {
				out = append(out, r)
			}
		}
		records = out
	}
	if cfg.IncludeEnabled && cfg.IncludeRules.Len() > 0 {
		filterRecords(func(r *js.Object) bool { return MatchesRegexRules(r.Get("name"), cfg.IncludeRules.Items) })
	}
	if cfg.ExcludeEnabled && cfg.ExcludeRules.Len() > 0 {
		filterRecords(func(r *js.Object) bool { return !MatchesRegexRules(r.Get("name"), cfg.ExcludeRules.Items) })
	}
	if cfg.ProtocolsEnabled && len(cfg.ProtocolValues) > 0 {
		allowed := map[string]bool{}
		for _, p := range cfg.ProtocolValues {
			allowed[normalizeProtocol(p)] = true
		}
		filterRecords(func(r *js.Object) bool { return allowed[js.ToString(r.Get("protocol"))] })
	}
	if cfg.RegionsEnabled && len(cfg.RegionValues) > 0 {
		allowed := map[string]bool{}
		for _, v := range cfg.RegionValues {
			allowed[toRegionZh(v)] = true
		}
		for i, r := range records {
			records[i] = EnsureRegionInfo(r, cfg.EnableEmoji)
		}
		filterRecords(func(r *js.Object) bool { return allowed[js.ToString(r.Get("regionZh"))] })
	}
	if cfg.FilterScriptEnabled && cfg.FilterScriptExpr != "" {
		for i, r := range records {
			base := r
			if !needRegionEmoji {
				base = EnsureRegionInfo(r, cfg.EnableEmoji)
			}
			n := base.Clone()
			n.Set("index", float64(i+1))
			records[i] = n
		}
		filterRecords(func(r *js.Object) bool { return evaluateScriptExpression(r, cfg.FilterScriptExpr) })
	}
	if cfg.UselessEnabled {
		filterRecords(func(r *js.Object) bool { return !isUselessNode(r) })
	}
	if cfg.RegexEnabled && cfg.RegexRules.Len() > 0 {
		for i, r := range records {
			newName := ApplyRegexRename(r.Get("name"), cfg.RegexRules, r)
			n := r.Clone()
			n.Set("name", newName)
			records[i] = n
		}
	}
	if cfg.DedupEnabled {
		if cfg.DedupMode == "url" {
			seen := map[string]bool{}
			filterRecords(func(r *js.Object) bool {
				k := js.ToString(r.Get("url"))
				if seen[k] {
					return false
				}
				seen[k] = true
				return true
			})
		} else {
			var keys []string
			m := map[string]*js.Object{}
			for _, r := range records {
				key := makeDedupKey(r, cfg)
				if key == "" {
					key = "__raw__:" + js.ToString(r.Get("url"))
					if _, ok := m[key]; !ok {
						keys = append(keys, key)
					}
					m[key] = r
					continue
				}
				if _, ok := m[key]; !ok {
					keys = append(keys, key)
				}
				m[key] = choosePreferred(m[key], r, cfg.ProtocolOrder)
			}
			records = records[:0:0]
			for _, k := range keys {
				records = append(records, m[k])
			}
		}
	}
	if needRegionEmoji {
		for i, r := range records {
			records[i] = EnsureRegionInfo(r, cfg.EnableEmoji)
		}
	}
	if cfg.RenameScriptEnabled && cfg.RenameScriptExpr != "" {
		for i, r := range records {
			enriched := r
			if !needRegionEmoji {
				enriched = EnsureRegionInfo(r, cfg.EnableEmoji)
			}
			withIndex := enriched.Clone()
			withIndex.Set("index", float64(i+1))
			newName := applyScriptRename(withIndex, cfg.RenameScriptExpr)
			n := enriched.Clone()
			n.Set("name", newName)
			if js.Truthy(newName) {
				n.Set("url", SetNodeName(enriched.Get("url"), enriched.Get("protocol"), newName))
			} else {
				n.Set("url", enriched.Get("url"))
			}
			records[i] = n
		}
	}
	if cfg.TemplateEnabled {
		templateHasEmoji := strings.Contains(cfg.Template, "{emoji}")
		var groupOrder []string
		buckets := map[string][]*js.Object{}
		for _, r := range records {
			gk := getIndexGroupKey(r, cfg.IndexScope)
			if _, ok := buckets[gk]; !ok {
				groupOrder = append(groupOrder, gk)
			}
			buckets[gk] = append(buckets[gk], r)
		}
		for _, gk := range groupOrder {
			current := js.ToNumber(cfg.IndexStart)
			for _, r := range buckets[gk] {
				regionText := js.Or(objGet(cfg.RegionAlias, js.ToString(r.Get("region"))), r.Get("region"))
				protocolText := js.Or(objGet(cfg.ProtocolAlias, js.ToString(r.Get("protocol"))), r.Get("protocol"))
				idx := current
				current++
				var nameVar any = r.Get("name")
				if templateHasEmoji {
					nameVar = stripLeadingEmoji(r.Get("name"))
				}
				vars := js.Obj("emoji", r.Get("emoji"), "region", regionText, "protocol", protocolText,
					"index", padIndex(idx, cfg.IndexPad), "name", nameVar, "server", r.Get("server"), "port", r.Get("port"))
				newName := RenderTemplate(cfg.Template, vars, r)
				r.Set("name", newName)
				r.Set("url", SetNodeName(r.Get("url"), r.Get("protocol"), newName))
			}
		}
	} else if cfg.RegexEnabled && cfg.RegexRules.Len() > 0 {
		for i, r := range records {
			n := r.Clone()
			if js.Truthy(r.Get("name")) {
				n.Set("url", SetNodeName(r.Get("url"), r.Get("protocol"), r.Get("name")))
			}
			records[i] = n
		}
	}
	if cfg.SortEnabled && cfg.SortKeys.Len() > 0 {
		items := make([]any, len(records))
		for i, r := range records {
			items[i] = r
		}
		js.SortStable(items, MakeComparator(js.Obj("keys", cfg.SortKeys, "nameIgnoreEmoji", cfg.NameIgnoreEmoji)))
		for i, it := range items {
			records[i] = it.(*js.Object)
		}
	}
	out := make([]any, len(records))
	for i, r := range records {
		out[i] = r.Get("url")
	}
	return out
}
