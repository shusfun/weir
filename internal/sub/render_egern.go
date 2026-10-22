package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/template-renderers/render-egern.js

var (
	reCRLFChars = js.MustRegExp(`[\r\n]`, "g")
	reHyphenAll = js.MustRegExp(`-`, "g")
	reHTTPURL   = js.MustRegExp(`^https?:\/\/`, "i")
)

// egernS 对应 render-egern.js 的 s()：去掉换行并 trim，null/undefined 视为空串。
func egernS(v any) string {
	if js.IsNullish(v) {
		return ""
	}
	return js.Trim(reCRLFChars.Replace(js.ToString(v), ""))
}

func egernSkipTLS(p *js.Object) bool {
	return js.Truthy(p.Get("skip-cert-verify")) || js.Truthy(p.Get("skipCertVerify"))
}

func egernSNI(p *js.Object) string {
	return egernS(js.Coalesce(p.Get("servername"), p.Get("sni"), p.Get("server")))
}

func egernMapTransport(proxy *js.Object) any {
	network := js.ToLower(js.ToString(js.Or(proxy.Get("network"), "tcp")))
	transport := js.NewObject()
	var tls *js.Object
	if js.Truthy(proxy.Get("tls")) || js.Truthy(proxy.Get("reality-opts")) {
		tls = js.Obj("skip_tls_verify", egernSkipTLS(proxy), "sni", egernSNI(proxy))
	}
	wsOpts := proxy.Get("ws-opts")
	switch network {
	case "ws", "websocket":
		ws := js.Obj("path", egernS(js.Or(objGet(wsOpts, "path"), "/")), "headers", js.Or(objGet(wsOpts, "headers"), js.NewObject()))
		if tls != nil {
			transport.Set("wss", ws.Assign(tls))
		} else {
			transport.Set("ws", ws)
		}
	case "grpc":
		g := proxy.Get("grpc-opts")
		transport.Set("grpc", js.Obj("service_name", egernS(js.Or(objGet(g, "grpc-service-name"), objGet(g, "service-name"), objGet(g, "serviceName"), "grpc"))))
		if tls != nil {
			transport.Set("tls", tls)
		}
	case "h2", "http2":
		h2 := proxy.Get("h2-opts")
		var hosts *js.Array
		if a, ok := objGet(h2, "host").(*js.Array); ok {
			hosts = js.Arr()
			for _, h := range a.Items {
				hosts.Push(egernS(h))
			}
		} else {
			hosts = js.Arr(egernS(js.Or(objGet(h2, "host"), proxy.Get("server"))))
		}
		transport.Set("h2", js.Obj("path", egernS(js.Or(objGet(h2, "path"), "/")), "host", hosts))
		if tls != nil {
			transport.Set("tls", tls)
		}
	default:
		if tls != nil {
			transport.Set("tls", tls)
		}
	}
	if t, ok := transport.Get("tls").(*js.Object); ok && js.Truthy(proxy.Get("reality-opts")) {
		ro := proxy.Get("reality-opts")
		t.Set("reality", js.Obj(
			"public_key", egernS(js.Or(objGet(ro, "public-key"), objGet(ro, "publicKey"))),
			"short_id", egernS(js.Or(objGet(ro, "short-id"), objGet(ro, "shortId"))),
		))
	}
	if transport.Len() > 0 {
		return transport
	}
	return nil
}

