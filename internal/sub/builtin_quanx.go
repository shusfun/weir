package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/builtin-quanx-generator.js

var (
	reSemicolonAll = js.MustRegExp(`;`, "g")
	reQuotesAll    = js.MustRegExp(`["']`, "g")
	qxRuleRewrites = []struct {
		re  *js.RegExp
		rep string
	}{
		{js.MustRegExp(`^HOST-SUFFIX`, "i"), "host-suffix"},
		{js.MustRegExp(`^HOST-KEYWORD`, "i"), "host-keyword"},
		{js.MustRegExp(`^HOST,`, "i"), "host,"},
		{js.MustRegExp(`^IP-CIDR`, "i"), "ip-cidr"},
		{js.MustRegExp(`^GEOIP`, "i"), "geoip"},
		{js.MustRegExp(`^FINAL`, "i"), "final"},
		{js.MustRegExp(`,\s*DIRECT\b`, "g"), ", direct"},
		{js.MustRegExp(`,\s*REJECT\b`, "g"), ", reject"},
	}
	reFilterRemotePrefix = js.MustRegExp(`^filter_remote,\s*`, "i")
)

func sanitizeQxNodeName(name any) string {
	if !js.Truthy(name) {
		return "Untitled"
	}
	safe := cleanControlChars(s(name))
	safe = reCommaAll.Replace(safe, " ")
	safe = reEqualAll.Replace(safe, "-")
	safe = reSemicolonAll.Replace(safe, " ")
	safe = reQuotesAll.Replace(safe, "")
	safe = js.Trim(reWhitespaceRun.Replace(safe, " "))
	if safe == "" {
		return "Untitled"
	}
	return safe
}

