package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/builtin-loon-generator.js

const qureIconRepo = "https://raw.githubusercontent.com/Koolson/Qure/master/IconSet/Color"

func loonQuote(v any) string {
	if js.IsNullish(v) {
		return ""
	}
	str := s(v)
	if reSurgeQuote.Test(str) {
		return `"` + reDoubleQuote.Replace(str, `\"`) + `"`
	}
	return str
}

// joinPart 对应数组元素在 join 时的转换（undefined / null 变为空串）。
func joinPart(v any) string {
	if js.IsNullish(v) {
		return ""
	}
	return s(v)
}

var nodeIconRules = []struct {
	re   *js.RegExp
	icon string
}{
	{js.MustRegExp(`港|HK|Hong Kong`, "i"), "Hong_Kong"},
	{js.MustRegExp(`台|TW|Taiwan`, "i"), "Taiwan"},
	{js.MustRegExp(`日|JP|Japan`, "i"), "Japan"},
	{js.MustRegExp(`新|SG|Singapore|狮城`, "i"), "Singapore"},
	{js.MustRegExp(`美|US|America`, "i"), "United_States"},
	{js.MustRegExp(`韩|KR|Korea`, "i"), "South_Korea"},
	{js.MustRegExp(`英|UK|Great Britain`, "i"), "United_Kingdom"},
	{js.MustRegExp(`德|DE|Germany`, "i"), "Germany"},
	{js.MustRegExp(`法|FR|France`, "i"), "France"},
	{js.MustRegExp(`俄|RU|Russia`, "i"), "Russia"},
}

func getIconByNodeName(name any) string {
	n := s(name)
	for _, r := range nodeIconRules {
		if r.re.Test(n) {
			return qureIconRepo + "/" + r.icon + ".png"
		}
	}
	return ""
}

func appendLoonTLSParams(parts []string, proxy *js.Object) []string {
	if sni := js.Or(proxy.Get("sni"), proxy.Get("servername")); js.Truthy(sni) {
		parts = append(parts, "sni="+loonQuote(sni))
	}
	if skipCertVerify(proxy) {
		parts = append(parts, "skip-cert-verify=true")
	}
	if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
		if a, ok := alpn.(*js.Array); ok {
			alpn = a.At(0)
		}
		parts = append(parts, "alpn="+loonQuote(alpn))
	}
	if fp := proxy.Get("client-fingerprint"); js.Truthy(fp) {
		parts = append(parts, "client-fingerprint="+loonQuote(fp))
	}
	return parts
}

