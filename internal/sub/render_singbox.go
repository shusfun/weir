package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/template-renderers/render-singbox.js

func sanitizeTag(v any) string {
	if t := js.Trim(strOr(v)); t != "" {
		return t
	}
	return "Untitled"
}

// parsePortSB 对应 Number.parseInt(String(port), 10)，非有限值返回 nil（undefined）。
func parsePortSB(port any) any {
	n := js.ParseInt(js.ToString(port), 10)
	if js.IsFinite(n) {
		return n
	}
	return nil
}

func sbTLS(proxy *js.Object, server any) *js.Object {
	return js.Obj("enabled", true, "server_name", js.Coalesce(proxy.Get("servername"), proxy.Get("sni"), server), "insecure", skipCertVerify(proxy))
}

func arrayWrap(v any) *js.Array {
	if a, ok := v.(*js.Array); ok {
		return a
	}
	return js.Arr(v)
}

func sbBuildOutbound(proxy *js.Object) any {
	if proxy == nil || !js.Truthy(proxy.Get("server")) || !js.Truthy(proxy.Get("port")) {
		return nil
	}
	typ := js.ToLower(strOr(proxy.Get("type")))
	tag := sanitizeTag(proxy.Get("name"))
	server := proxy.Get("server")
	serverPort := parsePortSB(proxy.Get("port"))
	if !js.Truthy(serverPort) {
		return nil
	}
	hasTLSHint := js.Truthy(proxy.Get("tls")) || js.Truthy(proxy.Get("sni")) || js.Truthy(proxy.Get("servername"))
	switch typ {
	case "http", "https":
		ob := js.Obj("tag", tag, "type", "http", "server", server, "server_port", serverPort,
			"username", js.Or(proxy.Get("username"), nil), "password", js.Or(proxy.Get("password"), nil))
		if !js.Truthy(proxy.Get("username")) {
			ob.Set("username", nil)
		}
		if !js.Truthy(proxy.Get("password")) {
			ob.Set("password", nil)
		}
		if typ == "https" || hasTLSHint {
			ob.Set("tls", sbTLS(proxy, server))
		}
		return ob
	case "trojan":
		return js.Obj("tag", tag, "type", "trojan", "server", server, "server_port", serverPort,
			"password", js.Or(proxy.Get("password"), ""), "tls", sbTLS(proxy, server))
	case "ss", "shadowsocks":
		ob := js.Obj("tag", tag, "type", "shadowsocks", "server", server, "server_port", serverPort,
			"method", js.Or(proxy.Get("cipher"), "aes-128-gcm"), "password", js.Or(proxy.Get("password"), ""))
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
		return ob
	case "vmess":
		alterID := 0.0
		if n := js.ToNumber(proxy.Get("alterId")); js.IsFinite(n) {
			alterID = n
		}
		ob := js.Obj("tag", tag, "type", "vmess", "server", server, "server_port", serverPort, "uuid", js.Or(proxy.Get("uuid"), ""),
			"security", js.Or(proxy.Get("cipher"), "auto"), "congestion_control", js.Or(proxy.Get("congestion-control"), "cubic"), "alter_id", alterID)
		network := js.Or(proxy.Get("network"), "")
		switch {
		case js.StrictEquals(network, "ws"):
			ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
			ob.Set("transport", js.Obj("type", "ws", "path", js.Or(objGet(ws, "path"), "/"), "headers", js.Or(objGet(ws, "headers"), js.NewObject())))
		case js.StrictEquals(network, "grpc"):
			g := js.Or(proxy.Get("grpc-opts"), proxy.Get("grpcOpts"))
			ob.Set("transport", js.Obj("type", "grpc", "service_name", js.Or(objGet(g, "grpc-service-name"), objGet(g, "serviceName"), "grpc")))
		case js.StrictEquals(network, "h2") || js.StrictEquals(network, "http"):
			ns := s(network)
			opts := js.Or(proxy.Get(ns+"-opts"), proxy.Get(ns+"Opts"))
			var host any = js.Arr()
			if h := objGet(opts, "host"); js.Truthy(h) {
				host = arrayWrap(h)
			}
			t := "http"
			if ns == "h2" {
				t = "h2"
			}
			ob.Set("transport", js.Obj("type", t, "host", host, "path", js.Or(objGet(opts, "path"), "/")))
		}
		if hasTLSHint {
			tls := sbTLS(proxy, server)
			if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
				tls.Set("alpn", arrayWrap(alpn))
			}
			ob.Set("tls", tls)
		}
		return ob
	case "vless":
		ob := js.Obj("tag", tag, "type", "vless", "server", server, "server_port", serverPort, "uuid", js.Or(proxy.Get("uuid"), ""))
		if v := proxy.Get("flow"); js.Truthy(v) {
			ob.Set("flow", v)
		}
		network := js.Or(proxy.Get("network"), "")
		switch {
		case js.StrictEquals(network, "ws"):
			ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
			ob.Set("transport", js.Obj("type", "ws", "path", js.Or(objGet(ws, "path"), "/"), "headers", js.Or(objGet(ws, "headers"), js.NewObject())))
		case js.StrictEquals(network, "grpc"):
			g := js.Or(proxy.Get("grpc-opts"), proxy.Get("grpcOpts"))
			ob.Set("transport", js.Obj("type", "grpc", "service_name", js.Or(objGet(g, "grpc-service-name"), objGet(g, "serviceName"), "grpc")))
		case js.StrictEquals(network, "httpupgrade"):
			h := js.Or(proxy.Get("httpupgrade-opts"), proxy.Get("httpupgradeOpts"))
			ob.Set("transport", js.Obj("type", "httpupgrade", "path", js.Or(objGet(h, "path"), "/"), "host", js.Or(objGet(h, "host"), "")))
		}
		if hasTLSHint {
			tls := sbTLS(proxy, server)
			if fp := js.Or(proxy.Get("client-fingerprint"), proxy.Get("clientFingerprint"), proxy.Get("fp")); js.Truthy(fp) {
				tls.Set("utls", js.Obj("enabled", true, "fingerprint", fp))
			}
			if ro := js.Or(proxy.Get("reality-opts"), proxy.Get("realityOpts")); js.Truthy(ro) {
				reality := js.Obj("enabled", true, "public_key", js.Or(objGet(ro, "public-key"), objGet(ro, "publicKey"), ""),
					"short_id", js.Or(objGet(ro, "short-id"), objGet(ro, "shortId"), ""))
				if sx := js.Or(objGet(ro, "spider-x"), objGet(ro, "spiderX")); js.Truthy(sx) {
					reality.Set("spider_x", sx)
				}
				tls.Set("reality", reality)
			}
			ob.Set("tls", tls)
		}
		return ob
	case "hysteria2", "hy2":
		return js.Obj("tag", tag, "type", "hysteria2", "server", server, "server_port", serverPort,
			"password", js.Or(proxy.Get("password"), ""), "tls", sbTLS(proxy, server))
	case "tuic":
		ob := js.Obj("tag", tag, "type", "tuic", "server", server, "server_port", serverPort, "uuid", js.Or(proxy.Get("uuid"), ""),
			"password", js.Or(proxy.Get("password"), ""), "tls", sbTLS(proxy, server))
		if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
			ob.Get("tls").(*js.Object).Set("alpn", arrayWrap(alpn))
		}
		if cc := js.Or(proxy.Get("congestion-control"), proxy.Get("congestion-controller"), proxy.Get("congestion")); js.Truthy(cc) {
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
		return ob
	case "wireguard":
		var local any = js.Arr()
		if ip := proxy.Get("ip"); js.IsArray(ip) {
			local = ip
		} else if js.Truthy(ip) {
			local = js.Arr(ip)
		}
		return js.Obj("tag", tag, "type", "wireguard", "server", server, "server_port", serverPort, "local_address", local,
			"private_key", js.Or(proxy.Get("private-key"), ""), "peer_public_key", js.Or(proxy.Get("public-key"), ""),
			"pre_shared_key", js.Or(proxy.Get("preshared-key"), ""))
	}
	return nil
}

