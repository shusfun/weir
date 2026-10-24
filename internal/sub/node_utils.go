package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/utils/node-utils.js

// NodeProtocolRegexUtils 对应 node-utils.js 的 NODE_PROTOCOL_REGEX。
var NodeProtocolRegexUtils = js.MustRegExp(`^(ss|ssr|vmess|vless|trojan|hysteria2?|hy|hy2|tuic|snell|anytls|socks5|socks|wireguard|naive\+https?|naive\+quic|mierus):\/\/`, "i")

var (
	reLocalAtHost    = js.MustRegExp(`@(?:\[([^\]]+)\]|([^:?#/]+))`, "")
	reBracketTrim    = js.MustRegExp(`^\[|\]$`, "g")
	reTrailingDot    = js.MustRegExp(`\.$`, "")
	reLoopback4      = js.MustRegExp(`^127(?:\.\d{1,3}){3}$`, "")
	reLoopbackMapped = js.MustRegExp(`^::ffff:127(?:\.\d{1,3}){3}$`, "")
)

// IsLocalProxyEndpoint 对应 isLocalProxyEndpoint；参数可以是代理对象或节点链接。
func IsLocalProxyEndpoint(proxy any) bool {
	var host any = ""
	switch p := proxy.(type) {
	case *js.Object:
		host = js.Or(p.Get("server"), p.Get("host"), "")
	case *js.Array:
		host = ""
	case string:
		if u, err := js.ParseURL(p); err == nil {
			host = u.Hostname()
		} else {
			m := reLocalAtHost.MatchFirst(p)
			host = ""
			if m != nil {
				host = js.Or(m.Group(1), m.Group(2), "")
			}
		}
	}
	h := js.ToLower(js.Trim(js.ToString(host)))
	h = reBracketTrim.Replace(h, "")
	h = reTrailingDot.Replace(h, "")
	return h == "localhost" || h == "::1" || h == "::" || h == "0.0.0.0" || reLoopback4.Test(h) || reLoopbackMapped.Test(h)
}

func normalizeBase64Utils(input any) string {
	s := stripJSSpace(strOr(input))
	s = urlSafeToStd(s)
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	return s
}

func base64URLSafeEncodeUTF8(s string) string {
	return strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(js.Base64EncodeUTF8(s)), "=")
}

// base64URLSafeDecodeUTF8 对应 decodeURIComponent(escape(atob(normalizeBase64(str))))。
func base64URLSafeDecodeUTF8(s string) (string, error) {
	bin, err := js.Atob(normalizeBase64Utils(s))
	if err != nil {
		return "", err
	}
	return js.DecodeURIComponentEscape(bin)
}

func updateSsrRemarks(link string, updater func(string) string) string {
	if link == "" || !strings.HasPrefix(js.ToLower(link), "ssr://") {
		return link
	}
	rawPayload := strings.Split(link[len("ssr://"):], "#")[0]
	decoded, err := base64URLSafeDecodeUTF8(rawPayload)
	if err != nil {
		return link
	}
	const queryMarker = "/?"
	queryIndex := strings.Index(decoded, queryMarker)
	if queryIndex == -1 {
		return link
	}
	mainPart := decoded[:queryIndex+len(queryMarker)]
	params := js.ParseSearchParams(decoded[queryIndex+len(queryMarker):])
	oldRemarks := ""
	if r := params.Str("remarks"); r != "" {
		oldRemarks, err = base64URLSafeDecodeUTF8(r)
		if err != nil {
			return link
		}
	}
	newRemarks := updater(oldRemarks)
	if newRemarks == "" || newRemarks == oldRemarks {
		return "ssr://" + base64URLSafeEncodeUTF8(decoded)
	}
	params.Set("remarks", base64URLSafeEncodeUTF8(newRemarks))
	return "ssr://" + base64URLSafeEncodeUTF8(mainPart+params.String())
}

