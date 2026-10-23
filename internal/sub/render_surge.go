package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/template-renderers/render-surge.js

// s 对应模板字符串插值 `${v}`。
func s(v any) string { return js.ToString(v) }

func isTrue(v any) bool { return js.StrictEquals(v, true) }

func skipCertVerify(p *js.Object) bool {
	return isTrue(p.Get("skip-cert-verify")) || isTrue(p.Get("skipCertVerify"))
}

func sniOf(p *js.Object) any { return js.Coalesce(p.Get("servername"), p.Get("sni")) }

func surgeProxyLine(proxy *js.Object) any {
	typ := js.ToLower(s(js.Or(proxy.Get("type"), "")))
	name := js.Or(proxy.Get("name"), "Untitled")
	server, port := proxy.Get("server"), proxy.Get("port")
	if !js.Truthy(server) || !js.Truthy(port) {
		return nil
	}
	head := func(kind string) string { return s(name) + " = " + kind + ", " + s(server) + ", " + s(port) }
	switch typ {
	case "trojan":
		extras := []string{"password=" + s(js.Or(proxy.Get("password"), ""))}
		if sni := sniOf(proxy); sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		if js.StrictEquals(proxy.Get("network"), "ws") {
			extras = append(extras, "ws=true")
			ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
			if v := objGet(ws, "path"); js.Truthy(v) {
				extras = append(extras, "ws-path="+s(v))
			}
			if h := objGet(objGet(ws, "headers"), "Host"); js.Truthy(h) {
				extras = append(extras, "ws-headers=Host:"+s(h))
			}
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "skip-cert-verify=true")
		}
		return head("trojan") + ", " + strings.Join(extras, ", ")
	case "ss", "shadowsocks":
		extras := []string{"encrypt-method=" + s(js.Or(proxy.Get("cipher"), "aes-128-gcm")), "password=" + s(js.Or(proxy.Get("password"), ""))}
		plugin := js.Or(proxy.Get("plugin"), "")
		opts := js.Or(proxy.Get("plugin-opts"), proxy.Get("pluginOpts"), js.NewObject())
		if js.StrictEquals(plugin, "obfs-local") || js.Truthy(proxy.Get("obfs")) {
			if m := js.Or(objGet(opts, "mode"), proxy.Get("obfs")); js.Truthy(m) {
				extras = append(extras, "obfs="+s(m))
			}
			if h := js.Or(objGet(opts, "host"), proxy.Get("obfs-host")); js.Truthy(h) {
				extras = append(extras, "obfs-host="+s(h))
			}
			if u := js.Or(objGet(opts, "uri"), proxy.Get("obfs-uri")); js.Truthy(u) {
				extras = append(extras, "obfs-uri="+s(u))
			}
		} else if js.StrictEquals(plugin, "v2ray-plugin") || js.StrictEquals(objGet(opts, "mode"), "websocket") {
			extras = append(extras, "ws=true")
			if v := objGet(opts, "path"); js.Truthy(v) {
				extras = append(extras, "ws-path="+s(v))
			}
			if v := objGet(opts, "host"); js.Truthy(v) {
				extras = append(extras, "ws-headers=Host:"+s(v))
			}
			if js.Truthy(objGet(opts, "tls")) || js.StrictEquals(objGet(opts, "mode"), "websocket-tls") {
				extras = append(extras, "tls=true")
			}
		}
		if js.Truthy(proxy.Get("udp")) {
			extras = append(extras, "udp-relay=true")
		}
		return head("ss") + ", " + strings.Join(extras, ", ")
	case "vmess":
		extras := []string{"username=" + s(js.Or(proxy.Get("uuid"), ""))}
		network := js.Or(proxy.Get("network"), "")
		switch {
		case js.StrictEquals(network, "ws") || js.Truthy(proxy.Get("ws-opts")):
			extras = append(extras, "ws=true")
			ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
			if v := objGet(ws, "path"); js.Truthy(v) {
				extras = append(extras, "ws-path="+s(v))
			}
			if h := objGet(objGet(ws, "headers"), "Host"); js.Truthy(h) {
				extras = append(extras, "ws-headers=Host:"+s(h))
			}
		case js.StrictEquals(network, "grpc"):
			extras = append(extras, "transport=grpc")
			g := js.Or(proxy.Get("grpc-opts"), proxy.Get("grpcOpts"))
			if v := objGet(g, "grpc-service-name"); js.Truthy(v) {
				extras = append(extras, "grpc-service-name="+s(v))
			}
		case js.StrictEquals(network, "h2"):
			extras = append(extras, "transport=h2")
			h2 := js.Or(proxy.Get("h2-opts"), proxy.Get("h2Opts"))
			if v := objGet(h2, "path"); js.Truthy(v) {
				extras = append(extras, "h2-path="+s(v))
			}
			if host := objGet(h2, "host"); js.Truthy(host) {
				if a, ok := host.(*js.Array); ok {
					host = a.At(0)
				}
				extras = append(extras, "h2-host="+s(host))
			}
		}
		if aid := proxy.Get("alterId"); js.StrictEquals(aid, 0.0) || js.IsNullish(aid) {
			extras = append(extras, "vmess-aead=true")
		}
		sni := sniOf(proxy)
		if js.Truthy(proxy.Get("tls")) || sni != nil {
			extras = append(extras, "tls=true")
		}
		if sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "skip-cert-verify=true")
		}
		return head("vmess") + ", " + strings.Join(extras, ", ")
	case "vless":
		return nil
	case "hysteria2", "hy2":
		extras := []string{"password=" + s(js.Or(proxy.Get("password"), ""))}
		if sni := sniOf(proxy); sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		return head("hysteria2") + ", " + strings.Join(extras, ", ")
	case "tuic":
		var extras []string
		if sni := sniOf(proxy); sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		if v := proxy.Get("congestion-control"); js.Truthy(v) {
			extras = append(extras, "congestion-control="+s(v))
		}
		if v := proxy.Get("congestion-controller"); js.Truthy(v) {
			extras = append(extras, "congestion-control="+s(v))
		}
		if js.StrictEquals(proxy.Get("udp-relay-mode"), "native") {
			extras = append(extras, "udp-relay=true")
		}
		if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
			extras = append(extras, "alpn="+s(joinIfArray(alpn)))
		}
		if uuid := proxy.Get("uuid"); js.Truthy(uuid) {
			token := s(uuid)
			if js.Truthy(proxy.Get("password")) {
				token = s(uuid) + ":" + s(proxy.Get("password"))
			}
			extras = append([]string{"token=" + token}, extras...)
		} else if js.Truthy(proxy.Get("password")) {
			extras = append([]string{"token=" + s(proxy.Get("password"))}, extras...)
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "skip-cert-verify=true")
		}
		return head("tuic") + ", " + strings.Join(extras, ", ")
	case "wireguard":
		if !js.Truthy(proxy.Get("private-key")) || !js.Truthy(proxy.Get("public-key")) || (!js.Truthy(proxy.Get("ip")) && !js.Truthy(proxy.Get("ipv6"))) {
			return nil
		}
		return s(name) + " = wireguard, section-name=" + getWireGuardSectionName(name)
	case "http", "https", "socks5":
		var extras []string
		if v := proxy.Get("username"); js.Truthy(v) {
			extras = append(extras, "username="+s(v))
		}
		if v := proxy.Get("password"); js.Truthy(v) {
			extras = append(extras, "password="+s(v))
		}
		if typ == "https" {
			extras = append(extras, "tls=true")
		}
		kind := "http"
		if typ == "socks5" {
			kind = "socks5"
		}
		out := head(kind)
		if len(extras) > 0 {
			out += ", " + strings.Join(extras, ", ")
		}
		return out
	case "anytls":
		extras := []string{"password=" + s(js.Or(proxy.Get("password"), ""))}
		if sni := sniOf(proxy); sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
			extras = append(extras, "alpn="+s(joinIfArray(alpn)))
		}
		if isTrue(proxy.Get("skip-cert-verify")) {
			extras = append(extras, "skip-cert-verify=true")
		}
		return head("anytls") + ", " + strings.Join(extras, ", ")
	}
	return nil
}