func mapSingboxGroupType(t any) string {
	switch js.ToLower(strOr(t)) {
	case "url-test":
		return "urltest"
	}
	return "selector"
}

func sbGroupOutbounds(groups []*js.Object) []any {
	var out []any
	for _, g := range groups {
		mapped := mapSingboxGroupType(g.Get("type"))
		raw := truthyItems(arrayOrEmpty(g.Get("members")))
		members := js.Arr()
		for _, m := range raw {
			if mapped == "urltest" {
				switch js.ToUpper(s(m)) {
				case "DIRECT", "REJECT", "REJECT-DROP", "PASS":
					continue
				}
			}
			members.Push(m)
		}
		ob := js.Obj("tag", sanitizeTag(g.Get("name")), "type", mapped, "outbounds", members)
		if mapped == "urltest" {
			opts := g.Get("options")
			ob.Set("url", js.Or(objGet(opts, "url"), gstaticURL))
			ob.Set("interval", s(js.Or(objGet(opts, "interval"), 300.0))+"s")
		}
		if members.Len() > 0 {
			ob.Set("default", members.At(0))
		}
		out = append(out, ob)
	}
	return out
}

func mapRuleToSingbox(rule *js.Object) any {
	typ := js.ToLower(strOr(rule.Get("type")))
	policy := rule.Get("policy")
	switch typ {
	case "rule-set":
		return js.Obj("rule_set", sanitizeTag(s(policy)+"_"+s(rule.Get("value"))), "outbound", policy)
	case "geoip", "geosite":
		return js.Obj("rule_set", js.Arr(typ+"-"+js.ToLower(s(js.Or(rule.Get("value"), "cn")))), "outbound", policy)
	case "match", "final":
		return js.Obj("outbound", policy)
	case "domain-suffix":
		return js.Obj("domain_suffix", js.Arr(rule.Get("value")), "outbound", policy)
	case "domain-keyword":
		return js.Obj("domain_keyword", js.Arr(rule.Get("value")), "outbound", policy)
	}
	return nil
}

