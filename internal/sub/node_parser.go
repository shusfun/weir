package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/utils/node-parser.js

// NodeProtocolRegex 对应 node-parser.js 的 NODE_PROTOCOL_REGEX。
var NodeProtocolRegex = js.MustRegExp(`^(ss|ssr|vmess|vless|trojan|hysteria2|hy2|hysteria|tuic|snell|naive\+https?|naive\+quic|socks5|socks|http|anytls|wireguard|mierus):\/\/`, "i")

var (
	reSurgeLine = js.MustRegExp(`^([^=]+?)\s*=\s*(shadowsocks|ss|ssr|vmess|vless|trojan|hysteria2?|hy2|hysteria|tuic|snell|anytls|socks5|http|https|wireguard)\s*,\s*([^,]+?)\s*,\s*(\d+)(.*)$`, "i")
	reQXLine    = js.MustRegExp(`^(shadowsocks|ss|ssr|vmess|vless|trojan|hysteria2?|hy2|hysteria|tuic|snell|anytls|socks5|http|https|wireguard)\s*=\s*([^,:]+?)\s*:\s*(\d+)(.*)$`, "i")
	reSlashAll  = js.MustRegExp(`\/`, "g")
	reProxiesKW = js.MustRegExp(`\bproxies\s*:`, "")
	reAnySpace  = js.MustRegExp(`\s`, "g")
	reB64Body   = js.MustRegExp(`^[A-Za-z0-9+/=]+$`, "")
	reCRLF      = js.MustRegExp(`\r\n`, "g")
)

func splitTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	for i, p := range parts {
		parts[i] = js.Trim(p)
	}
	return parts
}

func unquoteKV(v string) string {
	if strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		// 对应 v.slice(1, -1)；单个引号时结果为空串
		if len(v) >= 2 {
			return v[1 : len(v)-1]
		}
		return ""
	}
	return v
}

func ensurePluginOpts(proxy *js.Object) *js.Object {
	po, ok := proxy.Get("pluginOpts").(*js.Object)
	if !ok || !js.Truthy(proxy.Get("pluginOpts")) {
		po = js.NewObject()
		proxy.Set("pluginOpts", po)
	}
	return po
}

func ensureRealityOpts(proxy *js.Object) *js.Object {
	ro, ok := proxy.Get("reality-opts").(*js.Object)
	if !ok {
		ro = js.NewObject()
		proxy.Set("reality-opts", ro)
	}
	return ro
}