var reWGSectionUnsafe = js.MustRegExp(`[^a-zA-Z0-9_一-龥-]`, "g")

func getWireGuardSectionName(name any) string {
	return "WG_" + reWGSectionUnsafe.Replace(s(js.Or(name, "WireGuard")), "_")
}

func findInArray(a *js.Array, pred func(any) bool) any {
	for _, v := range a.Items {
		if pred(v) {
			return v
		}
	}
	return nil
}

func surgeWireGuardSection(proxy *js.Object) any {
	if proxy == nil || js.ToLower(s(js.Or(proxy.Get("type"), ""))) != "wireguard" {
		return nil
	}
	if !js.Truthy(proxy.Get("server")) || !js.Truthy(proxy.Get("port")) || !js.Truthy(proxy.Get("private-key")) ||
		!js.Truthy(proxy.Get("public-key")) || (!js.Truthy(proxy.Get("ip")) && !js.Truthy(proxy.Get("ipv6"))) {
		return nil
	}
	name := js.Or(proxy.Get("name"), "Untitled")
	lines := []string{"[WireGuard " + getWireGuardSectionName(name) + "]", "private-key = " + s(proxy.Get("private-key"))}
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
		dns := d
		if a, ok := d.(*js.Array); ok {
			dns = a.Join(", ")
		}
		if js.Truthy(dns) {
			lines = append(lines, "dns-server = "+s(dns))
		}
	}
	if v := proxy.Get("mtu"); js.Truthy(v) {
		lines = append(lines, "mtu = "+s(v))
	}
	peer := []string{"public-key = " + s(proxy.Get("public-key"))}
	var allowed string
	if a := proxy.Get("allowed-ips"); js.Truthy(a) {
		allowed = s(joinIfArray(a))
	} else if js.Truthy(proxy.Get("ipv6")) {
		allowed = "0.0.0.0/0, ::/0"
	} else {
		allowed = "0.0.0.0/0"
	}
	if strings.Contains(allowed, ",") {
		peer = append(peer, `allowed-ips = "`+allowed+`"`)
	} else {
		peer = append(peer, "allowed-ips = "+allowed)
	}
	server := s(proxy.Get("server"))
	endpoint := server
	if strings.Contains(server, ":") && !strings.HasPrefix(server, "[") {
		endpoint = "[" + server + "]"
	}
	peer = append(peer, "endpoint = "+endpoint+":"+s(proxy.Get("port")))
	if v := proxy.Get("preshared-key"); js.Truthy(v) {
		peer = append(peer, "preshared-key = "+s(v))
	}
	if v := proxy.Get("persistent-keepalive"); js.Truthy(v) {
		peer = append(peer, "keepalive = "+s(v))
	}
	if v := proxy.Get("reserved"); js.Truthy(v) {
		clientID := v
		if a, ok := v.(*js.Array); ok {
			clientID = a.Join("/")
		}
		peer = append(peer, "client-id = "+s(clientID))
	}
	lines = append(lines, "peer = ("+strings.Join(peer, ", ")+")")
	return strings.Join(lines, "\n")
}