func buildQxLine(proxy *js.Object) string {
	if proxy == nil || !js.Truthy(proxy.Get("server")) || !js.Truthy(proxy.Get("port")) {
		return ""
	}
	name := sanitizeQxNodeName(proxy.Get("name"))
	typ := js.ToLower(s(js.Or(proxy.Get("type"), "")))
	hp := s(proxy.Get("server")) + ":" + s(proxy.Get("port"))
	tag := ", tag=" + name
	tlsVerify := func(extras []string) []string {
		if skipCertVerify(proxy) {
			return append(extras, "tls-verification=false")
		}
		return extras
	}
	sni := js.Or(proxy.Get("sni"), proxy.Get("servername"))
	switch typ {
	case "ss", "shadowsocks":
		var extras []string
		plugin := js.Or(proxy.Get("plugin"), "")
		opts := js.Or(proxy.Get("plugin-opts"), proxy.Get("pluginOpts"), js.NewObject())
		if js.StrictEquals(plugin, "obfs-local") || js.Truthy(proxy.Get("obfs")) {
			extras = append(extras, "obfs="+s(js.Or(proxy.Get("obfs"), objGet(opts, "mode"))))
			if h := js.Or(proxy.Get("obfs-host"), objGet(opts, "host")); js.Truthy(h) {
				extras = append(extras, "obfs-host="+s(h))
			}
		} else if js.StrictEquals(plugin, "v2ray-plugin") || js.StrictEquals(objGet(opts, "mode"), "websocket") {
			if js.Truthy(objGet(opts, "tls")) || js.StrictEquals(objGet(opts, "mode"), "websocket-tls") {
				extras = append(extras, "obfs=wss")
			} else {
				extras = append(extras, "obfs=ws")
			}
			if v := objGet(opts, "path"); js.Truthy(v) {
				extras = append(extras, "obfs-uri="+s(v))
			}
			if v := objGet(opts, "host"); js.Truthy(v) {
				extras = append(extras, "obfs-host="+s(v))
			}
		}
		if js.Truthy(proxy.Get("udp")) {
			extras = append(extras, "udp-relay=true")
		}
		if js.Truthy(proxy.Get("tfo")) {
			extras = append(extras, "fast-open=true")
		}
		return withExtras("shadowsocks="+hp+", method="+s(js.Or(proxy.Get("cipher"), "aes-128-gcm"))+", password="+s(js.Or(proxy.Get("password"), "")), extras) + tag
	case "vmess":
		var extras []string
		hasTLS := js.Truthy(proxy.Get("tls")) || js.Truthy(sni)
		if js.StrictEquals(proxy.Get("network"), "ws") || js.Truthy(proxy.Get("ws-opts")) {
			if hasTLS {
				extras = append(extras, "obfs=wss")
			} else {
				extras = append(extras, "obfs=ws")
			}
			ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
			if v := objGet(ws, "path"); js.Truthy(v) {
				extras = append(extras, "obfs-uri="+s(v))
			}
			if h := objGet(objGet(ws, "headers"), "Host"); js.Truthy(h) {
				extras = append(extras, "obfs-host="+s(h))
			} else if js.Truthy(sni) {
				extras = append(extras, "obfs-host="+s(sni))
			}
		} else {
			if hasTLS {
				extras = append(extras, "over-tls=true")
			}
			if js.Truthy(sni) {
				extras = append(extras, "tls-host="+s(sni))
			}
		}
		if js.Truthy(proxy.Get("tfo")) {
			extras = append(extras, "fast-open=true")
		}
		extras = tlsVerify(extras)
		return withExtras("vmess="+hp+", method="+normalizeQxVmessMethod(proxy.Get("cipher"))+", password="+s(js.Or(proxy.Get("uuid"), "")), extras) + tag
	case "trojan":
		var extras []string
		if js.StrictEquals(proxy.Get("network"), "ws") || js.Truthy(proxy.Get("ws-opts")) {
			extras = append(extras, "obfs=ws")
			ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
			if v := objGet(ws, "path"); js.Truthy(v) {
				extras = append(extras, "obfs-uri="+s(v))
			}
			if h := objGet(objGet(ws, "headers"), "Host"); js.Truthy(h) {
				extras = append(extras, "obfs-host="+s(h))
			}
		} else {
			extras = append(extras, "over-tls=true")
		}
		if js.Truthy(sni) {
			extras = append(extras, "tls-host="+s(sni))
		}
		if v := proxy.Get("obfs"); js.Truthy(v) {
			extras = append(extras, "obfs="+s(v)+", obfs-host="+s(js.Or(proxy.Get("obfs-host"), "")))
		}
		if js.Truthy(proxy.Get("tfo")) {
			extras = append(extras, "fast-open=true")
		}
		extras = tlsVerify(extras)
		return withExtras("trojan="+hp+", password="+s(js.Or(proxy.Get("password"), "")), extras) + tag
	case "vless":
		extras := []string{"method=none"}
		transport := js.Or(proxy.Get("network"), "tcp")
		isReality := js.StrictEquals(proxy.Get("security"), "reality") || js.Truthy(proxy.Get("reality-opts"))
		hasTLS := js.Truthy(proxy.Get("tls")) || isReality
		host := js.Or(proxy.Get("sni"), proxy.Get("servername"))
		pick := func(a, b string) string {
			if hasTLS {
				return a
			}
			return b
		}
		switch {
		case js.StrictEquals(transport, "ws") || js.Truthy(proxy.Get("ws-opts")):
			extras = append(extras, pick("obfs=wss", "obfs=ws"))
			ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
			if h := objGet(objGet(ws, "headers"), "Host"); js.Truthy(h) {
				extras = append(extras, "obfs-host="+s(h))
			} else if js.Truthy(host) {
				extras = append(extras, "obfs-host="+s(host))
			}
			if v := objGet(ws, "path"); js.Truthy(v) {
				extras = append(extras, "obfs-uri="+s(v))
			}
		case js.StrictEquals(transport, "grpc") || js.Truthy(proxy.Get("grpc-opts")):
			extras = append(extras, pick("obfs=over-tls", "obfs=grpc"))
			if js.Truthy(host) {
				extras = append(extras, "obfs-host="+s(host))
			}
			g := js.Or(proxy.Get("grpc-opts"), proxy.Get("grpcOpts"))
			if v := objGet(g, "grpc-service-name"); !hasTLS && js.Truthy(v) {
				extras = append(extras, "obfs-uri="+s(v))
			}
		case js.StrictEquals(transport, "xhttp") || js.Truthy(proxy.Get("xhttp-opts")):
			extras = append(extras, pick("obfs=over-tls", "obfs=http"))
			x := js.Or(proxy.Get("xhttp-opts"), proxy.Get("xhttpOpts"))
			if h := objGet(x, "host"); js.Truthy(h) {
				extras = append(extras, "obfs-host="+s(h))
			} else if js.Truthy(host) {
				extras = append(extras, "obfs-host="+s(host))
			}
			if v := objGet(x, "path"); !hasTLS && js.Truthy(v) {
				extras = append(extras, "obfs-uri="+s(v))
			}
		case hasTLS:
			extras = append(extras, "obfs=over-tls")
			if js.Truthy(host) {
				extras = append(extras, "obfs-host="+s(host))
			}
		}
		if isReality {
			ro := js.Or(proxy.Get("reality-opts"), js.NewObject())
			if v := objGet(ro, "public-key"); js.Truthy(v) {
				extras = append(extras, "reality-base64-pubkey="+s(v))
			}
			if v := objGet(ro, "short-id"); js.Truthy(v) {
				extras = append(extras, "reality-hex-shortid="+s(v))
			}
		}
		if v := proxy.Get("flow"); js.Truthy(v) {
			extras = append(extras, "vless-flow="+s(v))
		}
		extras = tlsVerify(extras)
		return withExtras("vless="+hp+", password="+s(js.Or(proxy.Get("uuid"), "")), extras) + tag
	case "http", "https":
		var extras []string
		if typ == "https" {
			extras = append(extras, "over-tls=true")
		}
		if js.Truthy(sni) {
			extras = append(extras, "tls-host="+s(sni))
		}
		extras = tlsVerify(extras)
		return withExtras("http="+hp+", username="+s(js.Or(proxy.Get("username"), ""))+", password="+s(js.Or(proxy.Get("password"), "")), extras) + tag
	case "hysteria2", "hy2":
		return ""
	case "tuic":
		var parts []string
		if v := proxy.Get("uuid"); js.Truthy(v) {
			parts = append(parts, s(v))
		}
		if v := proxy.Get("password"); js.Truthy(v) {
			parts = append(parts, s(v))
		}
		if js.Truthy(sni) {
			parts = append(parts, "sni="+s(sni))
		}
		if cc := js.Or(proxy.Get("congestion-control"), proxy.Get("congestion-controller")); js.Truthy(cc) {
			parts = append(parts, "congestion-controller="+s(cc))
		}
		if v := proxy.Get("udp-relay-mode"); js.Truthy(v) {
			parts = append(parts, "udp-relay="+s(v))
		}
		if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
			parts = append(parts, "alpn="+s(joinIfArray(alpn)))
		}
		parts = tlsVerify(parts)
		return "tuic=" + hp + ", " + strings.Join(parts, ", ") + tag
	case "anytls":
		extras := []string{"password=" + s(js.Or(proxy.Get("password"), "")), "over-tls=true"}
		if skipCertVerify(proxy) {
			extras = append(extras, "tls-verification=false")
		} else {
			extras = append(extras, "tls-verification=true")
		}
		if js.Truthy(sni) {
			extras = append(extras, "tls-host="+s(sni))
		}
		if js.StrictEquals(proxy.Get("security"), "reality") || js.Truthy(proxy.Get("reality-opts")) {
			ro := js.Or(proxy.Get("reality-opts"), js.NewObject())
			if v := objGet(ro, "public-key"); js.Truthy(v) {
				extras = append(extras, "reality-base64-pubkey="+s(v))
			}
			if v := objGet(ro, "short-id"); js.Truthy(v) {
				extras = append(extras, "reality-hex-shortid="+s(v))
			}
		}
		extras = append(extras, "fast-open="+boolWord(proxy.Get("tfo")), "udp-relay="+boolWord(proxy.Get("udp")))
		return "anytls=" + hp + ", " + strings.Join(extras, ", ") + tag
	}
	return ""
}

