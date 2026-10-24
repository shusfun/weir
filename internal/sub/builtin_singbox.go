package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/builtin-singbox-generator.js

var (
	reWhitespaceRun = js.MustRegExp(`\s+`, "g")
	reCRLFTab       = js.MustRegExp(`[\r\n\t]`, "g")
)

func sanitizeSingboxName(name any) string {
	if !js.Truthy(name) {
		return "Untitled"
	}
	safe := cleanControlChars(s(name))
	safe = js.Trim(reCRLFTab.Replace(reWhitespaceRun.Replace(safe, " "), " "))
	if safe == "" {
		return "Untitled"
	}
	return safe
}

func ensureTLS(ob *js.Object) *js.Object {
	t, ok := ob.Get("tls").(*js.Object)
	if !ok || !js.Truthy(ob.Get("tls")) {
		t = js.NewObject()
		ob.Set("tls", t)
	}
	return t
}

func builtinSingboxOutbound(proxy *js.Object) *js.Object {
	if proxy == nil || !js.Truthy(proxy.Get("server")) || !js.Truthy(proxy.Get("port")) {
		return nil
	}
	typ := js.ToLower(s(js.Or(proxy.Get("type"), "")))
	tag := sanitizeSingboxName(proxy.Get("name"))
	server := proxy.Get("server")
	port := parsePortSB(proxy.Get("port"))
	if !js.Truthy(port) {
		return nil
	}
	sniOr := func() any { return js.Or(proxy.Get("sni"), proxy.Get("servername"), server) }
	ob := js.Obj("tag", tag, "type", "direct")
	base := func(t string) {
		ob.Set("type", t)
		ob.Set("server", server)
		ob.Set("server_port", port)
	}
	switch typ {
	case "ss", "shadowsocks":
		base("shadowsocks")
		ob.Set("method", js.Or(proxy.Get("cipher"), "aes-128-gcm"))
		ob.Set("password", js.Or(proxy.Get("password"), ""))
		if js.Truthy(proxy.Get("udp")) {
			ob.Set("udp_over_tcp", false)
		}
		plugin := js.Or(proxy.Get("plugin"), "")
		opts := js.Or(proxy.Get("plugin-opts"), proxy.Get("pluginOpts"), js.NewObject())
		if js.StrictEquals(plugin, "v2ray-plugin") || js.StrictEquals(objGet(opts, "mode"), "websocket") {
			ob.Set("plugin", "v2ray-plugin")
			po := []string{"mode=websocket"}
			if v := objGet(opts, "host"); js.Truthy(v) {
				po = append(po, "host="+s(v))
			}
			if v := objGet(opts, "path"); js.Truthy(v) {
				po = append(po, "path="+s(v))
			}
			if js.Truthy(objGet(opts, "tls")) || js.StrictEquals(objGet(opts, "mode"), "websocket-tls") {
				po = append(po, "tls")
			}
			ob.Set("plugin_opts", strings.Join(po, ";"))
		}
	case "vmess":
		base("vmess")
		ob.Set("uuid", js.Or(proxy.Get("uuid"), ""))
		ob.Set("security", js.Or(proxy.Get("cipher"), "auto"))
		alterID := 0.0
		if n := js.ToNumber(proxy.Get("alterId")); js.IsFinite(n) {
			alterID = n
		}
		ob.Set("alter_id", alterID)
		if js.Truthy(proxy.Get("tls")) || js.Truthy(proxy.Get("sni")) || js.Truthy(proxy.Get("servername")) || js.Truthy(proxy.Get("skip-cert-verify")) {
			tls := js.Obj("enabled", true, "server_name", sniOr())
			if skipCertVerify(proxy) {
				tls.Set("insecure", true)
			}
			ob.Set("tls", tls)
		}
		tt := "tcp"
		if js.StrictEquals(proxy.Get("network"), "ws") {
			tt = "ws"
		}
		transport := js.Obj("type", tt)
		if p := js.Or(objGet(proxy.Get("ws-opts"), "path"), objGet(proxy.Get("wsOpts"), "path")); js.Truthy(p) {
			transport.Set("path", p)
		}
		if h := js.Or(objGet(objGet(proxy.Get("ws-opts"), "headers"), "Host"), objGet(objGet(proxy.Get("wsOpts"), "headers"), "Host")); js.Truthy(h) {
			transport.Set("headers", js.Obj("Host", h))
		}
		ob.Set("transport", transport)
	case "vless":
		base("vless")
		ob.Set("uuid", js.Or(proxy.Get("uuid"), ""))
		if network := proxy.Get("network"); js.Truthy(network) {
			transport := js.Obj("type", network)
			switch {
			case js.StrictEquals(network, "ws"):
				ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
				if v := objGet(ws, "path"); js.Truthy(v) {
					transport.Set("path", v)
				}
				if h := objGet(objGet(ws, "headers"), "Host"); js.Truthy(h) {
					transport.Set("headers", js.Obj("Host", h))
				}
			case js.StrictEquals(network, "grpc"):
				g := js.Or(proxy.Get("grpc-opts"), proxy.Get("grpcOpts"))
				if v := objGet(g, "grpc-service-name"); js.Truthy(v) {
					transport.Set("service_name", v)
				}
			case js.StrictEquals(network, "xhttp"):
				x := js.Or(proxy.Get("xhttp-opts"), proxy.Get("xhttpOpts"))
				for _, k := range []string{"path", "host", "mode"} {
					if v := objGet(x, k); js.Truthy(v) {
						transport.Set(k, v)
					}
				}
			}
			ob.Set("transport", transport)
		}
		if v := proxy.Get("flow"); js.Truthy(v) {
			ob.Set("flow", v)
		}
		if reality := js.Or(proxy.Get("reality_opts"), proxy.Get("reality-opts")); js.Truthy(reality) {
			ob.Set("tls", js.Obj("enabled", true, "server_name", sniOr(),
				"utls", js.Obj("enabled", true, "fingerprint", js.Or(proxy.Get("client-fingerprint"), "chrome")),
				"reality", js.Obj("enabled", true, "public_key", js.Or(objGet(reality, "public-key"), objGet(reality, "public_key")),
					"short_id", js.Or(objGet(reality, "short-id"), objGet(reality, "short_id")))))
		}
	case "trojan":
		base("trojan")
		ob.Set("password", js.Or(proxy.Get("password"), ""))
		ob.Set("tls", js.Obj("enabled", true, "server_name", sniOr()))
		if js.StrictEquals(proxy.Get("network"), "ws") {
			ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
			ob.Set("transport", js.Obj("type", "ws", "path", js.Or(objGet(ws, "path"), "/"), "headers", js.Or(objGet(ws, "headers"), js.NewObject())))
		}
	case "hysteria2", "hy2":
		base("hysteria2")
		ob.Set("password", js.Or(proxy.Get("password"), ""))
		ob.Set("tls", js.Obj("enabled", true, "server_name", sniOr()))
	case "tuic":
		base("tuic")
		ob.Set("uuid", js.Or(proxy.Get("uuid"), ""))
		ob.Set("password", js.Or(proxy.Get("password"), ""))
		if cc := js.Or(proxy.Get("congestion-controller"), proxy.Get("congestion-control"), proxy.Get("congestion")); js.Truthy(cc) {
			ob.Set("congestion_control", cc)
		}
		if v := proxy.Get("udp-over-stream"); v != nil {
			ob.Set("udp_over_stream", js.Truthy(v))
		}
		if proxy.Get("zero-rtt-handshake") != nil || proxy.Get("reduce-rtt") != nil {
			ob.Set("zero_rtt_handshake", js.Truthy(js.Coalesce(proxy.Get("zero-rtt-handshake"), proxy.Get("reduce-rtt"))))
		}
		if v := proxy.Get("heartbeat"); js.Truthy(v) {
			ob.Set("heartbeat", s(v))
		}
		if v := proxy.Get("network"); js.Truthy(v) {
			ob.Set("network", v)
		}
		tls := js.Obj("enabled", true, "server_name", sniOr())
		if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
			tls.Set("alpn", arrayWrap(alpn))
		}
		ob.Set("tls", tls)
	case "anytls":
		base("anytls")
		ob.Set("password", js.Or(proxy.Get("password"), ""))
		ob.Set("tls", js.Obj("enabled", true, "server_name", sniOr()))
	case "snell":
		base("snell")
		ob.Set("psk", js.Or(proxy.Get("psk"), proxy.Get("password"), ""))
		if v := proxy.Get("version"); js.Truthy(v) {
			ob.Set("version", js.ToNumber(v))
		}
	case "wireguard":
		base("wireguard")
		ob.Set("private_key", js.Or(proxy.Get("private-key"), ""))
		var local any = js.Arr()
		if ip := proxy.Get("ip"); js.IsArray(ip) {
			local = ip
		} else if js.Truthy(ip) {
			local = js.Arr(ip)
		}
		ob.Set("local_address", local)
		ob.Set("peer_public_key", js.Or(proxy.Get("public-key"), ""))
		ob.Set("pre_shared_key", js.Or(proxy.Get("preshared-key"), ""))
		ob.Set("reserved", proxy.Get("reserved"))
	case "http", "https":
		base("http")
		if v := proxy.Get("username"); js.Truthy(v) {
			ob.Set("username", v)
		}
		if v := proxy.Get("password"); js.Truthy(v) {
			ob.Set("password", v)
		}
		if typ == "https" || js.Truthy(proxy.Get("tls")) || js.Truthy(proxy.Get("sni")) || js.Truthy(proxy.Get("servername")) {
			ob.Set("tls", js.Obj("enabled", true, "server_name", sniOr(), "insecure", skipCertVerify(proxy)))
		}
	case "socks5", "socks5-tls":
		base("socks")
		if v := proxy.Get("username"); js.Truthy(v) {
			ob.Set("username", v)
		}
		if v := proxy.Get("password"); js.Truthy(v) {
			ob.Set("password", v)
		}
		if typ == "socks5-tls" {
			ob.Set("tls", js.Obj("enabled", true, "server_name", sniOr()))
		}
	default:
		return nil
	}
	if skipCertVerify(proxy) {
		t := ensureTLS(ob)
		t.Set("enabled", true)
		t.Set("insecure", true)
	}
	if sni := js.Or(proxy.Get("sni"), proxy.Get("servername")); js.Truthy(sni) &&
		((typ != "ss" && typ != "shadowsocks") || js.Truthy(proxy.Get("tls")) || js.Truthy(objGet(ob.Get("tls"), "enabled"))) {
		ensureTLS(ob).Set("server_name", sni)
	}
	if fp := proxy.Get("client-fingerprint"); js.Truthy(fp) {
		ensureTLS(ob).Set("utls", js.Obj("enabled", true, "fingerprint", fp))
	}
	if js.Truthy(proxy.Get("tfo")) {
		ob.Set("tcp_fast_open", true)
	}
	return ob
}