func clashProxyToLoonLine(proxy *js.Object) string {
	if proxy == nil || !js.Truthy(proxy.Get("server")) || !js.Truthy(proxy.Get("port")) {
		return ""
	}
	name := sanitizeSurgeNodeName(proxy.Get("name"))
	typ := js.ToLower(s(js.Or(proxy.Get("type"), "")))
	server, port := s(proxy.Get("server")), s(proxy.Get("port"))
	var parts []string
	wsParts := func() {
		ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
		if v := objGet(ws, "path"); js.Truthy(v) {
			parts = append(parts, "path="+loonQuote(v))
		}
		if h := objGet(objGet(ws, "headers"), "Host"); js.Truthy(h) {
			parts = append(parts, "host="+loonQuote(h))
		}
	}
	switch typ {
	case "ss", "shadowsocks":
		parts = append(parts, name+" = Shadowsocks", server, port, s(js.Or(proxy.Get("cipher"), "aes-128-gcm")), loonQuote(js.Or(proxy.Get("password"), "")))
		if js.Truthy(proxy.Get("udp")) {
			parts = append(parts, "udp-relay=true")
		}
		plugin := js.Or(proxy.Get("plugin"), "")
		opts := js.Or(proxy.Get("plugin-opts"), proxy.Get("pluginOpts"), js.NewObject())
		if js.StrictEquals(plugin, "obfs-local") || js.Truthy(proxy.Get("obfs")) {
			parts = append(parts, "obfs-name="+s(js.Or(proxy.Get("obfs"), objGet(opts, "mode"))))
			if h := js.Or(proxy.Get("obfs-host"), objGet(opts, "host")); js.Truthy(h) {
				parts = append(parts, "obfs-host="+loonQuote(h))
			}
			if u := js.Or(proxy.Get("obfs-uri"), objGet(opts, "uri")); js.Truthy(u) {
				parts = append(parts, "obfs-uri="+loonQuote(u))
			}
		} else if js.StrictEquals(plugin, "v2ray-plugin") || js.StrictEquals(objGet(opts, "mode"), "websocket") {
			parts = append(parts, "transport=ws")
			if v := objGet(opts, "path"); js.Truthy(v) {
				parts = append(parts, "path="+loonQuote(v))
			}
			if v := objGet(opts, "host"); js.Truthy(v) {
				parts = append(parts, "host="+loonQuote(v))
			}
			if js.Truthy(objGet(opts, "tls")) || js.StrictEquals(objGet(opts, "mode"), "websocket-tls") {
				parts = append(parts, "over-tls=true")
			}
		}
	case "vmess":
		parts = append(parts, name+" = vmess", server, port, s(js.Or(proxy.Get("cipher"), "auto")), `"`+s(js.Or(proxy.Get("uuid"), ""))+`"`, "alterId=0")
		if js.Truthy(proxy.Get("tls")) {
			parts = append(parts, "over-tls=true")
		}
		if js.StrictEquals(proxy.Get("network"), "ws") {
			parts = append(parts, "transport=ws")
			wsParts()
		}
		parts = appendLoonTLSParams(parts, proxy)
	case "vless":
		parts = append(parts, name+" = vless", server, port, `"`+s(js.Or(proxy.Get("uuid"), ""))+`"`)
		if v := proxy.Get("flow"); js.Truthy(v) {
			parts = append(parts, "flow="+loonQuote(v))
		}
		if network := proxy.Get("network"); js.Truthy(network) {
			parts = append(parts, "transport="+s(network))
			switch {
			case js.StrictEquals(network, "ws"):
				wsParts()
			case js.StrictEquals(network, "grpc"):
				g := js.Or(proxy.Get("grpc-opts"), proxy.Get("grpcOpts"))
				if v := objGet(g, "grpc-service-name"); js.Truthy(v) {
					parts = append(parts, "grpc-service-name="+loonQuote(v))
				}
			case js.StrictEquals(network, "xhttp"):
				x := js.Or(proxy.Get("xhttp-opts"), proxy.Get("xhttpOpts"))
				for _, k := range []string{"path", "host", "mode"} {
					if v := objGet(x, k); js.Truthy(v) {
						parts = append(parts, k+"="+loonQuote(v))
					}
				}
			}
		} else {
			parts = append(parts, "transport=tcp")
		}
		if js.Truthy(proxy.Get("tls")) || js.StrictEquals(proxy.Get("security"), "reality") {
			parts = append(parts, "over-tls=true")
			if ro := js.Or(proxy.Get("reality-opts"), proxy.Get("realityOpts")); js.Truthy(ro) {
				parts = append(parts, "reality=true")
				if v := objGet(ro, "public-key"); js.Truthy(v) {
					parts = append(parts, "public-key="+loonQuote(v))
				}
				if v := objGet(ro, "short-id"); js.Truthy(v) {
					parts = append(parts, "short-id="+loonQuote(v))
				}
			}
		}
		parts = appendLoonTLSParams(parts, proxy)
	case "trojan":
		parts = append(parts, name+" = trojan", server, port, loonQuote(js.Or(proxy.Get("password"), "")))
		if js.StrictEquals(proxy.Get("network"), "ws") {
			parts = append(parts, "transport=ws")
			wsParts()
		}
		parts = appendLoonTLSParams(parts, proxy)
	case "hysteria2", "hy2":
		parts = append(parts, name+" = hysteria2", server, port, loonQuote(js.Or(proxy.Get("password"), "")))
		parts = appendLoonTLSParams(parts, proxy)
	case "tuic":
		parts = append(parts, name+" = tuic", server, port, loonQuote(js.Or(proxy.Get("token"), proxy.Get("uuid"), "")))
		if v := proxy.Get("password"); js.Truthy(v) {
			parts = append(parts, "password="+loonQuote(v))
		}
		parts = appendLoonTLSParams(parts, proxy)
	case "wireguard":
		parts = append(parts, name+" = wireguard", server, port, joinPart(proxy.Get("private-key")))
		if ip := proxy.Get("ip"); js.Truthy(ip) {
			if a, ok := ip.(*js.Array); ok {
				ip = a.At(0)
			}
			parts = append(parts, "self-ip="+s(ip))
		}
		if v := proxy.Get("public-key"); js.Truthy(v) {
			parts = append(parts, "public-key="+s(v))
		}
		if v := proxy.Get("mtu"); js.Truthy(v) {
			parts = append(parts, "mtu="+s(v))
		}
		if v := proxy.Get("reserved"); js.Truthy(v) {
			parts = append(parts, "client-id="+s(joinWith(v, "/")))
		}
	case "anytls":
		parts = append(parts, name+" = anytls", server, port, loonQuote(js.Or(proxy.Get("password"), "")))
		parts = appendLoonTLSParams(parts, proxy)
	case "snell":
		parts = append(parts, name+" = snell", server, port)
		if psk := js.Or(proxy.Get("psk"), proxy.Get("password")); js.Truthy(psk) {
			parts = append(parts, "psk="+loonQuote(psk))
		}
		if v := proxy.Get("version"); js.Truthy(v) {
			parts = append(parts, "version="+s(v))
		}
		if v := proxy.Get("reuse"); v != nil {
			parts = append(parts, "reuse="+s(v))
		}
		if v := proxy.Get("tfo"); v != nil {
			parts = append(parts, "tfo="+s(v))
		}
		if oo := proxy.Get("obfs-opts"); js.Truthy(oo) {
			if m := objGet(oo, "mode"); js.Truthy(m) && !js.StrictEquals(m, "none") {
				parts = append(parts, "obfs="+loonQuote(m))
			}
			if h := objGet(oo, "host"); js.Truthy(h) {
				parts = append(parts, "obfs-host="+loonQuote(h))
			}
		}
	default:
		return ""
	}
	if js.Truthy(proxy.Get("tfo")) && typ != "snell" {
		has := false
		for _, p := range parts {
			if strings.HasPrefix(p, "fast-open=") || strings.HasPrefix(p, "tfo=") {
				has = true
				break
			}
		}
		if !has {
			parts = append(parts, "fast-open=true")
		}
	}
	if icon := getIconByNodeName(proxy.Get("name")); icon != "" {
		parts = append(parts, "icon="+icon)
	}
	return strings.Join(parts, ", ")
}

