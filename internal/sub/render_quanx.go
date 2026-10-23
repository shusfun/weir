package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/template-renderers/render-quanx.js

func withExtras(base string, extras []string) string {
	if len(extras) > 0 {
		return base + ", " + strings.Join(extras, ", ")
	}
	return base
}

func normalizeQxVmessMethod(method any) string {
	n := js.ToLower(js.Trim(strOr(method)))
	if n == "" || n == "auto" {
		return "none"
	}
	return n
}

func quanxProxyLine(proxy *js.Object) any {
	typ := js.ToLower(s(js.Or(proxy.Get("type"), "")))
	name := js.Or(proxy.Get("name"), "Untitled")
	server, port := proxy.Get("server"), proxy.Get("port")
	if !js.Truthy(server) || !js.Truthy(port) {
		return nil
	}
	hp := s(server) + ":" + s(port)
	tag := ", tag=" + s(name)
	switch typ {
	case "trojan":
		var extras []string
		if js.StrictEquals(proxy.Get("network"), "ws") {
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
		if sni := sniOf(proxy); sni != nil {
			extras = append(extras, "tls-host="+s(sni))
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "tls-verification=false")
		}
		return withExtras("trojan="+hp+", password="+s(js.Or(proxy.Get("password"), "")), extras) + tag
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
		return withExtras("shadowsocks="+hp+", method="+s(js.Or(proxy.Get("cipher"), "aes-128-gcm"))+", password="+s(js.Or(proxy.Get("password"), "")), extras) + tag
	case "vmess":
		var extras []string
		sni := sniOf(proxy)
		hasTLS := js.Truthy(proxy.Get("tls")) || sni != nil
		if js.StrictEquals(proxy.Get("network"), "ws") {
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
			} else if sni != nil {
				extras = append(extras, "obfs-host="+s(sni))
			}
		} else {
			if hasTLS {
				extras = append(extras, "over-tls=true")
			}
			if sni != nil {
				extras = append(extras, "tls-host="+s(sni))
			}
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "tls-verification=false")
		}
		return withExtras("vmess="+hp+", method="+normalizeQxVmessMethod(proxy.Get("cipher"))+", password="+s(js.Or(proxy.Get("uuid"), "")), extras) + tag
	case "vless":
		extras := []string{"method=none"}
		transport := js.Or(proxy.Get("network"), "tcp")
		isReality := js.StrictEquals(proxy.Get("security"), "reality") || js.Truthy(proxy.Get("reality-opts"))
		hasTLS := js.Truthy(proxy.Get("tls")) || isReality
		hostValue := js.Coalesce(proxy.Get("sni"), proxy.Get("servername"))
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
			} else if hostValue != nil {
				extras = append(extras, "obfs-host="+s(hostValue))
			}
			if v := objGet(ws, "path"); js.Truthy(v) {
				extras = append(extras, "obfs-uri="+s(v))
			}
		case js.StrictEquals(transport, "grpc") || js.Truthy(proxy.Get("grpc-opts")):
			extras = append(extras, pick("obfs=over-tls", "obfs=grpc"))
			if hostValue != nil {
				extras = append(extras, "obfs-host="+s(hostValue))
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
			} else if hostValue != nil {
				extras = append(extras, "obfs-host="+s(hostValue))
			}
			if v := objGet(x, "path"); !hasTLS && js.Truthy(v) {
				extras = append(extras, "obfs-uri="+s(v))
			}
		case hasTLS:
			extras = append(extras, "obfs=over-tls")
			if hostValue != nil {
				extras = append(extras, "obfs-host="+s(hostValue))
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
		if skipCertVerify(proxy) {
			extras = append(extras, "tls-verification=false")
		}
		return withExtras("vless="+hp+", password="+s(js.Or(proxy.Get("uuid"), "")), extras) + tag
	case "http", "https":
		var extras []string
		if typ == "https" {
			extras = append(extras, "over-tls=true")
		}
		if sni := sniOf(proxy); sni != nil {
			extras = append(extras, "tls-host="+s(sni))
		}
		return withExtras("http="+hp+", username="+s(js.Or(proxy.Get("username"), ""))+", password="+s(js.Or(proxy.Get("password"), "")), extras) + tag
	case "hysteria2", "hy2":
		// Quantumult X 不接受 MiSub 输出的 Hysteria2 语法，与上游一样直接省略
		return nil
	case "tuic":
		var extras []string
		if v := proxy.Get("uuid"); js.Truthy(v) {
			extras = append(extras, s(v))
		}
		if v := proxy.Get("password"); js.Truthy(v) {
			extras = append(extras, s(v))
		}
		if sni := sniOf(proxy); sni != nil {
			extras = append(extras, "sni="+s(sni))
		}
		if cc := js.Or(proxy.Get("congestion-control"), proxy.Get("congestion-controller")); js.Truthy(cc) {
			extras = append(extras, "congestion-controller="+s(cc))
		}
		if v := proxy.Get("udp-relay-mode"); js.Truthy(v) {
			extras = append(extras, "udp-relay="+s(v))
		}
		if alpn := proxy.Get("alpn"); js.Truthy(alpn) {
			extras = append(extras, "alpn="+s(joinIfArray(alpn)))
		}
		if skipCertVerify(proxy) {
			extras = append(extras, "tls-verification=false")
		}
		return "tuic=" + hp + ", " + strings.Join(extras, ", ") + tag
	case "anytls":
		extras := []string{"password=" + s(js.Or(proxy.Get("password"), "")), "over-tls=true"}
		if skipCertVerify(proxy) {
			extras = append(extras, "tls-verification=false")
		} else {
			extras = append(extras, "tls-verification=true")
		}
		if sni := sniOf(proxy); sni != nil {
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
	return nil
}

func boolWord(v any) string {
	if js.Truthy(v) {
		return "true"
	}
	return "false"
}

func quanxPolicyLine(group *js.Object) string {
	typ := js.ToLower(s(js.Or(group.Get("type"), "select")))
	members := strings.Join(groupMembers(group, typ), ", ")
	opts := group.Get("options")
	tolerance := js.Or(objGet(opts, "tolerance"), 50.0)
	interval := js.Or(objGet(opts, "interval"), 300.0)
	name := s(group.Get("name"))
	switch typ {
	case "url-test", "url-latency-benchmark":
		return "url-latency-benchmark=" + name + ", " + members + ", check-interval=" + s(interval) + ", tolerance=" + s(tolerance)
	case "fallback", "available":
		return "available=" + name + ", " + members
	}
	return "static=" + name + ", " + members
}

func quanxRuleLine(rule *js.Object) any {
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

// RenderQuanxFromTemplateModel 对应 renderQuanxFromTemplateModel。
func RenderQuanxFromTemplateModel(modelVal any, options *js.Object) string {
	model := NormalizeUnifiedTemplateModel(modelVal)
	proxies := modelProxies(model, options)
	rules := arrayOrEmpty(model.Get("rules")).Items
	var remote, local []*js.Object
	for _, r := range rules {
		ro := objOrEmpty(r)
		v, isStr := ro.Get("value").(string)
		if js.ToUpper(s(ro.Get("type"))) == "RULE-SET" && isStr && strings.HasPrefix(v, "http") {
			remote = append(remote, ro)
		} else {
			local = append(local, ro)
		}
	}
	lines := []string{
		"[general]",
		"; 监听端口",
		"network_check_url=http://www.gstatic.com/generate_204",
		"server_check_url=http://www.gstatic.com/generate_204",
		"",
		"[dns]",
		"no-ipv6",
		"server = 223.5.5.5",
		"server = 114.114.114.114",
		"",
		"[server_local]",
	}
	for _, p := range proxies {
		lines = appendAny(lines, quanxProxyLine(p))
	}
	lines = append(lines, "", "[policy]")
	for _, g := range groupsWithMembers(model) {
		lines = append(lines, quanxPolicyLine(g))
	}
	lines = append(lines, "", "[filter_remote]")
	for _, r := range remote {
		lines = append(lines, "filter_remote, "+s(r.Get("value"))+", tag="+s(r.Get("policy"))+", force-policy="+s(r.Get("policy"))+", update-interval=86400, enabled=true")
	}
	lines = append(lines, "", "[filter_local]")
	for _, r := range local {
		lines = appendAny(lines, quanxRuleLine(r))
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}
