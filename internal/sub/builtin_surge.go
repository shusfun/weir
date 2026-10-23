package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/builtin-surge-generator.js（Surge 不支持 VLESS，此类节点被跳过）。

var (
	reCommaAll     = js.MustRegExp(`,`, "g")
	reEqualAll     = js.MustRegExp(`=`, "g")
	reSurgeQuote   = js.MustRegExp(`[,\s"=]`, "")
	reDoubleQuote  = js.MustRegExp(`"`, "g")
	reWGNameUnsafe = js.MustRegExp(`[^a-zA-Z0-9_一-龥]`, "g")
	reRegExpChars  = js.MustRegExp(`[.*+?^${}()|[\]\\]`, "g")
)

func sanitizeSurgeNodeName(name any) string {
	if !js.Truthy(name) {
		return "Untitled"
	}
	safe := cleanControlChars(s(name))
	safe = reEqualAll.Replace(reCommaAll.Replace(safe, " "), "-")
	safe = js.Trim(reWhitespaceRun.Replace(safe, " "))
	if safe == "" {
		return "Untitled"
	}
	return safe
}

func surgeQuote(v any) string {
	if !js.Truthy(v) {
		return ""
	}
	str := s(v)
	if reSurgeQuote.Test(str) {
		return `"` + reDoubleQuote.Replace(str, `\"`) + `"`
	}
	return str
}

func appendSurgeTLSParams(parts []string, proxy *js.Object) []string {
	if sni := js.Or(proxy.Get("sni"), proxy.Get("servername")); js.Truthy(sni) {
		parts = append(parts, "sni="+s(sni))
	}
	if skipCertVerify(proxy) {
		parts = append(parts, "skip-cert-verify=true")
	}
	if v := proxy.Get("server-cert-fingerprint-sha256"); js.Truthy(v) {
		parts = append(parts, "server-cert-fingerprint-sha256="+s(v))
	}
	return parts
}

type surgeResult struct {
	proxyLine        string
	wireguardSection string
	clashProxy       *js.Object
}

func surgeWireGuardResult(name string, proxy *js.Object) *surgeResult {
	section := "WG_" + reWGNameUnsafe.Replace(name, "_")
	lines := []string{"[WireGuard " + section + "]"}
	if v := proxy.Get("private-key"); js.Truthy(v) {
		lines = append(lines, "private-key = "+s(v))
	}
	if ip := proxy.Get("ip"); js.Truthy(ip) {
		ipv4 := ip
		if a, ok := ip.(*js.Array); ok {
			ipv4 = findInArray(a, func(v any) bool { return !strings.Contains(s(v), ":") })
		}
		if js.Truthy(ipv4) {
			lines = append(lines, "self-ip = "+s(ipv4))
		}
	}
	if v6 := proxy.Get("ipv6"); js.Truthy(v6) {
		ipv6 := v6
		if a, ok := v6.(*js.Array); ok {
			ipv6 = findInArray(a, js.Truthy)
		}
		if js.Truthy(ipv6) {
			lines = append(lines, "self-ip-v6 = "+s(ipv6))
		}
	}
	if d := proxy.Get("dns"); js.Truthy(d) {
		lines = append(lines, "dns-server = "+s(joinWith(d, ", ")))
	} else {
		lines = append(lines, "dns-server = 8.8.8.8, 1.1.1.1")
	}
	if v := proxy.Get("mtu"); js.Truthy(v) {
		lines = append(lines, "mtu = "+s(v))
	}
	var peer []string
	if v := proxy.Get("public-key"); js.Truthy(v) {
		peer = append(peer, "public-key = "+s(v))
	}
	allowed := "0.0.0.0/0, ::/0"
	if a := proxy.Get("allowed-ips"); js.Truthy(a) {
		allowed = s(joinWith(a, ", "))
	}
	peer = append(peer, `allowed-ips = "`+allowed+`"`)
	peer = append(peer, "endpoint = "+s(proxy.Get("server"))+":"+s(proxy.Get("port")))
	if v := proxy.Get("preshared-key"); js.Truthy(v) {
		peer = append(peer, "preshared-key = "+s(v))
	}
	if v := proxy.Get("persistent-keepalive"); js.Truthy(v) {
		peer = append(peer, "keepalive = "+s(v))
	}
	if v := proxy.Get("reserved"); js.Truthy(v) {
		peer = append(peer, "client-id = "+s(joinWith(v, "/")))
	}
	lines = append(lines, "peer = ("+strings.Join(peer, ", ")+")")
	return &surgeResult{proxyLine: name + " = wireguard, section-name = " + section, wireguardSection: strings.Join(lines, "\n")}
}

// joinWith 对应 `Array.isArray(v) ? v.join(sep) : v`。
func joinWith(v any, sep string) any {
	if a, ok := v.(*js.Array); ok {
		return a.Join(sep)
	}
	return v
}