// PrependNodeName 对应 prependNodeName。
func PrependNodeName(link, prefix string) string {
	if prefix == "" {
		return link
	}
	if strings.HasPrefix(js.ToLower(link), "ssr://") {
		return updateSsrRemarks(link, func(originalName string) string {
			if strings.HasPrefix(originalName, prefix) {
				return originalName
			}
			if originalName != "" {
				return prefix + " - " + originalName
			}
			return prefix
		})
	}
	appendToFragment := func(baseLink, namePrefix string) string {
		hashIndex := strings.LastIndex(baseLink, "#")
		originalName := ""
		if hashIndex != -1 {
			originalName = js.SafeDecodeURIComponent(baseLink[hashIndex+1:])
		}
		base := baseLink
		if hashIndex != -1 {
			base = baseLink[:hashIndex]
		}
		if strings.HasPrefix(originalName, namePrefix) {
			return baseLink
		}
		newName := namePrefix
		if originalName != "" {
			newName = namePrefix + " - " + originalName
		}
		return base + "#" + js.EncodeURIComponent(newName)
	}
	if strings.HasPrefix(link, "vmess://") {
		b, err := js.AtobBytes(link[len("vmess://"):])
		if err == nil {
			cfgVal, err2 := js.JSONParse(js.TextDecode(b))
			if cfg, isObj := cfgVal.(*js.Object); err2 == nil && isObj {
				originalPs := js.Or(cfg.Get("ps"), "")
				ps, isStr := originalPs.(string)
				if !isStr {
					// 非字符串的 ps 调用 startsWith 会抛出异常，回退到通用方法
					return appendToFragment(link, prefix)
				}
				if !strings.HasPrefix(ps, prefix) {
					if ps != "" {
						cfg.Set("ps", prefix+" - "+ps)
					} else {
						cfg.Set("ps", prefix)
					}
				}
				return "vmess://" + js.Base64EncodeUTF8(js.MustStringify(cfg))
			} else if arr, isArr := cfgVal.(*js.Array); err2 == nil && isArr {
				// 数组上的 ps 属性不会被 JSON.stringify 输出，结果等同于重新编码原数组
				return "vmess://" + js.Base64EncodeUTF8(js.MustStringify(arr))
			}
			// 其余情况（null 或原始值）在严格模式下赋值会抛出异常，回退到通用方法
		}
		return appendToFragment(link, prefix)
	}
	return appendToFragment(link, prefix)
}

// ExtractRegionFromNodeName 对应 extractRegionFromNodeName。
func ExtractRegionFromNodeName(nodeName any) string { return ExtractNodeRegion(nodeName) }

var (
	reHasFlagEmoji   = js.MustRegExp(`[\u{1F1E6}-\u{1F1FF}]{2}|\u{1F3F4}[\u{E0061}-\u{E007A}]{2,}\u{E007F}`, "u")
	reProtocolLazy   = js.MustRegExp(`^(.*?):\/\/`, "")
	reQueryOrHash    = js.MustRegExp(`[?#]`, "")
	reStripPatterns  []*js.RegExp
	reStripMultiSpce = js.MustRegExp(`\s{2,}`, "g")
)

func init() {
	reStripPatterns = []*js.RegExp{
		js.MustRegExp(`[\u{1F1E6}-\u{1F1FF}]{2}`, "gu"),
		js.MustRegExp(`\u{1F3F4}[\u{E0061}-\u{E007A}]{2,}\u{E007F}`, "gu"),
		js.MustRegExp(`\u{1F3F3}\uFE0F?\u200D\u{1F308}`, "gu"),
		js.MustRegExp(`\u{1F3F3}\uFE0F?\u200D\u{26A7}`, "gu"),
		js.MustRegExp(`[\u{1F3F1}\u{1F3F3}\u{1F3F4}\u{1F6A9}\u{1F3C1}\u{1F38C}]`, "gu"),
	}
}

// AddFlagEmoji 对应 addFlagEmoji。
func AddFlagEmoji(link string) string {
	if link == "" {
		return link
	}
	appendEmoji := func(name string) string {
		if name == "" {
			return name
		}
		if reHasFlagEmoji.Test(name) {
			return name
		}
		flag := js.ToString(ExtractNodeMetadata(name).Get("flag"))
		if flag == "" {
			return name
		}
		return flag + " " + name
	}
	if strings.HasPrefix(js.ToLower(link), "ssr://") {
		return updateSsrRemarks(link, appendEmoji)
	}
	if strings.HasPrefix(link, "vmess://") {
		b, err := js.AtobBytes(normalizeBase64Utils(link[len("vmess://"):]))
		if err != nil {
			return link
		}
		cfgVal, err := js.JSONParse(js.TextDecode(b))
		if err != nil || js.IsNullish(cfgVal) {
			return link
		}
		cfg, isObj := cfgVal.(*js.Object)
		if !isObj {
			return link
		}
		ps := cfg.Get("ps")
		if !js.Truthy(ps) {
			return link
		}
		psStr, isStr := ps.(string)
		if !isStr {
			// 非字符串名称在 appendEmoji 中总是原样返回，因此链接不变
			return link
		}
		updated := appendEmoji(psStr)
		if updated == psStr {
			return link
		}
		cfg.Set("ps", updated)
		return "vmess://" + js.Base64EncodeUTF8(js.MustStringify(cfg))
	}
	hashIndex := strings.LastIndex(link, "#")
	originalName := ""
	basePart := link
	if hashIndex != -1 {
		basePart = link[:hashIndex]
		rawName := link[hashIndex+1:]
		if d, err := js.DecodeURIComponent(rawName); err == nil {
			originalName = d
			if strings.Contains(originalName, "%") {
				if d2, err := js.DecodeURIComponent(originalName); err == nil {
					originalName = d2
				} else {
					originalName = rawName
				}
			}
		} else {
			originalName = rawName
		}
	} else if m := reProtocolLazy.MatchFirst(link); m != nil {
		protocol := m.Str(1)
		rest := link[len(protocol)+3:]
		atIdx := strings.LastIndex(rest, "@")
		hostPortPart := rest
		if atIdx != -1 {
			hostPortPart = rest[atIdx+1:]
		}
		hostPortPart = reQueryOrHash.Split(hostPortPart, -1)[0]
		originalName = strings.Split(hostPortPart, ":")[0]
		if originalName == "" {
			originalName = "Node"
		}
	}
	if originalName == "" {
		return link
	}
	newName := appendEmoji(originalName)
	if newName == originalName && hashIndex == -1 {
		return link
	}
	return basePart + "#" + js.EncodeURIComponent(newName)
}