func egernMapProxy(proxy *js.Object) *js.Object {
	typ := js.ToLower(egernS(proxy.Get("type")))
	name, server := egernS(proxy.Get("name")), egernS(proxy.Get("server"))
	password, uuid := egernS(proxy.Get("password")), egernS(proxy.Get("uuid"))
	port := proxy.Get("port")
	switch typ {
	case "trojan":
		t := js.Obj("name", name, "server", server, "port", port, "password", password, "skip_tls_verify", egernSkipTLS(proxy))
		if sni := egernSNI(proxy); sni != "" {
			t.Set("sni", sni)
		}
		if network := proxy.Get("network"); js.StrictEquals(network, "ws") || js.StrictEquals(network, "websocket") {
			ws := js.Or(proxy.Get("ws-opts"), proxy.Get("wsOpts"))
			w := js.Obj("enabled", true, "path", egernS(js.Or(objGet(ws, "path"), "/")))
			if host := egernS(js.Or(objGet(objGet(ws, "headers"), "Host"), objGet(ws, "host"))); host != "" {
				w.Set("host", host)
			}
			t.Set("websocket", w)
		} else if tr := egernMapTransport(proxy); tr != nil {
			t.Set("transport", tr)
		}
		return js.Obj("trojan", t)
	case "vless":
		v := js.Obj("name", name, "server", server, "port", port, "user_id", uuid)
		if flow := proxy.Get("flow"); js.Truthy(flow) {
			if strings.Contains(js.ToString(flow), "vision") {
				v.Set("flow", "xtls-rprx-vision")
			} else {
				v.Set("flow", egernS(flow))
			}
		}
		if tr := egernMapTransport(proxy); tr != nil {
			v.Set("transport", tr)
		}
		return js.Obj("vless", v)
	case "vmess":
		v := js.Obj("name", name, "server", server, "port", port, "user_id", uuid, "security", egernS(js.Or(proxy.Get("cipher"), "auto")))
		if tr := egernMapTransport(proxy); tr != nil {
			v.Set("transport", tr)
		}
		return js.Obj("vmess", v)
	case "ss", "shadowsocks":
		ss := js.Obj("name", name, "server", server, "port", port, "method", egernS(js.Or(proxy.Get("cipher"), proxy.Get("method"))), "password", password)
		po := proxy.Get("plugin-opts")
		if js.StrictEquals(proxy.Get("plugin"), "obfs") {
			ss.Set("obfs", egernS(js.Or(objGet(po, "mode"), "http")))
			ss.Set("obfs_host", egernS(js.Or(objGet(po, "host"), proxy.Get("server"))))
		} else if js.StrictEquals(proxy.Get("plugin"), "v2ray-plugin") || js.StrictEquals(objGet(po, "mode"), "websocket") {
			opts := js.Or(proxy.Get("plugin-opts"), proxy.Get("pluginOpts"), js.NewObject())
			isTLS := js.Truthy(objGet(opts, "tls")) || js.StrictEquals(objGet(opts, "mode"), "websocket-tls")
			headers := js.NewObject()
			if h := objGet(opts, "host"); js.Truthy(h) {
				headers = js.Obj("Host", egernS(h))
			}
			if isTLS {
				ss.Set("transport", js.Obj("wss", js.Obj("path", egernS(js.Or(objGet(opts, "path"), "/")), "headers", headers,
					"sni", egernS(js.Coalesce(proxy.Get("servername"), proxy.Get("sni"), objGet(opts, "host"), proxy.Get("server"))),
					"skip_tls_verify", egernSkipTLS(proxy))))
			} else {
				ss.Set("transport", js.Obj("ws", js.Obj("path", egernS(js.Or(objGet(opts, "path"), "/")), "headers", headers)))
			}
		}
		return js.Obj("shadowsocks", ss)
	case "hysteria2", "hy2":
		h := js.Obj("name", name, "server", server, "port", port, "auth", password, "skip_tls_verify", egernSkipTLS(proxy))
		if sni := egernSNI(proxy); sni != "" {
			h.Set("sni", sni)
		}
		return js.Obj("hysteria2", h)
	case "tuic":
		t := js.Obj("name", name, "server", server, "port", port, "uuid", uuid, "password", password, "version", 5.0,
			"congestion_control", egernS(js.Or(proxy.Get("congestion-control"), "cubic")), "skip_tls_verify", egernSkipTLS(proxy))
		if sni := egernSNI(proxy); sni != "" {
			t.Set("sni", sni)
		}
		if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
			if a, ok := alpn.(*js.Array); ok {
				t.Set("alpn", a)
			} else {
				t.Set("alpn", js.Arr(egernS(alpn)))
			}
		}
		if v := proxy.Get("udp-relay-mode"); js.Truthy(v) {
			t.Set("udp_relay_mode", egernS(v))
		}
		return js.Obj("tuic", t)
	case "anytls":
		a := js.Obj("name", name, "server", server, "port", port, "password", password, "udp_relay", js.Truthy(proxy.Get("udp")))
		if sni := egernSNI(proxy); sni != "" {
			a.Set("sni", sni)
		}
		return js.Obj("anytls", a)
	}
	inner := js.Obj("name", name, "server", server, "port", port)
	if js.Truthy(proxy.Get("tfo")) {
		inner.Set("tcp_fast_open", true)
	}
	return js.Obj(typ, inner)
}

