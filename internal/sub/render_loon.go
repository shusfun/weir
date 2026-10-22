package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/template-renderers/render-loon.js

var reReservedSep = js.MustRegExp(`[\/,]`, "")

func loonProxyLine(proxy *js.Object) any {
	typ := js.ToLower(s(js.Or(proxy.Get("type"), "")))
	name := js.Or(proxy.Get("name"), "Untitled")
	server, port := proxy.Get("server"), proxy.Get("port")
	if !js.Truthy(server) || !js.Truthy(port) {
		return nil
	}
	sni := sniOf(proxy)
	head := func(kind string) string { return s(name) + " = " + kind + ", " + s(server) + ", " + s(port) }
	wsExtras := func(extras []string) []string {
		ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
		if v := objGet(ws, "path"); js.Truthy(v) {
			extras = append(extras, "path="+s(v))
		}
		if h := objGet(objGet(ws, "headers"), "Host"); js.Truthy(h) {
			extras = append(extras, "host="+s(h))
		}
		return extras
	}
	switch typ {
	case "trojan":
		extras := []string{s(js.Or(proxy.Get("password"), ""))}
		if js.StrictEquals(proxy.Get("network"), "ws") {
			extras = wsExtras(append(extras, "transport=ws"))
		}
		if sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "skip-cert-verify=true")
		}
		return head("trojan") + ", " + strings.Join(extras, ", ")
	case "ss", "shadowsocks":
		var extras []string
		plugin := js.Or(proxy.Get("plugin"), "")
		opts := js.Or(proxy.Get("plugin-opts"), proxy.Get("pluginOpts"), js.NewObject())
		if js.StrictEquals(plugin, "obfs-local") || js.Truthy(proxy.Get("obfs")) {
			extras = append(extras, "obfs-name="+s(js.Or(proxy.Get("obfs"), objGet(opts, "mode"))))
			if h := js.Or(proxy.Get("obfs-host"), objGet(opts, "host")); js.Truthy(h) {
				extras = append(extras, "obfs-host="+s(h))
			}
			if u := js.Or(proxy.Get("obfs-uri"), objGet(opts, "uri")); js.Truthy(u) {
				extras = append(extras, "obfs-uri="+s(u))
			}
		} else if js.StrictEquals(plugin, "v2ray-plugin") || js.StrictEquals(objGet(opts, "mode"), "websocket") {
			extras = append(extras, "transport=ws")
			if v := objGet(opts, "path"); js.Truthy(v) {
				extras = append(extras, "path="+s(v))
			}
			if v := objGet(opts, "host"); js.Truthy(v) {
				extras = append(extras, "host="+s(v))
			}
			if js.Truthy(objGet(opts, "tls")) || js.StrictEquals(objGet(opts, "mode"), "websocket-tls") {
				extras = append(extras, "over-tls=true")
			}
		}
		if js.Truthy(proxy.Get("udp")) {
			extras = append(extras, "udp-relay=true")
		}
		out := s(name) + " = Shadowsocks, " + s(server) + ", " + s(port) + ", " + s(js.Or(proxy.Get("cipher"), "aes-128-gcm")) + ", " + s(js.Or(proxy.Get("password"), ""))
		if len(extras) > 0 {
			out += ", " + strings.Join(extras, ", ")
		}
		return out
	case "vmess":
		cipher := js.Or(proxy.Get("cipher"), "auto")
		uuid := `"` + s(js.Or(proxy.Get("uuid"), "")) + `"`
		alterID := js.Or(proxy.Get("alterId"), 0.0)
		var extras []string
		if js.Truthy(proxy.Get("tls")) || sni != nil {
			extras = append(extras, "over-tls=true")
		}
		if js.StrictEquals(proxy.Get("network"), "ws") {
			extras = wsExtras(append(extras, "transport=ws"))
		}
		if sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "skip-cert-verify=true")
		}
		extraStr := ""
		if len(extras) > 0 {
			extraStr = ", " + strings.Join(extras, ", ")
		}
		return head("vmess") + ", " + s(cipher) + ", " + uuid + ", " + s(alterID) + extraStr
	case "vless":
		extras := []string{s(js.Or(proxy.Get("uuid"), ""))}
		network := proxy.Get("network")
		if js.Truthy(network) {
			extras = append(extras, "transport="+s(network))
		}
		if js.StrictEquals(network, "ws") {
			extras = wsExtras(extras)
		}
		if js.StrictEquals(network, "grpc") {
			g := js.Or(proxy.Get("grpc-opts"), proxy.Get("grpcOpts"))
			if v := objGet(g, "grpc-service-name"); js.Truthy(v) {
				extras = append(extras, "grpc-service-name="+s(v))
			}
		}
		if v := proxy.Get("flow"); js.Truthy(v) {
			extras = append(extras, "flow="+s(v))
		}
		if js.Truthy(proxy.Get("tls")) || sni != nil {
			extras = append(extras, "over-tls=true")
		}
		if ro := js.Or(proxy.Get("reality-opts"), proxy.Get("realityOpts")); js.Truthy(ro) {
			extras = append(extras, "reality=true")
			if v := objGet(ro, "public-key"); js.Truthy(v) {
				extras = append(extras, "public-key="+s(v))
			}
			if v := objGet(ro, "short-id"); js.Truthy(v) {
				extras = append(extras, "short-id="+s(v))
			}
		}
		if sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "skip-cert-verify=true")
		}
		return head("vless") + ", " + strings.Join(extras, ", ")
	case "hysteria2", "hy2":
		extras := []string{s(js.Or(proxy.Get("password"), ""))}
		if sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "skip-cert-verify=true")
		}
		if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
			if a := joinIfArray(alpn); js.Truthy(a) {
				extras = append(extras, "alpn="+s(a))
			}
		}
		return head("hysteria2") + ", " + strings.Join(extras, ", ")
	case "tuic":
		extras := []string{"version=5"}
		if sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "skip-cert-verify=true")
		}
		if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
			extras = append(extras, "alpn="+s(joinIfArray(alpn)))
		}
		extras = append(extras, "congestion-control="+s(js.Or(proxy.Get("congestion-control"), proxy.Get("congestion-controller"), "bbr")))
		if v := proxy.Get("udp-relay-mode"); js.Truthy(v) {
			extras = append(extras, "udp-relay-mode="+s(v))
		}
		extras = append(extras, "reduce-rtt=true")
		if js.Truthy(proxy.Get("fast-open")) {
			extras = append(extras, "fast-open=true")
		}
		return head("tuic") + ", " + s(js.Or(proxy.Get("password"), "")) + ", " + s(js.Or(proxy.Get("uuid"), "")) + ", " + strings.Join(extras, ", ")
	case "wireguard":
		if !js.Truthy(proxy.Get("private-key")) || !js.Truthy(proxy.Get("public-key")) {
			return nil
		}
		var extras []string
		if ip := proxy.Get("ip"); js.Truthy(ip) {
			ipv4 := ip
			if a, ok := ip.(*js.Array); ok {
				ipv4 = findInArray(a, func(v any) bool { return !strings.Contains(s(v), ":") })
			}
			if js.Truthy(ipv4) {
				extras = append(extras, "interface-ip="+s(ipv4))
			}
		}
		if v6 := proxy.Get("ipv6"); js.Truthy(v6) {
			ipv6 := v6
			if a, ok := v6.(*js.Array); ok {
				ipv6 = findInArray(a, js.Truthy)
			}
			if js.Truthy(ipv6) {
				extras = append(extras, "interface-ipV6="+s(ipv6))
			}
		}
		extras = append(extras, `private-key="`+s(proxy.Get("private-key"))+`"`)
		if v := proxy.Get("mtu"); js.Truthy(v) {
			extras = append(extras, "mtu="+s(v))
		}
		if d := proxy.Get("dns"); js.Truthy(d) {
			var list []any
			if a, ok := d.(*js.Array); ok {
				list = truthyItems(a)
			} else {
				list = []any{d}
			}
			dnsList := &js.Array{Items: list}
			if v := findInArray(dnsList, func(v any) bool { return !strings.Contains(s(v), ":") }); js.Truthy(v) {
				extras = append(extras, "dns="+s(v))
			}
			if v := findInArray(dnsList, func(v any) bool { return strings.Contains(s(v), ":") }); js.Truthy(v) {
				extras = append(extras, "dnsV6="+s(v))
			}
		}
		peer := []string{`public-key="` + s(proxy.Get("public-key")) + `"`}
		if v := proxy.Get("preshared-key"); js.Truthy(v) {
			peer = append(peer, `preshared-key="`+s(v)+`"`)
		}
		if r := proxy.Get("reserved"); js.Truthy(r) {
			var reserved *js.Array
			if a, ok := r.(*js.Array); ok {
				reserved = a
			} else {
				reserved = js.Arr()
				for _, part := range reReservedSep.Split(s(r), -1) {
					if n := js.ToNumber(js.Trim(part)); js.IsFinite(n) {
						reserved.Push(n)
					}
				}
			}
			if reserved.Len() > 0 {
				peer = append(peer, "reserved=["+reserved.Join(",")+"]")
			}
		}
		if a := proxy.Get("allowed-ips"); js.Truthy(a) {
			var allowed any = a
			if arr, ok := a.(*js.Array); ok {
				allowed = arr.Join(", ")
			}
			peer = append(peer, `allowed-ips="`+s(allowed)+`"`)
		} else if js.Truthy(proxy.Get("ipv6")) {
			peer = append(peer, `allowed-ips="0.0.0.0/0, ::/0"`)
		} else {
			peer = append(peer, `allowed-ips="0.0.0.0/0"`)
		}
		host := s(server)
		if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
			host = "[" + host + "]"
		}
		peer = append(peer, "endpoint="+host+":"+s(port))
		extras = append(extras, "peers=[{"+strings.Join(peer, ",")+"}]")
		return s(name) + " = wireguard," + strings.Join(extras, ",")
	case "anytls":
		extras := []string{s(js.Or(proxy.Get("password"), ""))}
		if sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
			if a := joinIfArray(alpn); js.Truthy(a) {
				extras = append(extras, "alpn="+s(a))
			}
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "skip-cert-verify=true")
		}
		return head("anytls") + ", " + strings.Join(extras, ", ")
	}
	return nil
}