var qxGroupIcons = map[string]string{
	DefaultSelectGroup: qureIconRepo + "/Proxy.png",
	DefaultRelayGroup:  qureIconRepo + "/Proxy.png",
	"自动选择":             qureIconRepo + "/Speedtest.png",
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

const qxEmptyConfig = "#!MANAGED-CONFIG http://example.com interval=86400 strict=false\n\n[general]\nserver_check_url = http://www.gstatic.com/generate_204\nexcluded_routes = 192.168.0.0/16, 172.16.0.0/12, 100.64.0.0/10, 10.0.0.0/8\n\n[dns]\nno-ipv6\nserver = 223.5.5.5\nserver = 119.29.29.29\n\n[server_remote]\n\n[server_local]\n\n[rewrite_remote]\n\n[rewrite_local]\n\n[mitm]\n"

func normalizeQxPolicyMember(m any) any {
	switch js.ToUpper(strOr(m)) {
	case "DIRECT":
		return "direct"
	case "REJECT", "REJECT-DROP":
		return "reject"
	}
	return m
}

// GenerateBuiltinQuanxConfig 对应 generateBuiltinQuanxConfig（输出 Quantumult X 配置）。
func GenerateBuiltinQuanxConfig(nodeList string, options *js.Object) string {
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
		p.Set("name", namer.GetUniqueName(sanitizeQxNodeName(p.Get("name"))))
		if line := buildQxLine(p); line != "" {
			lines = append(lines, line)
			withMeta = append(withMeta, p)
		}
	}
	if len(lines) == 0 {
		return qxEmptyConfig
	}
	var sections []string
	if js.Truthy(managed) {
		sections = append(sections, "#!MANAGED-CONFIG "+s(managed)+" interval="+s(interval)+" strict=false")
	}
	sections = append(sections,
		"[general]\nserver_check_url = http://www.gstatic.com/generate_204\nexcluded_routes = 192.168.0.0/16, 172.16.0.0/12, 100.64.0.0/10, 10.0.0.0/8",
		"[dns]\nno-ipv6\nserver = 223.5.5.5\nserver = 119.29.29.29",
		"[server_remote]",
		"[server_local]\n"+strings.Join(lines, "\n"),
	)
	levelKey := js.ToUpper(s(js.Or(ruleLevel, "std")))
	factoryLevel := levelKey
	switch levelKey {
	case "BASE", "STD", "FULL", "RELAY":
	default:
		factoryLevel = "STD"
	}
	groups := PruneProxyGroups(asObjects(PolicyGroups(factoryLevel, withMeta, options)), withMeta)
	var groupLines []string
	for _, g := range groups {
		typ := "static"
		switch {
		case js.StrictEquals(g.Get("type"), "url-test"):
			typ = "url-latency-benchmark"
		case js.StrictEquals(g.Get("type"), "fallback"):
			typ = "available"
		}
		members := js.Arr()
		for _, m := range arrayOrEmpty(g.Get("proxies")).Items {
			members.Push(normalizeQxPolicyMember(m))
		}
		icon := ""
		if ic, ok := qxGroupIcons[s(g.Get("name"))]; ok {
			icon = ", img-url=" + ic
		}
		extra := ""
		if typ == "url-latency-benchmark" {
			extra = ", check-interval=300, tolerance=50"
		}
		groupLines = append(groupLines, typ+"="+s(g.Get("name"))+", "+members.Join(", ")+extra+icon)
	}
	sections = append(sections, "[policy]\n"+strings.Join(groupLines, "\n"))
	var remote, local []string
	for _, r := range GetBuiltinRules(levelKey, "quanx") {
		line := s(r)
		if strings.HasPrefix(line, "filter_remote") {
			remote = append(remote, reFilterRemotePrefix.Replace(line, ""))
			continue
		}
		for _, rw := range qxRuleRewrites {
			line = rw.re.Replace(line, rw.rep)
		}
		local = append(local, line)
	}
	localLines := []string{"; 基础分流", "host-suffix, localhost, direct", "host-suffix, local, direct", "ip-cidr, 127.0.0.0/8, direct",
		"ip-cidr, 10.0.0.0/8, direct", "ip-cidr, 100.64.0.0/10, direct", "ip-cidr, 172.16.0.0/12, direct", "ip-cidr, 192.168.0.0/16, direct"}
	for _, l := range local {
		if l != "" {
			localLines = append(localLines, l)
		}
	}
	sections = append(sections, "[filter_remote]\n"+strings.Join(remote, "\n"), "[filter_local]\n"+strings.Join(localLines, "\n"),
		"[rewrite_remote]", "[rewrite_local]", "[mitm]")
	return strings.Join(sections, "\n\n") + "\n"
}