var autoSelectTypes = map[string]bool{"url-test": true, "fallback": true, "load-balance": true}

// groupMembers 返回过滤空值后的成员；自动测速类分组会去掉 DIRECT / REJECT 等内置策略。
func groupMembers(group *js.Object, typ string) []string {
	var out []string
	for _, m := range arrayOrEmpty(group.Get("members")).Items {
		if !js.Truthy(m) {
			continue
		}
		if autoSelectTypes[typ] {
			switch js.ToUpper(s(m)) {
			case "DIRECT", "REJECT", "REJECT-DROP", "PASS":
				continue
			}
		}
		out = append(out, s(m))
	}
	return out
}

func groupFilter(group *js.Object) string {
	if f, ok := group.Get("filters").(*js.Array); ok && f.Len() > 0 {
		return f.Join("|")
	}
	return ""
}

func surgeProxyGroupLine(group *js.Object) string {
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
		var base string
		if filter != "" {
			base = name + " = " + typ + ", policy-path=" + filter + ", url=" + urlV + ", interval=" + interval
		} else {
			base = name + " = " + typ + ", " + members + ", url=" + urlV + ", interval=" + interval
		}
		if js.Truthy(tolerance) {
			return base + ", tolerance=" + s(tolerance)
		}
		return base
	case "load-balance":
		if filter != "" {
			return name + " = load-balance, policy-path=" + filter + ", url=" + urlV + ", interval=" + interval
		}
		return name + " = load-balance, " + members
	}
	return name + " = select, " + members
}