// parseSurgeOrQxLine 对应同名内部函数，返回 nil 表示不是 Surge/QX 行。
func parseSurgeOrQxLine(line string) *js.Object {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
		return nil
	}
	if m := reSurgeLine.MatchFirst(line); m != nil {
		proxy := js.Obj("name", js.Trim(m.Str(1)), "type", js.ToLower(m.Str(2)), "server", js.Trim(m.Str(3)), "port", js.ToNumber(m.Str(4)))
		typ := js.ToString(proxy.Get("type"))
		if extra := m.Str(5); extra != "" {
			positional := 0
			for _, p := range splitTrim(extra, ",") {
				if p == "" {
					continue
				}
				kv := strings.Split(p, "=")
				if len(kv) >= 2 {
					k := js.ToLower(js.Trim(kv[0]))
					v := unquoteKV(js.Trim(strings.Join(kv[1:], "=")))
					if k == "password" || k == "auth" || k == "psk" || k == "username" || k == "uuid" {
						switch typ {
						case "vless", "vmess":
							proxy.Set("uuid", v)
						case "snell":
							proxy.Set("psk", v)
						default:
							proxy.Set("password", v)
						}
					}
					if k == "private-key" {
						proxy.Set("private-key", v)
					}
					if k == "peer-public-key" || k == "public-key" {
						proxy.Set("public-key", v)
					}
					if k == "self-ip" {
						proxy.Set("ip", v)
					}
					if k == "client-id" {
						proxy.Set("reserved", reSlashAll.Replace(v, ","))
					}
					if k == "token" && typ == "tuic" {
						proxy.Set("password", v)
						proxy.Set("token", v)
					}
					if k == "sni" {
						proxy.Set("sni", v)
					}
					if k == "skip-cert-verify" && v == "true" {
						proxy.Set("skipCertVerify", true)
					}
					if k == "encrypt-method" || k == "cipher" || k == "method" {
						proxy.Set("cipher", v)
					}
					if k == "obfs" {
						ensurePluginOpts(proxy).Set("mode", v)
					}
					if k == "obfs-host" {
						ensurePluginOpts(proxy).Set("host", v)
					}
					if k == "version" {
						proxy.Set("version", js.ParseInt(v, 0))
					}
					if k == "reuse" && v == "true" {
						proxy.Set("reuse", true)
					}
					if k == "tfo" && v == "true" {
						proxy.Set("tfo", true)
					}
					if k == "udp-relay" && v == "true" {
						proxy.Set("udp", true)
					}
				} else {
					val := js.Trim(p)
					if val == "" {
						continue
					}
					switch {
					case typ == "shadowsocks" || typ == "ss":
						if positional == 0 {
							proxy.Set("cipher", val)
						} else if positional == 1 {
							proxy.Set("password", val)
						}
					case typ == "vmess" || typ == "vless":
						if positional == 0 {
							proxy.Set("uuid", val)
						}
					case typ == "trojan" || strings.HasPrefix(typ, "hysteria") || typ == "hy2" || typ == "tuic":
						if positional == 0 {
							proxy.Set("password", val)
						}
					case typ == "snell":
						if positional == 0 {
							proxy.Set("psk", val)
						}
					}
					positional++
				}
			}
		}
		return proxy
	}
	if m := reQXLine.MatchFirst(line); m != nil {
		proxy := js.Obj("name", "Untitled", "type", js.ToLower(m.Str(1)), "server", js.Trim(m.Str(2)), "port", js.ToNumber(m.Str(3)))
		typ := js.ToString(proxy.Get("type"))
		if extra := m.Str(4); extra != "" {
			for _, p := range splitTrim(extra, ",") {
				if p == "" {
					continue
				}
				kv := strings.Split(p, "=")
				if len(kv) < 2 {
					continue
				}
				k := js.ToLower(js.Trim(kv[0]))
				v := unquoteKV(js.Trim(strings.Join(kv[1:], "=")))
				if k == "tag" {
					proxy.Set("name", v)
				}
				if k == "password" || k == "auth" || k == "psk" || k == "username" || k == "uuid" {
					if typ == "vless" || typ == "vmess" {
						proxy.Set("uuid", v)
					} else {
						proxy.Set("password", v)
					}
				}
				if k == "token" && typ == "tuic" {
					proxy.Set("password", v)
					proxy.Set("token", v)
				}
				if k == "sni" || k == "tls-host" || k == "obfs-host" {
					proxy.Set("sni", v)
				}
				if k == "tls-verification" && v == "false" {
					proxy.Set("skipCertVerify", true)
				}
				if k == "method" || k == "cipher" {
					// `none` 是 Shadowsocks 的合法配置，不能在解析阶段被抹掉
					proxy.Set("cipher", v)
				}
				if k == "obfs" {
					if v == "over-tls" {
						proxy.Set("tls", true)
					} else {
						ensurePluginOpts(proxy).Set("mode", v)
					}
				}
				if k == "vless-flow" {
					proxy.Set("flow", v)
				}
				if k == "reality-base64-pubkey" || k == "reality-pubkey" {
					ensureRealityOpts(proxy).Set("public-key", v)
				}
				if k == "reality-hex-shortid" || k == "reality-shortid" {
					ensureRealityOpts(proxy).Set("short-id", v)
				}
				if k == "reality-support-x25519mlkem768" || k == "support-x25519mlkem768" {
					ensureRealityOpts(proxy).Set("support-x25519mlkem768", v == "true" || v == "1")
				}
				if k == "version" {
					proxy.Set("version", js.ParseInt(v, 0))
				}
				if k == "reuse" && v == "true" {
					proxy.Set("reuse", true)
				}
				if k == "tfo" && v == "true" {
					proxy.Set("tfo", true)
				}
				if k == "udp-relay" && v == "true" {
					proxy.Set("udp", true)
				}
			}
		}
		return proxy
	}
	return nil
}