func stripFlagEmoji(name string) string {
	if name == "" {
		return name
	}
	cleaned := name
	for _, p := range reStripPatterns {
		cleaned = p.Replace(cleaned, "")
	}
	return js.Trim(reStripMultiSpce.Replace(cleaned, " "))
}

func decodeVmessPayload(raw string) any {
	base64Part := raw
	if strings.Contains(base64Part, "%") {
		d, err := js.DecodeURIComponent(base64Part)
		if err != nil {
			return nil
		}
		base64Part = d
	}
	base64Part = padBase64(urlSafeToStd(stripJSSpace(base64Part)))
	b, err := js.AtobBytes(base64Part)
	if err != nil {
		return nil
	}
	v, err := js.JSONParse(js.TextDecode(b))
	if err != nil {
		return nil
	}
	return v
}

// RemoveFlagEmoji 对应 removeFlagEmoji。
func RemoveFlagEmoji(link string) string {
	if link == "" {
		return link
	}
	if strings.HasPrefix(link, "vmess://") {
		cfgVal := decodeVmessPayload(link[len("vmess://"):])
		cfg, isObj := cfgVal.(*js.Object)
		if !isObj {
			if _, isArr := cfgVal.(*js.Array); !isArr {
				return link
			}
			return link
		}
		ps := cfg.Get("ps")
		if !js.Truthy(ps) {
			return link
		}
		psStr, isStr := ps.(string)
		if !isStr {
			// 非字符串调用 replace 会抛出异常，上游捕获后返回原链接
			return link
		}
		cfg.Set("ps", stripFlagEmoji(psStr))
		return "vmess://" + js.Base64EncodeUTF8(js.MustStringify(cfg))
	}
	hashIndex := strings.LastIndex(link, "#")
	if hashIndex == -1 {
		return link
	}
	originalName, err := js.DecodeURIComponent(link[hashIndex+1:])
	if err != nil {
		return link
	}
	return link[:hashIndex+1] + js.EncodeURIComponent(stripFlagEmoji(originalName))
}

var (
	reHy2Auth       = js.MustRegExp(`^(hysteria2|hy2):\/\/([^@]+)@`, "i")
	reUrlSeparators = js.MustRegExp(`[@/?#]`, "")
	reHy2Query      = js.MustRegExp(`([?&](?:obfs-password|auth|password)=)([^&]+)`, "gi")
	reAmpEq         = js.MustRegExp(`[&=]`, "")
)

// FixOptions 对应 fixNodeUrlEncoding 的 options。
type FixOptions struct {
	PlusAsSpace bool
}

func fixOptionsFrom(v any) FixOptions {
	o, _ := v.(*js.Object)
	return FixOptions{PlusAsSpace: js.Truthy(o.Get("plusAsSpace"))}
}