var loonGroupIcons = map[string]string{
	DefaultSelectGroup: qureIconRepo + "/Proxy.png",
	DefaultRelayGroup:  qureIconRepo + "/Proxy.png",
	"♻️ 自动选择":          qureIconRepo + "/Speedtest.png",
	"🔯 故障转移":           qureIconRepo + "/Relay.png",
	"🎬 视频广告":           qureIconRepo + "/Reject.png",
	"🎥 流媒体":            qureIconRepo + "/Video.png",
	"🍎 Apple":          qureIconRepo + "/Apple.png",
	"Ⓜ️ Microsoft":     qureIconRepo + "/Microsoft.png",
	"📲 Telegram":       qureIconRepo + "/Telegram.png",
	"🎧 Spotify":        qureIconRepo + "/Spotify.png",
	"🇭🇰 香港节点":          qureIconRepo + "/Hong_Kong.png",
	"🇹🇼 台湾节点":          qureIconRepo + "/Taiwan.png",
	"🇯🇵 日本节点":          qureIconRepo + "/Japan.png",
	"🇸🇬 狮城节点":          qureIconRepo + "/Singapore.png",
	"🇺🇸 美国节点":          qureIconRepo + "/United_States.png",
	"🇰🇷 韩国节点":          qureIconRepo + "/South_Korea.png",
	"🇬🇧 英国节点":          qureIconRepo + "/United_Kingdom.png",
}

// relayGroupsForINI 处理 RELAY 等级下 Surge / Loon 的链式代理组（relay 类型 + 补齐“落地节点”）。
func relayGroupsForINI(groups []*js.Object, proxyNames []any) []*js.Object {
	for i, g := range groups {
		if js.StrictEquals(g.Get("name"), "🔗 链式代理") {
			c := g.Clone()
			c.Set("type", "relay")
			c.Set("proxies", strs2("入口节点", "落地节点"))
			groups[i] = c
		}
	}
	entryIdx := -1
	for i, g := range groups {
		if js.StrictEquals(g.Get("name"), "落地节点") {
			return groups
		}
		if entryIdx == -1 && js.StrictEquals(g.Get("name"), "入口节点") {
			entryIdx = i
		}
	}
	members := append([]any{"♻️ 自动选择", "👋 手动切换", "DIRECT"}, proxyNames...)
	landing := js.Obj("name", "落地节点", "type", "select", "proxies", js.Arr(members...))
	at := entryIdx + 1
	return append(groups[:at:at], append([]*js.Object{landing}, groups[at:]...)...)
}