// ExtractValidNodes 对应 extractValidNodes：支持 sing-box JSON、Clash YAML、Base64、纯文本与 Surge/QX 行。
func ExtractValidNodes(textVal any) []string {
	text, isStr := textVal.(string)
	if !isStr || text == "" {
		return []string{}
	}
	nodes := []string{}
	trimmed := js.Trim(text)
	if strings.HasPrefix(trimmed, "{") {
		if v, err := js.JSONParse(trimmed); err == nil {
			if o, ok := v.(*js.Object); ok && js.IsArray(o.Get("outbounds")) {
				nodes = ExtractSingboxNodes(o)
				if len(nodes) > 0 {
					return nodes
				}
			}
		}
	}
	if reProxiesKW.Test(js.ToLower(text)) {
		if doc, err := js.YAMLLoad(text); err == nil && !js.IsNullish(doc) {
			proxies := js.Or(objGet(doc, "proxies"), objGet(doc, "Proxy"))
			if arr, ok := proxies.(*js.Array); ok {
				for _, p := range arr.Items {
					if u, ok := ConvertClashProxyToURL(p); ok && u != "" {
						nodes = append(nodes, u)
					}
				}
				if len(nodes) > 0 {
					return nodes
				}
			}
		}
	}
	processed := text
	cleaned := reAnySpace.Replace(text, "")
	normalized := urlSafeToStd(cleaned)
	if pad := len(normalized) % 4; pad != 0 {
		normalized += strings.Repeat("=", 4-pad)
	}
	if reB64Body.Test(normalized) && len(normalized) > 20 {
		if b, err := js.AtobBytes(normalized); err == nil {
			processed = js.TextDecode(b)
		}
	}
	for _, line := range strings.Split(reCRLF.Replace(processed, "\n"), "\n") {
		line = js.Trim(line)
		if NodeProtocolRegex.Test(line) {
			nodes = append(nodes, line)
			continue
		}
		if proxy := parseSurgeOrQxLine(line); proxy != nil {
			if u, ok := ConvertClashProxyToURL(proxy); ok && u != "" {
				nodes = append(nodes, u)
			}
		}
	}
	return nodes
}

// supportedSSCiphers 对应 SUPPORTED_SS_CIPHERS。
var supportedSSCiphers = map[string]bool{
	"none": true, "aes-128-gcm": true, "aes-256-gcm": true, "chacha20-poly1305": true,
	"chacha20-ietf-poly1305": true, "xchacha20-ietf-poly1305": true, "2022-blake3-aes-128-gcm": true,
	"2022-blake3-aes-256-gcm": true, "2022-blake3-chacha20-poly1305": true,
}

var (
	reVlessStdUUID = js.MustRegExp(`^vless:\/\/([0-9a-fA-F-]{36})@`, "")
	reVlessAnyUser = js.MustRegExp(`^vless:\/\/([^@]+)@`, "")
	reVlessB64User = js.MustRegExp(`^[A-Za-z0-9+/=_-]+$`, "")
)

// extractSSMethod 对应 parseNodeList 中提取 SS 加密方式的逻辑。
func extractSSMethod(fixedURL string) string {
	body := fixedURL[5:]
	if h := strings.Index(body, "#"); h != -1 {
		body = body[:h]
	}
	method := ""
	atIndex := strings.LastIndex(body, "@")
	if atIndex != -1 && !strings.Contains(body, "://") {
		userInfo := body[:atIndex]
		if strings.Contains(userInfo, ":") {
			return strings.Split(userInfo, ":")[0]
		}
		if d, err := js.Atob(userInfo); err == nil && strings.Contains(d, ":") {
			method = strings.Split(d, ":")[0]
		}
		return method
	}
	if decoded, err := js.Atob(body); err == nil {
		if strings.Contains(decoded, "@") {
			userInfo := strings.Split(decoded, "@")[0]
			if strings.Contains(userInfo, ":") {
				method = strings.Split(userInfo, ":")[0]
			}
		}
		return method
	}
	if at := strings.LastIndex(body, "@"); at != -1 {
		if d, err := js.Atob(body[:at]); err == nil && strings.Contains(d, ":") {
			method = strings.Split(d, ":")[0]
		}
	}
	return method
}

// ParseNodeListOptions 对应 parseNodeList 的 options。
type ParseNodeListOptions = FixOptions