func surgeRuleLine(rule *js.Object) any {
	typ := js.ToUpper(s(js.Or(rule.Get("type"), "")))
	switch typ {
	case "":
		return nil
	case "RULE-SET":
		return "RULE-SET," + s(rule.Get("value")) + "," + s(rule.Get("policy"))
	case "MATCH", "FINAL":
		return "FINAL," + s(rule.Get("policy"))
	case "GEOIP":
		return "GEOIP," + s(js.Or(rule.Get("value"), "CN")) + "," + s(rule.Get("policy"))
	}
	return typ + "," + s(rule.Get("value")) + "," + s(rule.Get("policy"))
}

// nodeListURLs 解析 options.nodeList（按行，忽略空行与 # 注释）。
func nodeListURLs(options *js.Object) []string {
	nodeList, _ := options.Get("nodeList").(string)
	var out []string
	for _, l := range strings.Split(nodeList, "\n") {
		if l = js.Trim(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// modelProxies 返回模型中的节点；为空时按 options.nodeList 现场转换。
func modelProxies(model *js.Object, options *js.Object) []*js.Object {
	if a, ok := model.Get("proxies").(*js.Array); ok && a.Len() > 0 {
		out := make([]*js.Object, 0, a.Len())
		for _, p := range a.Items {
			out = append(out, objOrEmpty(p))
		}
		return out
	}
	return URLsToClashProxies(nodeListURLs(options), nil)
}

func groupsWithMembers(model *js.Object) []*js.Object {
	var out []*js.Object
	for _, g := range arrayOrEmpty(model.Get("groups")).Items {
		if m, ok := objGet(g, "members").(*js.Array); ok && m.Len() > 0 {
			out = append(out, objOrEmpty(g))
		}
	}
	return out
}

func appendAny(lines []string, vals ...any) []string {
	for _, v := range vals {
		if js.Truthy(v) {
			lines = append(lines, s(v))
		}
	}
	return lines
}

// RenderSurgeFromTemplateModel 对应 renderSurgeFromTemplateModel。
func RenderSurgeFromTemplateModel(modelVal any, options *js.Object) string {
	model := NormalizeUnifiedTemplateModel(modelVal)
	proxies := modelProxies(model, options)
	lines := []string{
		"[General]",
		"loglevel = notify",
		"dns-server = 223.5.5.5, 119.29.29.29",
		"encrypted-dns-server = h3://223.6.6.6/dns-query",
		"skip-proxy = 127.0.0.1, 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, 100.64.0.0/10, localhost, *.local, elpass.app, e.crashlytics.com, captive.apple.com, ::ffff:0:0:0:0/1, ::ffff:128:0:0:0/1, fe80::/10, fc00::/7, ::1/128",
		"bypass-tun = 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, 100.64.0.0/10",
		"ipv6 = true",
		"ipv6-vif = auto",
		"internet-test-url = http://www.apple.com/library/test/success.html",
		"proxy-test-url = http://www.gstatic.com/generate_204",
		"test-timeout = 5",
		"",
		"[Proxy]",
	}
	for _, p := range proxies {
		lines = appendAny(lines, surgeProxyLine(p))
	}
	lines = append(lines, "")
	var wg []string
	for _, p := range proxies {
		if js.ToLower(s(js.Or(p.Get("type"), ""))) == "wireguard" {
			if sec := surgeWireGuardSection(p); js.Truthy(sec) {
				wg = append(wg, s(sec))
			}
		}
	}
	if block := strings.Join(wg, "\n\n"); block != "" {
		lines = append(lines, block, "")
	}
	lines = append(lines, "[Proxy Group]")
	for _, g := range groupsWithMembers(model) {
		lines = append(lines, surgeProxyGroupLine(g))
	}
	lines = append(lines, "", "[Rule]")
	for _, r := range arrayOrEmpty(model.Get("rules")).Items {
		lines = appendAny(lines, surgeRuleLine(objOrEmpty(r)))
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}