func clashProxyToSurgeResult(proxy *js.Object) *surgeResult {
	if proxy == nil || !js.Truthy(proxy.Get("server")) || !js.Truthy(proxy.Get("port")) {
		return nil
	}
	name := sanitizeSurgeNodeName(proxy.Get("name"))
	typ := js.ToLower(s(js.Or(proxy.Get("type"), "")))
	if typ == "vless" {
		return nil
	}
	server, port := s(proxy.Get("server")), s(proxy.Get("port"))
	var parts []string
	wsParts := func() {
		parts = append(parts, "ws=true")
		ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
		if v := objGet(ws, "path"); js.Truthy(v) {
			parts = append(parts, "ws-path="+s(v))
		}
		if h := objGet(objGet(ws, "headers"), "Host"); js.Truthy(h) {
			parts = append(parts, "ws-headers=Host:"+s(h))
		}
	}
	switch typ {
	case "ss", "shadowsocks":
		parts = append(parts, name+" = ss", server, port, "encrypt-method="+s(js.Or(proxy.Get("cipher"), "aes-128-gcm")),
			"password="+surgeQuote(js.Or(proxy.Get("password"), "")))
		plugin := js.Or(proxy.Get("plugin"), "")
		opts := js.Or(proxy.Get("plugin-opts"), proxy.Get("pluginOpts"), js.NewObject())
		mode := objGet(opts, "mode")
		if js.StrictEquals(plugin, "obfs-local") || js.StrictEquals(mode, "http") || js.StrictEquals(mode, "tls") || js.Truthy(proxy.Get("obfs")) {
			if m := js.Or(mode, proxy.Get("obfs")); js.Truthy(m) {
				parts = append(parts, "obfs="+s(m))
			}
			if h := js.Or(objGet(opts, "host"), proxy.Get("obfs-host")); js.Truthy(h) {
				parts = append(parts, "obfs-host="+s(h))
			}
			if u := js.Or(objGet(opts, "uri"), proxy.Get("obfs-uri")); js.Truthy(u) {
				parts = append(parts, "obfs-uri="+s(u))
			}
		} else if js.StrictEquals(plugin, "v2ray-plugin") || js.StrictEquals(mode, "websocket") {
			if js.StrictEquals(mode, "websocket") || js.StrictEquals(mode, "websocket-tls") {
				parts = append(parts, "ws=true")
				if v := objGet(opts, "path"); js.Truthy(v) {
					parts = append(parts, "ws-path="+surgeQuote(v))
				}
				if v := objGet(opts, "host"); js.Truthy(v) {
					parts = append(parts, "ws-headers=Host:"+s(v))
				}
				if js.Truthy(objGet(opts, "tls")) || js.StrictEquals(mode, "websocket-tls") {
					parts = append(parts, "tls=true")
				}
			}
		}
		if v := proxy.Get("udp-port"); js.Truthy(v) {
			parts = append(parts, "udp-port="+s(v))
		}
		if js.Truthy(proxy.Get("udp")) {
			parts = append(parts, "udp-relay=true")
		}
	case "vmess":
		parts = append(parts, name+" = vmess", server, port, "username="+s(js.Or(proxy.Get("uuid"), "")))
		if c := proxy.Get("cipher"); js.Truthy(c) && !js.StrictEquals(c, "auto") {
			parts = append(parts, "encrypt-method="+s(c))
		}
		if js.Truthy(proxy.Get("tls")) {
			parts = append(parts, "tls=true")
		}
		if js.StrictEquals(proxy.Get("network"), "ws") {
			wsParts()
		}
		if aid := proxy.Get("alterId"); js.StrictEquals(aid, 0.0) || js.IsNullish(aid) {
			parts = append(parts, "vmess-aead=true")
		}
		parts = appendSurgeTLSParams(parts, proxy)
	case "trojan":
		parts = append(parts, name+" = trojan", server, port, "password="+surgeQuote(js.Or(proxy.Get("password"), "")))
		if js.StrictEquals(proxy.Get("network"), "ws") {
			wsParts()
		}
		parts = appendSurgeTLSParams(parts, proxy)
	case "hysteria2", "hy2":
		parts = append(parts, name+" = hysteria2", server, port, "password="+surgeQuote(js.Or(proxy.Get("password"), "")))
		if d := js.Or(proxy.Get("down"), proxy.Get("download-bandwidth")); js.Truthy(d) {
			parts = append(parts, "download-bandwidth="+s(d))
		}
		if v := proxy.Get("port-hopping"); js.Truthy(v) {
			parts = append(parts, "port-hopping="+s(v))
		}
		if v := proxy.Get("port-hopping-interval"); js.Truthy(v) {
			parts = append(parts, "port-hopping-interval="+s(v))
		}
		parts = appendSurgeTLSParams(parts, proxy)
	case "tuic":
		parts = append(parts, name+" = tuic", server, port)
		token := proxy.Get("token")
		if !js.Truthy(token) && js.Truthy(proxy.Get("uuid")) {
			if js.Truthy(proxy.Get("password")) {
				token = s(proxy.Get("uuid")) + ":" + s(proxy.Get("password"))
			} else {
				token = proxy.Get("uuid")
			}
		}
		if !js.Truthy(token) {
			token = js.Or(proxy.Get("password"), "")
		}
		parts = append(parts, "token="+surgeQuote(token))
		if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
			if a, ok := alpn.(*js.Array); ok {
				alpn = a.At(0)
			}
			parts = append(parts, "alpn="+s(alpn))
		}
		if v := proxy.Get("port-hopping"); js.Truthy(v) {
			parts = append(parts, "port-hopping="+s(v))
		}
		if v := proxy.Get("port-hopping-interval"); js.Truthy(v) {
			parts = append(parts, "port-hopping-interval="+s(v))
		}
		parts = appendSurgeTLSParams(parts, proxy)
	case "snell":
		parts = append(parts, name+" = snell", server, port, "psk="+surgeQuote(js.Or(proxy.Get("psk"), proxy.Get("password"), "")))
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
				parts = append(parts, "obfs="+s(m))
			}
			if h := objGet(oo, "host"); js.Truthy(h) {
				parts = append(parts, "obfs-host="+s(h))
			}
			if u := objGet(oo, "uri"); js.Truthy(u) {
				parts = append(parts, "obfs-uri="+s(u))
			}
		}
	case "anytls":
		parts = append(parts, name+" = anytls", server, port, "password="+surgeQuote(js.Or(proxy.Get("password"), "")))
		if v := proxy.Get("reuse"); v != nil {
			parts = append(parts, "reuse="+s(v))
		}
		parts = appendSurgeTLSParams(parts, proxy)
	case "wireguard":
		return surgeWireGuardResult(name, proxy)
	case "http", "https":
		parts = append(parts, name+" = "+typ, server, port, s(js.Or(proxy.Get("username"), "")), s(js.Or(proxy.Get("password"), "")))
		if typ == "https" {
			parts = appendSurgeTLSParams(parts, proxy)
		}
	case "socks5", "socks5-tls":
		parts = append(parts, name+" = "+typ, server, port)
		if js.Truthy(proxy.Get("username")) || js.Truthy(proxy.Get("password")) {
			parts = append(parts, s(js.Or(proxy.Get("username"), "")), s(js.Or(proxy.Get("password"), "")))
		}
		if js.Truthy(proxy.Get("udp")) {
			parts = append(parts, "udp-relay=true")
		}
		if typ == "socks5-tls" {
			parts = appendSurgeTLSParams(parts, proxy)
		}
	default:
		return nil
	}
	if v := proxy.Get("dialer-proxy"); js.Truthy(v) {
		parts = append(parts, "underlying-proxy="+s(v))
	}
	if v := proxy.Get("shadow-tls-password"); js.Truthy(v) {
		parts = append(parts, "shadow-tls-password="+s(v))
		if sv := proxy.Get("shadow-tls-sni"); js.Truthy(sv) {
			parts = append(parts, "shadow-tls-sni="+s(sv))
		}
		if sv := proxy.Get("shadow-tls-version"); js.Truthy(sv) {
			parts = append(parts, "shadow-tls-version="+s(sv))
		}
	}
	if js.Truthy(proxy.Get("tfo")) && typ != "snell" {
		has := false
		for _, p := range parts {
			if strings.HasPrefix(p, "tfo=") {
				has = true
				break
			}
		}
		if !has {
			parts = append(parts, "tfo=true")
		}
	}
	return &surgeResult{proxyLine: strings.Join(parts, ", ")}
}