func egernMapPolicyGroup(group *js.Object) *js.Object {
	typ := js.ToLower(egernS(js.Or(group.Get("type"), "select")))
	policies := js.Arr()
	if a, ok := group.Get("members").(*js.Array); ok {
		for _, m := range truthyItems(a) {
			policies.Push(egernS(m))
		}
	}
	opts := group.Get("options")
	numOr := func(v any, d float64) float64 {
		if n := js.ToNumber(v); js.Truthy(n) {
			return n
		}
		return d
	}
	switch typ {
	case "url-test", "urltest", "auto-test":
		return js.Obj("auto_test", js.Obj("name", egernS(group.Get("name")), "policies", policies,
			"interval", numOr(objGet(opts, "interval"), 600), "tolerance", numOr(objGet(opts, "tolerance"), 100)))
	case "fallback":
		return js.Obj("fallback", js.Obj("name", egernS(group.Get("name")), "policies", policies,
			"interval", numOr(objGet(opts, "interval"), 600)))
	}
	return js.Obj("select", js.Obj("name", egernS(group.Get("name")), "policies", policies))
}

func egernMapRule(rule *js.Object) *js.Object {
	typ := js.ToLower(egernS(rule.Get("type")))
	policy := egernS(js.Or(rule.Get("policy"), "DIRECT"))
	value := egernS(rule.Get("value"))
	if typ == "final" || typ == "match" {
		return js.Obj("default", js.Obj("policy", policy))
	}
	if typ == "rule-set" && reHTTPURL.Test(value) {
		return js.Obj("rule_set", js.Obj("match", value, "policy", policy, "update_interval", 86400.0))
	}
	return js.Obj(reHyphenAll.Replace(typ, "_"), js.Obj("match", value, "policy", policy))
}

// RenderEgernFromTemplateModel 对应 renderEgernFromTemplateModel（输出 Egern YAML）。
func RenderEgernFromTemplateModel(modelVal any) (string, error) {
	model := NormalizeUnifiedTemplateModel(modelVal)
	proxies := js.Arr()
	for _, p := range arrayOrEmpty(model.Get("proxies")).Items {
		proxies.Push(egernMapProxy(objOrEmpty(p)))
	}
	groups := js.Arr()
	for _, g := range groupsWithMembers(model) {
		groups.Push(egernMapPolicyGroup(g))
	}
	rules := js.Arr()
	for _, r := range arrayOrEmpty(model.Get("rules")).Items {
		rules.Push(egernMapRule(objOrEmpty(r)))
	}
	config := js.Obj(
		"general", js.Obj("dns_server", strs2("223.5.5.5", "114.114.114.114"), "ipv6", false),
		"proxies", proxies,
		"policy_groups", groups,
		"rules", rules,
	)
	settings := model.Get("settings")
	if mc := objGet(settings, "managedConfigUrl"); js.Truthy(mc) {
		config.Set("auto_update", js.Obj("url", egernS(mc), "interval", js.Or(objGet(settings, "interval"), 86400.0)))
	}
	return js.YAMLDump(config, js.YAMLDumpOptions{DoubleQuote: true, ForceQuotes: true})
}