// GenerateBuiltinLoonConfig 对应 generateBuiltinLoonConfig（输出 Loon INI）。
func GenerateBuiltinLoonConfig(nodeList string, options *js.Object) string {
	if options == nil {
		options = js.NewObject()
	}
	managed := options.Get("managedConfigUrl")
	if managed == nil {
		managed = ""
	}
	interval := options.Get("interval")
	if interval == nil {
		interval = 86400.0
	}
	ruleLevel := options.Get("ruleLevel")
	if ruleLevel == nil {
		ruleLevel = "std"
	}
	var urls []string
	for _, l := range strings.Split(cleanControlChars(nodeList), "\n") {
		if l = js.Trim(l); l != "" && !strings.HasPrefix(l, "#") {
			urls = append(urls, l)
		}
	}
	var lines []string
	var withMeta []*js.Object
	namer := NewUniqueNamer()
	for _, p := range URLsToClashProxies(urls, options) {
		unique := namer.GetUniqueName(sanitizeSurgeNodeName(p.Get("name")))
		p.Set("name", unique)
		if line := clashProxyToLoonLine(p); line != "" {
			lines = append(lines, line)
			withMeta = append(withMeta, p)
		}
	}
	if len(lines) == 0 {
		return "[Proxy]\nDIRECT = direct\n"
	}
	var sections []string
	if js.Truthy(managed) {
		sections = append(sections, "#!MANAGED-CONFIG "+s(managed)+" interval="+s(interval)+" strict=false")
	}
	sections = append(sections, "[General]\nipv6 = false\ndns-server = system, 223.5.5.5, 119.29.29.29\nskip-proxy = 127.0.0.1, 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, 100.64.0.0/10, localhost, *.local\nproxy-test-url = http://www.gstatic.com/generate_204\nresource-parser = https://raw.githubusercontent.com/sub-store-org/Sub-Store/master/scripts/sub-store-parser.js")
	sections = append(sections, "[Proxy]\nDIRECT = direct\n"+strings.Join(lines, "\n"))
	levelKey := js.ToUpper(s(js.Or(ruleLevel, "std")))
	factoryLevel := levelKey
	switch levelKey {
	case "BASE", "STD", "FULL", "RELAY":
	default:
		factoryLevel = "STD"
	}
	groups := asObjects(PolicyGroups(factoryLevel, withMeta, options))
	if levelKey == "RELAY" {
		var names []any
		for _, p := range withMeta {
			names = append(names, p.Get("name"))
		}
		groups = relayGroupsForINI(groups, names)
	}
	groups = PruneProxyGroups(groups, withMeta)
	var groupLines []string
	for _, g := range groups {
		typ := "select"
		switch {
		case js.StrictEquals(g.Get("type"), "url-test"):
			typ = "url-test"
		case js.StrictEquals(g.Get("type"), "fallback"):
			typ = "fallback"
		case js.StrictEquals(g.Get("type"), "relay"):
			typ = "relay"
		}
		icon := ""
		if ic, ok := loonGroupIcons[s(g.Get("name"))]; ok {
			icon = ", icon=" + ic
		}
		extra := ""
		if typ == "url-test" || typ == "fallback" {
			extra = ", url=http://www.gstatic.com/generate_204, interval=300, tolerance=50"
		}
		groupLines = append(groupLines, s(g.Get("name"))+" = "+typ+", "+arrayOrEmpty(g.Get("proxies")).Join(", ")+extra+icon)
	}
	sections = append(sections, "[Proxy Group]\n"+strings.Join(groupLines, "\n"))
	final := DefaultSelectGroup
	if levelKey == "RELAY" {
		final = DefaultRelayGroup
	}
	ruleLines := []string{"# 基础分流", "IP-CIDR,127.0.0.0/8,DIRECT", "IP-CIDR,10.0.0.0/8,DIRECT", "IP-CIDR,172.16.0.0/12,DIRECT", "IP-CIDR,192.168.0.0/16,DIRECT"}
	for _, r := range GetBuiltinRules(levelKey, "loon") {
		if js.Truthy(r) {
			ruleLines = append(ruleLines, s(r))
		}
	}
	ruleLines = append(ruleLines, "FINAL,"+final)
	sections = append(sections, "[Rule]\n"+strings.Join(ruleLines, "\n"))
	return strings.Join(sections, "\n\n") + "\n"
}