func detectRuleSetFormat(u any) string {
	raw := js.ToLower(js.Trim(strOr(u)))
	if strings.HasSuffix(raw, ".srs") {
		return "binary"
	}
	return "source"
}

func sbBuildRuleSets(rules []any) []any {
	var out []any
	for _, r := range rules {
		ro := objOrEmpty(r)
		if js.ToLower(strOr(ro.Get("type"))) == "rule-set" && js.StrictEquals(ro.Get("source"), "remote") {
			out = append(out, js.Obj("tag", sanitizeTag(s(ro.Get("policy"))+"_"+s(ro.Get("value"))), "type", "remote",
				"format", detectRuleSetFormat(ro.Get("value")), "url", PinRemoteRuleURL(ro.Get("value"), true),
				"update_interval", "24h", "download_detour", DNSProxyGroup))
		}
	}
	seen := map[string]bool{}
	for _, r := range rules {
		ro := objOrEmpty(r)
		typ := js.ToLower(strOr(ro.Get("type")))
		if typ != "geoip" && typ != "geosite" {
			continue
		}
		value := js.ToLower(s(js.Or(ro.Get("value"), "cn")))
		tag := typ + "-" + value
		if seen[tag] {
			continue
		}
		seen[tag] = true
		var u string
		if typ == "geoip" {
			u = pinURL("https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-" + value + ".srs")
		} else {
			u = pinURL("https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-" + value + ".srs")
		}
		out = append(out, js.Obj("tag", tag, "type", "remote", "format", "binary", "url", u, "update_interval", "24h", "download_detour", DNSProxyGroup))
	}
	return out
}

// RenderSingboxFromTemplateModel 对应 renderSingboxFromTemplateModel（输出 sing-box JSON）。
func RenderSingboxFromTemplateModel(modelVal any, options *js.Object) string {
	model := NormalizeUnifiedTemplateModel(modelVal)
	proxies := modelProxies(model, options)
	outbounds := js.Arr(js.Obj("tag", "DIRECT", "type", "direct"), js.Obj("tag", "REJECT", "type", "block"))
	for _, p := range proxies {
		if ob := sbBuildOutbound(p); ob != nil {
			outbounds.Push(ob)
		}
	}
	outbounds.Push(sbGroupOutbounds(groupsWithMembers(model))...)
	rules := arrayOrEmpty(model.Get("rules")).Items
	ruleSets := js.Arr(GetSingboxDNSRuleSet())
	for _, rs := range sbBuildRuleSets(rules) {
		if !js.StrictEquals(objGet(rs, "tag"), SingboxCNRuleSet) {
			ruleSets.Push(rs)
		}
	}
	routeRules := js.Arr()
	for _, r := range rules {
		if m := mapRuleToSingbox(objOrEmpty(r)); m != nil {
			routeRules.Push(m)
		}
	}
	defaultOutbound := any("DIRECT")
	for _, g := range arrayOrEmpty(model.Get("groups")).Items {
		if !js.StrictEquals(objGet(g, "name"), DNSProxyGroup) {
			defaultOutbound = js.Or(objGet(g, "name"), "DIRECT")
			break
		}
	}
	settings := model.Get("settings")
	dns := BuildSingboxDNSConfig(objGet(settings, "customDnsOverride"), SafeDNSOptions{Mode: objGet(settings, "dnsMode"), ProxyGroup: DNSProxyGroup})
	resolver := any("dns-cn-1")
	if first := objGet(dns.Get("servers"), "0"); first != nil {
		resolver = js.Or(objGet(first, "tag"), "dns-cn-1")
	}
	config := js.Obj(
		"log", js.Obj("level", "info"),
		"dns", dns,
		"inbounds", js.Arr(js.Obj("type", "tun", "tag", "tun-in", "address", strs2("172.19.0.1/30"), "auto_route", true, "strict_route", true, "stack", "mixed")),
		"outbounds", outbounds,
		"route", js.Obj("auto_detect_interface", true, "default_domain_resolver", resolver, "final", defaultOutbound,
			"rule_set", ruleSets, "rules", routeRules),
	)
	out, _ := js.JSONStringifyIndent(config, "  ")
	return out + "\n"
}