func escapeRegExp(str string) string { return reRegExpChars.Replace(str, `\$&`) }

// GenerateBuiltinSurgeConfig 对应 generateBuiltinSurgeConfig（输出 Surge INI）。
func GenerateBuiltinSurgeConfig(nodeList string, options *js.Object) string {
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
	var results []*surgeResult
	var names []string
	for _, p := range URLsToClashProxies(urls, options) {
		if r := clashProxyToSurgeResult(p); r != nil {
			r.clashProxy = p
			results = append(results, r)
			names = append(names, sanitizeSurgeNodeName(p.Get("name")))
		}
	}
	namer := NewUniqueNamer()
	var finalResults []*surgeResult
	var finalNames []string
	for i, r := range results {
		base := names[i]
		unique := namer.GetUniqueName(base)
		if unique != base {
			u := *r
			u.proxyLine = js.ReplaceString(r.proxyLine, base+" = ", unique+" = ")
			if u.wireguardSection != "" {
				re := js.MustRegExp("WG_"+escapeRegExp(reWGNameUnsafe.Replace(base, "_")), "g")
				u.wireguardSection = re.Replace(r.wireguardSection, "WG_"+reWGNameUnsafe.Replace(unique, "_"))
			}
			finalResults = append(finalResults, &u)
			finalNames = append(finalNames, unique)
		} else {
			finalResults = append(finalResults, r)
			finalNames = append(finalNames, base)
		}
	}
	if len(finalResults) == 0 {
		return "[General]\nloglevel = notify\n\n[Proxy]\nDIRECT = direct\n\n[Proxy Group]\n\n[Rule]\nFINAL,DIRECT\n"
	}
	var proxyLines, wgSections []string
	for _, r := range finalResults {
		proxyLines = append(proxyLines, r.proxyLine)
		if r.wireguardSection != "" {
			wgSections = append(wgSections, r.wireguardSection)
		}
	}
	managedLine := ""
	if js.Truthy(managed) {
		managedLine = "#!MANAGED-CONFIG " + s(managed) + " interval=" + s(interval) + " strict=false\n\n"
	}
	sections := []string{
		managedLine + "[General]\nloglevel = notify\nskip-proxy = 127.0.0.1, 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, 100.64.0.0/10, localhost, *.local\ndns-server = 119.29.29.29, 223.5.5.5, system",
		"[Proxy]\nDIRECT = direct\n" + strings.Join(proxyLines, "\n"),
	}
	levelKey := js.ToUpper(s(js.Or(ruleLevel, "std")))
	factoryLevel := levelKey
	switch levelKey {
	case "BASE", "STD", "FULL", "RELAY":
	default:
		factoryLevel = "STD"
	}
	grouping := make([]*js.Object, len(finalNames))
	for i, n := range finalNames {
		var meta any = js.NewObject()
		if cp := finalResults[i].clashProxy; cp != nil && js.Truthy(cp.Get("metadata")) {
			meta = cp.Get("metadata")
		}
		grouping[i] = js.Obj("tag", n, "name", n, "metadata", meta)
	}
	groups := asObjects(PolicyGroups(factoryLevel, grouping, options))
	if levelKey == "RELAY" {
		for i, g := range groups {
			if js.StrictEquals(g.Get("name"), "🔗 链式代理") {
				c := g.Clone()
				c.Set("type", "relay")
				c.Set("proxies", strs2("入口节点", "落地节点"))
				groups[i] = c
			}
		}
		has := false
		entryIdx := -1
		for i, g := range groups {
			if js.StrictEquals(g.Get("name"), "落地节点") {
				has = true
			}
			if entryIdx == -1 && js.StrictEquals(g.Get("name"), "入口节点") {
				entryIdx = i
			}
		}
		if !has {
			members := []any{"♻️ 自动选择", "👋 手动切换", "DIRECT"}
			for _, n := range finalNames {
				members = append(members, n)
			}
			landing := js.Obj("name", "落地节点", "type", "select", "proxies", js.Arr(members...))
			at := entryIdx + 1
			groups = append(groups[:at], append([]*js.Object{landing}, groups[at:]...)...)
		}
	}
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
		extra := ""
		if typ == "url-test" || typ == "fallback" {
			extra = ", url=http://www.gstatic.com/generate_204, interval=300, tolerance=50"
		}
		groupLines = append(groupLines, s(g.Get("name"))+" = "+typ+", "+arrayOrEmpty(g.Get("proxies")).Join(", ")+extra)
	}
	sections = append(sections, "[Proxy Group]\n"+strings.Join(groupLines, "\n"))
	final := DefaultSelectGroup
	if levelKey == "RELAY" {
		final = DefaultRelayGroup
	}
	ruleLines := []string{"# 基础分流", "DOMAIN-SUFFIX,localhost,DIRECT", "IP-CIDR,127.0.0.1/8,DIRECT", "IP-CIDR,10.0.0.0/8,DIRECT",
		"IP-CIDR,172.16.0.0/12,DIRECT", "IP-CIDR,192.168.0.0/16,DIRECT"}
	for _, r := range GetBuiltinRules(levelKey, "surge") {
		if js.Truthy(r) {
			ruleLines = append(ruleLines, s(r))
		}
	}
	ruleLines = append(ruleLines, "FINAL,"+final+",dns-failed")
	sections = append(sections, "[Rule]\n"+strings.Join(ruleLines, "\n"))
	if len(wgSections) > 0 {
		sections = append(sections, strings.Join(wgSections, "\n\n"))
	}
	return strings.Join(sections, "\n\n") + "\n"
}