func loonGroupLine(group *js.Object) string {
	typ := js.ToLower(s(js.Or(group.Get("type"), "select")))
	members := strings.Join(groupMembers(group, typ), ", ")
	filter := groupFilter(group)
	opts := group.Get("options")
	tolerance := objGet(opts, "tolerance")
	urlV := s(js.Or(objGet(opts, "url"), gstaticURL))
	interval := s(js.Or(objGet(opts, "interval"), 300.0))
	name := s(group.Get("name"))
	switch typ {
	case "url-test", "fallback":
		base := name + " = " + typ + ", " + members + ", url=" + urlV + ", interval=" + interval
		if filter != "" {
			base += ", filter=" + filter
		}
		if js.Truthy(tolerance) {
			return base + ", tolerance=" + s(tolerance)
		}
		return base
	case "load-balance":
		if filter != "" {
			return name + " = load-balance, " + members + ", filter=" + filter + ", url=" + urlV + ", interval=" + interval
		}
		return name + " = load-balance, " + members
	}
	return name + " = select, " + members
}

func loonRuleLine(rule *js.Object) any {
	typ := js.ToUpper(s(js.Or(rule.Get("type"), "")))
	switch typ {
	case "", "RULE-SET":
		return nil
	case "MATCH", "FINAL":
		return "FINAL," + s(rule.Get("policy"))
	case "GEOIP":
		return "GEOIP," + s(js.Or(rule.Get("value"), "CN")) + "," + s(rule.Get("policy"))
	}
	return typ + "," + s(rule.Get("value")) + "," + s(rule.Get("policy"))
}