// ParseNodeList 对应 parseNodeList（预览与计数使用），返回节点对象数组。
func ParseNodeList(content any, options FixOptions) []*js.Object {
	out := []*js.Object{}
	for _, nodeURL := range ExtractValidNodes(content) {
		fixedURL := FixNodeURLEncoding(nodeURL, options)
		var ss2022Warning *js.Object
		if strings.HasPrefix(fixedURL, "ss://") {
			method := extractSSMethod(fixedURL)
			if method != "" && !supportedSSCiphers[js.ToLower(method)] {
				continue
			}
			validation := ValidateSS2022Node(fixedURL)
			details := js.AsObject(validation.Get("details"))
			if !js.Truthy(validation.Get("valid")) && js.Truthy(details.Get("suggestedCipher")) {
				fix := FixSS2022Node(fixedURL)
				if js.Truthy(fix.Get("fixed")) {
					fixedURL = js.ToString(fix.Get("fixedUrl"))
					changes := js.AsObject(fix.Get("changes"))
					ss2022Warning = js.Obj("type", "ss2022_auto_fixed",
						"message", "已自动修复: "+js.ToString(changes.Get("from"))+" → "+js.ToString(changes.Get("to")),
						"originalCipher", changes.Get("from"), "fixedCipher", changes.Get("to"))
				} else {
					ss2022Warning = js.Obj("type", "ss2022_invalid", "message", validation.Get("error"), "details", validation.Get("details"))
				}
			} else if js.Truthy(validation.Get("warning")) {
				ss2022Warning = js.Obj("type", "ss2022_warning", "message", validation.Get("warning"))
			}
		}
		info := ParseNodeInfo(fixedURL)
		valid := true
		if info.Protocol == "vless" {
			id := ""
			if m := reVlessStdUUID.MatchFirst(fixedURL); m != nil {
				id = m.Str(1)
			} else if m := reVlessAnyUser.MatchFirst(fixedURL); m != nil {
				userPart := m.Str(1)
				if js.UTF16Len(userPart) > 40 && reVlessB64User.Test(userPart) {
					if d, err := js.Atob(padBase64(urlSafeToStd(userPart))); err == nil {
						id = d
					} else {
						id = userPart
					}
				} else {
					id = userPart
				}
			}
			if id != "" && strings.HasPrefix(id, "auto:") {
				valid = false
			}
		} else if info.Protocol == "vmess" && strings.HasPrefix(fixedURL, "vmess://") {
			if s, err := js.Atob(padBase64(urlSafeToStd(fixedURL[8:]))); err == nil {
				if cfg, err := js.JSONParse(s); err == nil {
					if id := objGet(cfg, "id"); !js.IsNullish(cfg) && js.Truthy(id) && strings.HasPrefix(js.ToString(id), "auto:") {
						valid = false
					}
				}
			}
		}
		if !valid {
			continue
		}
		result := js.Obj("url", fixedURL)
		result.Assign(info.Object())
		result.Set("metadata", ExtractNodeMetadata(info.Name))
		if ss2022Warning != nil {
			result.Set("warning", ss2022Warning)
		}
		out = append(out, result)
	}
	return out
}

// CalculateProtocolStats 对应 calculateProtocolStats。
func CalculateProtocolStats(nodes []*js.Object) *js.Object {
	stats := js.NewObject()
	total := float64(len(nodes))
	for _, node := range nodes {
		protocol := js.ToString(js.Or(node.Get("protocol"), "unknown"))
		stats.Set(protocol, js.ToNumber(js.Or(stats.Get(protocol), 0.0))+1)
	}
	for _, kv := range stats.Entries() {
		count := kv.Value.(float64)
		stats.Set(kv.Key, js.Obj("count", count, "percentage", js.MathRound(count/total*100)))
	}
	return stats
}

// RemoveDuplicateNodes 对应 removeDuplicateNodes（按 url 去重）。
func RemoveDuplicateNodes(nodes []*js.Object) []*js.Object {
	seen := map[string]bool{}
	out := []*js.Object{}
	for _, n := range nodes {
		u := js.ToString(js.Or(n.Get("url"), ""))
		if seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, n)
	}
	return out
}