// GenerateBuiltinSingboxConfig 对应 generateBuiltinSingboxConfig（输出 sing-box JSON）。
func GenerateBuiltinSingboxConfig(nodeList string, options *js.Object) string {
	if options == nil {
		options = js.NewObject()
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
	var outbounds []*js.Object
	type entry struct {
		tag      any
		outbound *js.Object
	}
	var entries []entry
	namer := NewUniqueNamer()
	for _, p := range URLsToClashProxies(urls, options) {
		p.Set("name", namer.GetUniqueName(sanitizeSingboxName(p.Get("name"))))
		if ob := builtinSingboxOutbound(p); ob != nil {
			outbounds = append(outbounds, ob)
			entries = append(entries, entry{ob.Get("tag"), ob})
		}
	}
	if len(outbounds) == 0 {
		outbounds = append(outbounds, js.Obj("tag", "DIRECT", "type", "direct"))
	}
	levelKey := js.ToUpper(s(js.Or(ruleLevel, "std")))
	factoryLevel := levelKey
	switch levelKey {
	case "BASE", "STD", "FULL", "RELAY":
	default:
		factoryLevel = "STD"
	}
	groups := PruneProxyGroups(asObjects(PolicyGroups(factoryLevel, outbounds, options)), outbounds)
	if levelKey == "RELAY" {
		chainTags := js.Arr()
		for _, e := range entries {
			c := e.outbound.Clone()
			c.Set("tag", "🔗 链式代理 - "+s(e.tag))
			c.Set("detour", "入口节点")
			outbounds = append(outbounds, c)
			chainTags.Push(c.Get("tag"))
		}
		var next []*js.Object
		for _, g := range groups {
			switch {
			case js.StrictEquals(g.Get("name"), "🔗 链式代理"):
				c := g.Clone()
				c.Set("type", "select")
				c.Set("proxies", chainTags)
				next = append(next, c)
			case js.StrictEquals(g.Get("name"), "落地节点"):
			default:
				next = append(next, g)
			}
		}
		groups = PruneProxyGroups(next, outbounds)
	}
	groupOutbounds := js.Arr()
	for _, g := range groups {
		typ := "selector"
		if js.StrictEquals(g.Get("type"), "url-test") || js.StrictEquals(g.Get("type"), "fallback") {
			typ = "urltest"
		}
		ob := js.Obj("tag", g.Get("name"), "type", typ, "outbounds", g.Get("proxies"))
		if typ == "urltest" {
			ob.Set("url", gstaticURL)
			ob.Set("interval", "10m")
			ob.Set("tolerance", 50.0)
		}
		groupOutbounds.Push(ob)
	}
	rawRules := GetBuiltinRules(levelKey, "singbox")
	ruleSets := js.Arr(GetSingboxDNSRuleSet())
	for _, kv := range GetRemoteProviderDefinitions("singbox", rawRules).Entries() {
		if !js.StrictEquals(objGet(kv.Value, "tag"), SingboxCNRuleSet) {
			ruleSets.Push(kv.Value)
		}
	}
	routeRules := js.Arr(
		js.Obj("ip_cidr", strs2("127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"), "outbound", "DIRECT"),
		js.Obj("domain_suffix", strs2("localhost"), "outbound", "DIRECT"),
	)
	for _, r := range rawRules {
		if js.StrictEquals(objGet(r, "type"), "rule_set") {
			routeRules.Push(js.Obj("rule_set", js.Arr(objGet(r, "tag")), "outbound", objGet(r, "outbound")))
		} else {
			routeRules.Push(r)
		}
	}
	routeRules.Push(js.Obj("domain_suffix", strs2("cn"), "outbound", "DIRECT"))
	dns := BuildSingboxDNSConfig(js.Or(options.Get("customDnsOverride"), ""), SafeDNSOptions{Mode: options.Get("dnsMode"), ProxyGroup: DNSProxyGroup})
	resolver := any("dns-cn-1")
	if first := objGet(dns.Get("servers"), "0"); first != nil {
		resolver = js.Or(objGet(first, "tag"), "dns-cn-1")
	}
	final := DefaultSelectGroup
	if levelKey == "RELAY" {
		final = DefaultRelayGroup
	}
	all := js.Arr(js.Obj("tag", "DIRECT", "type", "direct"), js.Obj("tag", "REJECT", "type", "block"))
	for _, ob := range outbounds {
		all.Push(ob)
	}
	all.Push(groupOutbounds.Items...)
	config := js.Obj(
		"log", js.Obj("level", "info"),
		"dns", dns,
		"inbounds", js.Arr(js.Obj("type", "tun", "tag", "tun-in", "address", strs2("172.19.0.1/30"), "auto_route", true, "strict_route", true, "stack", "mixed")),
		"outbounds", all,
		"route", js.Obj("auto_detect_interface", true, "default_domain_resolver", resolver, "final", final, "rule_set", ruleSets, "rules", routeRules),
	)
	out, _ := js.JSONStringifyIndent(config, "  ")
	return out + "\n"
}