func loonRemoteRuleLine(rule *js.Object) any {
	if js.ToUpper(s(js.Or(rule.Get("type"), ""))) != "RULE-SET" {
		return nil
	}
	if !js.Truthy(rule.Get("value")) || !js.Truthy(rule.Get("policy")) {
		return nil
	}
	return s(rule.Get("value")) + ", policy=" + s(rule.Get("policy")) + ", enabled=true"
}

// RenderLoonFromTemplateModel 对应 renderLoonFromTemplateModel。
func RenderLoonFromTemplateModel(modelVal any, options *js.Object) string {
	model := NormalizeUnifiedTemplateModel(modelVal)
	proxies := modelProxies(model, options)
	lines := []string{
		"[General]",
		"ip-mode = ipv4-preferred",
		"dns-server = 223.5.5.5, 119.29.29.29",
		"doh-server = https://223.5.5.5/resolve,https://sm2.doh.pub/dns-query",
		"skip-proxy = 127.0.0.1, 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, 100.64.0.0/10, localhost, *.local, elpass.app, e.crashlytics.com, captive.apple.com, ::ffff:0:0:0:0/1, ::ffff:128:0:0:0/1, fe80::/10, fc00::/7, ::1/128",
		"bypass-tun = 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, 100.64.0.0/10",
		"interface-mode = auto",
		"disable-stun = true",
		"allow-udp-proxy = true",
		"allow-wifi-access = false",
		"wifi-access-http-port = 7222",
		"wifi-access-socks5-port = 7221",
		"resource-parser = https://github.com/sub-store-org/Sub-Store/releases/latest/download/sub-store-parser.loon.min.js",
		"geoip-url = https://gitlab.com/Masaiki/GeoIP2-CN/-/raw/release/Country.mmdb",
		"proxy-test-url = http://www.gstatic.com/generate_204",
		"internet-test-url = http://www.apple.com/library/test/success.html",
		"",
		"[Proxy]",
	}
	for _, p := range proxies {
		lines = appendAny(lines, loonProxyLine(p))
	}
	lines = append(lines, "", "[Proxy Group]")
	for _, g := range groupsWithMembers(model) {
		lines = append(lines, loonGroupLine(g))
	}
	lines = append(lines, "", "[Rule]")
	rules := arrayOrEmpty(model.Get("rules")).Items
	for _, r := range rules {
		ro := objOrEmpty(r)
		if js.ToUpper(s(js.Or(ro.Get("type"), ""))) != "RULE-SET" {
			lines = appendAny(lines, loonRuleLine(ro))
		}
	}
	lines = append(lines, "", "[Remote Rule]")
	for _, r := range rules {
		ro := objOrEmpty(r)
		if js.StrictEquals(ro.Get("source"), "remote") || js.ToUpper(s(js.Or(ro.Get("type"), ""))) == "RULE-SET" {
			lines = appendAny(lines, loonRemoteRuleLine(ro))
		}
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}