// FormatNodeCount 对应 formatNodeCount。
func FormatNodeCount(count any) string {
	f, ok := count.(float64)
	if !ok || f < 0 {
		return "0 个节点"
	}
	return js.NumberToString(f) + " 个节点"
}

var reSnellPathPSK = js.MustRegExp(`^\/\/([^@]+)@`, "")

// ParseSnellURL 对应 node-parser.js 的 parseSnellUrl（基于 WHATWG URL）。
func ParseSnellURL(raw string) *js.Object {
	u, err := js.ParseURL(raw)
	if err != nil {
		return nil
	}
	psk := ""
	if u.Username != "" {
		d, err := js.DecodeURIComponent(u.Username)
		if err != nil {
			return nil
		}
		psk = d
	} else if m := reSnellPathPSK.MatchFirst(u.Pathname()); m != nil {
		d, err := js.DecodeURIComponent(m.Str(1))
		if err != nil {
			return nil
		}
		psk = d
	}
	server := u.Hostname()
	port := js.ParseInt(u.PortString(), 0)
	name := "Snell"
	if h := u.Hash(); h != "" {
		d, err := js.DecodeURIComponent(h[1:])
		if err != nil {
			return nil
		}
		name = d
	}
	if psk == "" || server == "" || !js.Truthy(port) {
		return nil
	}
	proxy := js.Obj("type", "snell", "name", name, "server", server, "port", port, "psk", psk)
	sp := u.SearchParams()
	if v := sp.Str("version"); v != "" {
		proxy.Set("version", js.ParseInt(v, 0))
	}
	if v, ok := sp.Get("reuse"); ok {
		proxy.Set("reuse", v == "true")
	}
	if v, ok := sp.Get("tfo"); ok {
		proxy.Set("tfo", v == "true")
	}
	obfs, obfsHost := sp.Str("obfs"), sp.Str("obfs-host")
	if obfs != "" || obfsHost != "" {
		opts := js.NewObject()
		proxy.Set("obfs-opts", opts)
		if obfs != "" {
			opts.Set("mode", obfs)
		}
		if obfsHost != "" {
			d, err := js.DecodeURIComponent(obfsHost)
			if err != nil {
				return nil
			}
			opts.Set("host", d)
		}
	}
	return proxy
}

var reSnellPort = js.MustRegExp(`:(\d+)(?:[/?#]|$)`, "")

// ValidateSnellNode 对应 validateSnellNode。
func ValidateSnellNode(raw any) *js.Object {
	u, ok := raw.(string)
	if !ok || u == "" {
		return js.Obj("valid", false, "error", "无效的 URL 格式")
	}
	if !strings.HasPrefix(u, "snell://") {
		return js.Obj("valid", false, "error", "不是 Snell 协议 URL")
	}
	if m := reSnellPort.MatchFirst(u); m != nil {
		portNumber := js.ParseInt(m.Str(1), 0)
		if portNumber < 1 || portNumber > 65535 {
			return js.Obj("valid", false, "error", "端口号无效: "+js.NumberToString(portNumber)+" (范围: 1-65535)",
				"details", js.Obj("port", portNumber))
		}
	}
	proxy := ParseSnellURL(u)
	if proxy == nil {
		return js.Obj("valid", false, "error", "无效的 Snell URL 格式")
	}
	if v := proxy.Get("version"); js.Truthy(v) {
		f := js.ToNumber(v)
		if f < 1 || f > 5 {
			return js.Obj("valid", false, "error", "Snell 版本号无效: "+js.ToString(v)+" (支持 1-5)", "details", js.Obj("version", v))
		}
	}
	if mode := objGet(proxy.Get("obfs-opts"), "mode"); js.Truthy(mode) {
		if m := js.ToString(mode); m != "http" && m != "tls" {
			return js.Obj("valid", false, "error", "不支持的混淆模式: "+m+" (支持: http, tls)", "details", js.Obj("obfsMode", mode))
		}
	}
	if p := js.ToNumber(proxy.Get("port")); p < 1 || p > 65535 {
		return js.Obj("valid", false, "error", "端口号无效: "+js.ToString(proxy.Get("port"))+" (范围: 1-65535)",
			"details", js.Obj("port", proxy.Get("port")))
	}
	return js.Obj("valid", true, "proxy", proxy)
}