// FixNodeURLEncoding 对应 node-utils.js 的 fixNodeUrlEncoding（parseNodeList 使用的版本）。
func FixNodeURLEncoding(nodeURL string, options FixOptions) string {
	if nodeURL == "" {
		return nodeURL
	}
	normalizeFragment := func(u string) string {
		hashIndex := strings.LastIndex(u, "#")
		if hashIndex == -1 {
			return u
		}
		base := u[:hashIndex+1]
		rawFragment := u[hashIndex+1:]
		if rawFragment == "" {
			return u
		}
		toDecode := rawFragment
		if options.PlusAsSpace {
			toDecode = strings.ReplaceAll(rawFragment, "+", " ")
		}
		decoded, err := js.DecodeURIComponent(toDecode)
		if err != nil {
			return u
		}
		return base + js.EncodeURIComponent(decoded)
	}
	if strings.HasPrefix(nodeURL, "hysteria2://") || strings.HasPrefix(nodeURL, "hy2://") {
		nodeURL = reHy2Auth.ReplaceFunc(nodeURL, func(m *js.Match) string {
			auth := m.Str(2)
			decoded := js.SafeDecodeURIComponent(auth)
			if decoded == auth {
				return m.Str(0)
			}
			if reUrlSeparators.Test(decoded) {
				return m.Str(0)
			}
			return m.Str(1) + "://" + decoded + "@"
		})
		nodeURL = reHy2Query.ReplaceFunc(nodeURL, func(m *js.Match) string {
			decoded := js.SafeDecodeURIComponent(m.Str(2))
			if reAmpEq.Test(decoded) {
				return m.Str(0)
			}
			return m.Str(1) + decoded
		})
		return normalizeFragment(nodeURL)
	}
	if strings.HasPrefix(nodeURL, "snell://") {
		u, err := js.ParseURL(nodeURL)
		if err != nil {
			return normalizeFragment(nodeURL)
		}
		sp := u.SearchParams()
		if sp.Has("ecn") {
			sp.Delete("ecn")
		}
		return normalizeFragment(u.Href())
	}
	if !strings.HasPrefix(nodeURL, "ss://") && !strings.HasPrefix(nodeURL, "vless://") && !strings.HasPrefix(nodeURL, "trojan://") {
		return nodeURL
	}
	hashIndex := strings.Index(nodeURL, "#")
	baseLink, fragment := nodeURL, ""
	if hashIndex != -1 {
		baseLink, fragment = nodeURL[:hashIndex], nodeURL[hashIndex:]
	}
	protocolEnd := strings.Index(baseLink, "://")
	atIndex := strings.Index(baseLink, "@")
	if protocolEnd != -1 && atIndex != -1 {
		// 与上游一致：若 @ 出现在协议分隔符之前，substring 会交换参数
		start, end := protocolEnd+3, atIndex
		if start > end {
			start, end = end, start
		}
		base64Part := baseLink[start:end]
		if strings.Contains(base64Part, "%") {
			decoded, err := js.DecodeURIComponent(base64Part)
			if err != nil {
				return normalizeFragment(nodeURL)
			}
			baseLink = baseLink[:protocolEnd] + "://" + decoded + baseLink[atIndex:]
		}
	}
	return normalizeFragment(baseLink + fragment)
}

var (
	reUnsafeYamlStart = js.MustRegExp(`^([*&!\[\]\{\},:?#%|>@\-])`, "")
	yamlStartReplacer = []struct{ from, to string }{
		{"*", "★"}, {"&", "＆"}, {"!", "！"}, {"[", "【"}, {"]", "】"}, {"{", "｛"}, {"}", "｝"},
		{",", "，"}, {":", "："}, {"-", "－"}, {"?", "？"}, {"#", "＃"}, {"%", "％"}, {"|", "｜"},
		{">", "＞"}, {"@", "＠"},
	}
)

func sanitizeYamlName(name string) string {
	if name == "" || !reUnsafeYamlStart.Test(name) {
		return name
	}
	// 上游按顺序链式替换开头字符；替换后的全角字符不会再命中后续规则
	for _, r := range yamlStartReplacer {
		if strings.HasPrefix(name, r.from) {
			name = r.to + name[len(r.from):]
		}
	}
	return name
}

// SanitizeNodeForYaml 对应 sanitizeNodeForYaml。
func SanitizeNodeForYaml(nodeURL string) string {
	if nodeURL == "" {
		return nodeURL
	}
	if strings.HasPrefix(nodeURL, "vmess://") {
		cfgVal := decodeVmessPayload(nodeURL[len("vmess://"):])
		cfg, isObj := cfgVal.(*js.Object)
		if !isObj {
			return nodeURL
		}
		ps := cfg.Get("ps")
		if !js.Truthy(ps) {
			return nodeURL
		}
		psStr, isStr := ps.(string)
		if !isStr {
			return nodeURL
		}
		if newPs := sanitizeYamlName(psStr); newPs != psStr {
			cfg.Set("ps", newPs)
			return "vmess://" + js.Base64EncodeUTF8(js.MustStringify(cfg))
		}
		return nodeURL
	}
	hashIndex := strings.LastIndex(nodeURL, "#")
	if hashIndex == -1 {
		return nodeURL
	}
	originalName, err := js.DecodeURIComponent(nodeURL[hashIndex+1:])
	if err != nil {
		return nodeURL
	}
	if newName := sanitizeYamlName(originalName); newName != originalName {
		return nodeURL[:hashIndex+1] + js.EncodeURIComponent(newName)
	}
	return nodeURL
}
