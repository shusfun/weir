package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/utils/url-to-clash.js

const virtualInfoNodePassword = "00000000-0000-0000-0000-000000000000"

var reVirtualInfoName = js.MustRegExp(`(?:流量剩余|到期时间|您的订阅已到期)`, "")

func isVirtualInfoProxy(proxy *js.Object) bool {
	if proxy == nil {
		return false
	}
	typ := js.ToLower(js.ToString(js.Or(proxy.Get("type"), "")))
	server := js.ToLower(js.Trim(js.ToString(js.Or(proxy.Get("server"), ""))))
	password := js.Trim(js.ToString(js.Or(proxy.Get("password"), "")))
	name := js.Trim(js.ToString(js.Or(proxy.Get("name"), "")))
	return typ == "trojan" && server == "127.0.0.1" && js.ToNumber(proxy.Get("port")) == 443 &&
		password == virtualInfoNodePassword && reVirtualInfoName.Test(name)
}

func parseQueryParams(u string) *js.URLSearchParams {
	queryIndex := strings.Index(u, "?")
	if queryIndex == -1 {
		return js.NewSearchParams()
	}
	hashIndex := strings.Index(u, "#")
	if hashIndex > queryIndex {
		return js.ParseSearchParams(u[queryIndex+1 : hashIndex])
	}
	return js.ParseSearchParams(u[queryIndex+1:])
}

func extractName(u string) string {
	hashIndex := strings.LastIndex(u, "#")
	if hashIndex == -1 {
		return ""
	}
	return js.SafeDecodeURIComponent(u[hashIndex+1:])
}

// base64DecodeLoose 对应 url-to-clash.js 的 base64Decode：失败时依次回退到纯 atob 与原文。
func base64DecodeLoose(s string) string {
	normalized := padBase64(urlSafeToStd(s))
	if bin, err := js.Atob(normalized); err == nil {
		if d, err := js.DecodeURIComponentEscape(bin); err == nil {
			return d
		}
	}
	if bin, err := js.Atob(urlSafeToStd(s)); err == nil {
		return bin
	}
	return s
}

// clashParseHostPort 对应 url-to-clash.js 的 parseHostPort。
func clashParseHostPort(hostPort string) (string, float64) {
	if strings.HasPrefix(hostPort, "[") {
		if closeBracket := strings.Index(hostPort, "]"); closeBracket != -1 {
			server := hostPort[1:closeBracket]
			after := hostPort[closeBracket+1:]
			port := 443.0
			if strings.HasPrefix(after, ":") {
				port = js.ParseInt(after[1:], 0)
			}
			return server, port
		}
	}
	parts := strings.Split(hostPort, ":")
	if len(parts) > 2 && parts[0] == "" {
		port := js.ParseInt(parts[len(parts)-1], 0)
		if !js.Truthy(port) {
			port = 443
		}
		return strings.Join(parts[:len(parts)-1], ":"), port
	}
	port := 443.0
	if len(parts) > 1 {
		if p := js.ParseInt(parts[1], 0); js.Truthy(p) {
			port = p
		}
	}
	return parts[0], port
}

// cutServerPart 截取 @ 之后、? 或 # 之前的 host:port 片段（与上游写法一致：优先按 ? 截断）。
func cutServerPart(serverPart string) string {
	queryIndex := strings.Index(serverPart, "?")
	hashIndex := strings.Index(serverPart, "#")
	if queryIndex != -1 {
		return serverPart[:queryIndex]
	} else if hashIndex != -1 {
		return serverPart[:hashIndex]
	}
	return serverPart
}

var reIPv6Bare = js.MustRegExp(`^[0-9a-f:]+$`, "i")

func applyReality(proxy *js.Object, params *js.URLSearchParams) {
	proxy.Set("tls", true)
	ro := js.NewObject()
	if v := params.Str("pbk"); v != "" {
		ro.Set("public-key", v)
	}
	if v := params.Str("sid"); v != "" {
		ro.Set("short-id", v)
	}
	if v := params.Str("spx"); v != "" {
		ro.Set("spider-x", v)
	}
	sxm := firstNonEmpty(params.GetValue("support-x25519mlkem768"), params.GetValue("support_x25519mlkem768"), params.GetValue("sxm"))
	if sxm == "1" || sxm == "true" {
		ro.Set("support-x25519mlkem768", true)
	}
	if ro.Len() > 0 {
		proxy.Set("reality-opts", ro)
	}
}

// firstNonEmpty 对应 `a || b || c`（URLSearchParams.get 返回字符串或 null），返回字符串（null 视为空串）。
func firstNonEmpty(vals ...any) string {
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	if s, ok := vals[len(vals)-1].(string); ok {
		return s
	}
	return ""
}

// sniFromParams 处理 sni 与 Shadowrocket 的 peer 参数。
func sniFromParams(proxy *js.Object, params *js.URLSearchParams) {
	if v := params.Str("sni"); v != "" {
		proxy.Set("servername", v)
		proxy.Set("sni", v)
	} else if v := params.Str("peer"); v != "" {
		proxy.Set("servername", v)
		proxy.Set("sni", v)
	}
}

func splitToArray(s, sep string) *js.Array { return js.StrArr(strings.Split(s, sep)) }

func parseVlessURL(u string) *js.Object {
	body := u[8:]
	atIndex := strings.Index(body, "@")
	if atIndex == -1 {
		return nil
	}
	uuid := body[:atIndex]
	serverPart := cutServerPart(body[atIndex+1:])
	if reIPv6Bare.Test(serverPart) && len(strings.Split(serverPart, ":")) > 2 {
		lastColon := strings.LastIndex(serverPart, ":")
		serverPart = "[" + serverPart[:lastColon] + "]:" + serverPart[lastColon+1:]
	}
	server, port := clashParseHostPort(serverPart)
	params := parseQueryParams(u)
	name := extractName(u)
	proxy := js.Obj("name", orStr(name, "VLESS-"+server), "type", "vless", "server", server, "port", port, "uuid", uuid)
	network := orStr(params.Str("type"), "tcp")
	if network != "tcp" {
		proxy.Set("network", network)
	}
	if network == "ws" {
		ws := js.NewObject()
		if v := params.Str("path"); v != "" {
			ws.Set("path", v)
		}
		if v := params.Str("host"); v != "" {
			ws.Set("headers", js.Obj("Host", v))
		}
		if ws.Len() > 0 {
			proxy.Set("ws-opts", ws)
		}
	}
	if network == "xhttp" {
		xo := js.NewObject()
		path := firstNonEmpty(params.GetValue("xhttp-path"), params.GetValue("path"))
		host := firstNonEmpty(params.GetValue("xhttp-host"), params.GetValue("host"), params.GetValue("sni"))
		if path != "" {
			xo.Set("path", path)
		}
		if host != "" {
			xo.Set("host", host)
			xo.Set("headers", js.Obj("Host", host))
		}
		if v := params.Str("mode"); v != "" {
			xo.Set("mode", v)
		}
		if extraRaw := params.Str("extra"); extraRaw != "" {
			if extraVal, err := js.JSONParse(extraRaw); err == nil {
				// hasOwnProperty 对 null/undefined 会抛出异常，被 catch 忽略
				if extra, ok := extraVal.(*js.Object); ok {
					for _, pair := range [][2]string{
						{"xPaddingObfsMode", "x-padding-obfs-mode"}, {"xPaddingMethod", "x-padding-method"},
						{"xPaddingPlacement", "x-padding-placement"}, {"xPaddingHeader", "x-padding-header"},
						{"xPaddingKey", "x-padding-key"},
					} {
						if extra.Has(pair[0]) {
							xo.Set(pair[1], extra.Get(pair[0]))
						}
					}
				}
			}
		}
		if xo.Len() > 0 {
			proxy.Set("xhttp-opts", xo)
		}
	}
	if network == "grpc" {
		g := js.NewObject()
		if v := params.Str("serviceName"); v != "" {
			g.Set("grpc-service-name", v)
		}
		if v := params.Str("mode"); v != "" {
			g.Set("grpc-mode", v)
		}
		if g.Len() > 0 {
			proxy.Set("grpc-opts", g)
		}
	}
	if network == "httpupgrade" {
		h := js.NewObject()
		if v := params.Str("path"); v != "" {
			h.Set("path", v)
		}
		if v := params.Str("host"); v != "" {
			h.Set("host", v)
		}
		if h.Len() > 0 {
			proxy.Set("httpupgrade-opts", h)
		}
	}
	security := orStr(params.Str("security"), "none")
	if security == "reality" {
		applyReality(proxy, params)
	} else if security == "tls" {
		proxy.Set("tls", true)
	}
	if params.Str("allowInsecure") == "1" || params.Str("insecure") == "1" {
		proxy.Set("skip-cert-verify", true)
	}
	sniFromParams(proxy, params)
	if v := params.Str("fp"); v != "" {
		proxy.Set("client-fingerprint", v)
	}
	if v := params.Str("flow"); v != "" {
		proxy.Set("flow", v)
	}
	if v := params.Str("alpn"); v != "" {
		proxy.Set("alpn", splitToArray(v, ","))
	}
	if v := params.Str("dp"); v != "" {
		proxy.Set("dialer-proxy", v)
	}
	return proxy
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func parseTrojanURL(u string) *js.Object {
	body := u[9:]
	atIndex := strings.Index(body, "@")
	if atIndex == -1 {
		return nil
	}
	password := js.SafeDecodeURIComponent(body[:atIndex])
	server, port := clashParseHostPort(cutServerPart(body[atIndex+1:]))
	params := parseQueryParams(u)
	name := extractName(u)
	proxy := js.Obj("name", orStr(name, "Trojan-"+server), "type", "trojan", "server", server, "port", port, "password", password)
	network := orStr(params.Str("type"), "tcp")
	if network != "tcp" {
		proxy.Set("network", network)
	}
	if network == "ws" {
		ws := js.NewObject()
		if v := params.Str("path"); v != "" {
			ws.Set("path", v)
		}
		if v := params.Str("host"); v != "" {
			ws.Set("headers", js.Obj("Host", v))
		}
		if ws.Len() > 0 {
			proxy.Set("ws-opts", ws)
		}
	}
	if network == "grpc" {
		g := js.NewObject()
		if v := params.Str("serviceName"); v != "" {
			g.Set("grpc-service-name", v)
		}
		if v := params.Str("mode"); v != "" {
			g.Set("grpc-mode", v)
		}
		if g.Len() > 0 {
			proxy.Set("grpc-opts", g)
		}
	}
	sniFromParams(proxy, params)
	if v := params.Str("fp"); v != "" {
		proxy.Set("client-fingerprint", v)
	}
	if params.Str("allowInsecure") == "1" {
		proxy.Set("skip-cert-verify", true)
	}
	if v := params.Str("dp"); v != "" {
		proxy.Set("dialer-proxy", v)
	}
	if params.Str("security") == "reality" {
		applyReality(proxy, params)
	}
	return proxy
}

func parseVmessURL(u string) (result *js.Object) {
	defer func() {
		if recover() != nil {
			result = nil
		}
	}()
	normalized := padBase64(urlSafeToStd(u[8:]))
	b, err := js.AtobBytes(normalized)
	if err != nil {
		return nil
	}
	cfgVal, err := js.JSONParse(js.TextDecode(b))
	if err != nil || js.IsNullish(cfgVal) {
		return nil
	}
	get := func(k string) any { return objGet(cfgVal, k) }
	proxy := js.Obj(
		"name", js.Or(get("ps"), "VMess-"+js.ToString(get("add"))),
		"type", "vmess",
		"server", js.Or(get("add"), get("host"), get("sni"), ""),
		"port", js.ParseInt(get("port"), 0),
		"uuid", get("id"),
		"alterId", js.Or(js.ParseInt(get("aid"), 0), 0.0),
		"cipher", js.Or(get("scy"), "auto"),
	)
	network := js.Or(get("net"), "tcp")
	if !js.StrictEquals(network, "tcp") {
		proxy.Set("network", network)
	}
	splitHosts := func(h any) *js.Array {
		s, ok := h.(string)
		if !ok {
			panic("host.split is not a function")
		}
		parts := strings.Split(s, ",")
		for i := range parts {
			parts[i] = js.Trim(parts[i])
		}
		return js.StrArr(parts)
	}
	switch {
	case js.StrictEquals(network, "ws"):
		ws := js.NewObject()
		if v := get("path"); js.Truthy(v) {
			ws.Set("path", v)
		}
		if v := get("host"); js.Truthy(v) {
			ws.Set("headers", js.Obj("Host", v))
		}
		if ws.Len() > 0 {
			proxy.Set("ws-opts", ws)
		}
	case js.StrictEquals(network, "grpc"):
		g := js.NewObject()
		if v := get("path"); js.Truthy(v) {
			g.Set("grpc-service-name", v)
		}
		if v := get("host"); js.Truthy(v) {
			g.Set("grpc-service-name", v)
		}
		if g.Len() > 0 {
			proxy.Set("grpc-opts", g)
		}
	case js.StrictEquals(network, "h2"):
		h2 := js.NewObject()
		if v := get("path"); js.Truthy(v) {
			h2.Set("path", v)
		}
		if v := get("host"); js.Truthy(v) {
			h2.Set("host", splitHosts(v))
		}
		if h2.Len() > 0 {
			proxy.Set("h2-opts", h2)
		}
	case js.StrictEquals(network, "http"):
		var hosts *js.Array
		if v := get("host"); js.Truthy(v) {
			hosts = splitHosts(v)
		} else {
			hosts = js.Arr()
		}
		proxy.Set("http-opts", js.Obj("path", js.Or(get("path"), "/"), "headers", js.Obj("Host", hosts)))
	case js.StrictEquals(network, "quic"):
		q := js.NewObject()
		if v := get("type"); js.Truthy(v) {
			q.Set("header", js.Obj("type", v))
		}
		if v := get("host"); js.Truthy(v) {
			q.Set("security", v)
		}
		if v := get("path"); js.Truthy(v) {
			q.Set("key", v)
		}
		if q.Len() > 0 {
			proxy.Set("quic-opts", q)
		}
	}
	if tls := get("tls"); js.StrictEquals(tls, "tls") || js.StrictEquals(tls, "reality") {
		proxy.Set("tls", true)
		if v := get("sni"); js.Truthy(v) {
			proxy.Set("servername", v)
		}
		if v := get("fp"); js.Truthy(v) {
			proxy.Set("client-fingerprint", v)
		}
		if v := get("alpn"); js.Truthy(v) {
			parts := strings.Split(js.ToString(v), ",")
			for i := range parts {
				parts[i] = js.Trim(parts[i])
			}
			proxy.Set("alpn", js.StrArr(parts))
		}
	}
	return proxy
}

func normalizeSsPluginOption(key string, value any) any {
	if key == "mux" {
		if js.StrictEquals(value, "1") || js.StrictEquals(value, "true") {
			return true
		}
		if js.StrictEquals(value, "0") || js.StrictEquals(value, "false") {
			return false
		}
	}
	return value
}

var (
	reEscEq    = js.MustRegExp(`\\=`, "g")
	reEscSemi  = js.MustRegExp(`\\;`, "g")
	reEscQ     = js.MustRegExp(`\\\?`, "g")
	reEscColon = js.MustRegExp(`\\:`, "g")
)

func parseSsPlugin(pluginStr string) (string, *js.Object) {
	parts := strings.Split(pluginStr, ";")
	opts := js.NewObject()
	for _, item := range parts[1:] {
		if item == "" {
			continue
		}
		eq := strings.Index(item, "=")
		if eq == -1 {
			opts.Set(item, true)
			continue
		}
		key := item[:eq]
		val := item[eq+1:]
		val = reEscEq.Replace(val, "=")
		val = reEscSemi.Replace(val, ";")
		val = reEscQ.Replace(val, "?")
		val = reEscColon.Replace(val, ":")
		var v any = val
		if val == "true" {
			v = true
		}
		if val == "false" {
			v = false
		}
		opts.Set(key, normalizeSsPluginOption(key, v))
	}
	return parts[0], opts
}

func parseSsURL(u string) *js.Object {
	body := u[5:]
	name := extractName(u)
	params := parseQueryParams(u)
	if h := strings.Index(body, "#"); h != -1 {
		body = body[:h]
	}
	if q := strings.Index(body, "?"); q != -1 {
		body = body[:q]
	}
	var method, password, server string
	var port float64
	if atIndex := strings.LastIndex(body, "@"); atIndex != -1 {
		userInfo := body[:atIndex]
		serverPart := body[atIndex+1:]
		decoded, err := js.Atob(padBase64(urlSafeToStd(userInfo)))
		if err != nil {
			decoded = userInfo
		}
		if c := strings.Index(decoded, ":"); c != -1 {
			method, password = decoded[:c], decoded[c+1:]
		}
		server, port = clashParseHostPort(serverPart)
	} else {
		decoded, err := js.Atob(padBase64(urlSafeToStd(body)))
		if err != nil {
			return nil
		}
		if atIdx := strings.LastIndex(decoded, "@"); atIdx != -1 {
			userPart := decoded[:atIdx]
			serverPart := decoded[atIdx+1:]
			if c := strings.Index(userPart, ":"); c != -1 {
				method, password = userPart[:c], userPart[c+1:]
			}
			server, port = clashParseHostPort(serverPart)
		}
	}
	if method == "" || password == "" || server == "" || !js.Truthy(port) {
		return nil
	}
	proxy := js.Obj("name", orStr(name, "SS-"+server), "type", "ss", "server", server, "port", port, "cipher", method, "password", password)
	if pluginStr := params.Str("plugin"); pluginStr != "" {
		pname, opts := parseSsPlugin(pluginStr)
		proxy.Set("plugin", pname)
		proxy.Set("plugin-opts", opts)
		if v := params.Str("obfs"); v != "" {
			opts.Set("mode", v)
		}
		if v := params.Str("obfs-host"); v != "" {
			opts.Set("host", v)
		}
		hasTLS := js.Truthy(opts.Get("tls"))
		if !hasTLS {
			mode := opts.Get("mode")
			if s, ok := mode.(string); ok {
				hasTLS = strings.Contains(s, "tls")
			} else if !js.IsNullish(mode) {
				// 非字符串（如布尔值）没有 includes 方法，上游会抛出异常并返回 null
				return nil
			}
		}
		if hasTLS || js.StrictEquals(opts.Get("security"), "tls") {
			proxy.Set("tls", true)
		}
		if h := opts.Get("host"); js.Truthy(h) {
			proxy.Set("sni", h)
			proxy.Set("servername", h)
		}
	}
	return proxy
}

func parseHysteria2URL(u string) *js.Object {
	prefixLen := 6
	if strings.HasPrefix(u, "hysteria2://") {
		prefixLen = 12
	}
	body := u[prefixLen:]
	atIndex := strings.Index(body, "@")
	if atIndex == -1 {
		return nil
	}
	password := js.SafeDecodeURIComponent(body[:atIndex])
	server, port := clashParseHostPort(cutServerPart(body[atIndex+1:]))
	params := parseQueryParams(u)
	name := extractName(u)
	proxy := js.Obj("name", orStr(name, "Hysteria2-"+server), "type", "hysteria2", "server", server, "port", port,
		"password", password, "auth", password)
	if v := params.Str("sni"); v != "" {
		proxy.Set("servername", v)
		proxy.Set("sni", v)
	}
	if params.Str("insecure") == "1" || params.Str("allowInsecure") == "1" {
		proxy.Set("skip-cert-verify", true)
	}
	if v := params.Str("handshake-timeout"); v != "" {
		proxy.Set("handshake-timeout", js.ParseInt(v, 10))
	}
	if v := params.Str("obfs"); v != "" {
		proxy.Set("obfs", v)
		if p := params.Str("obfs-password"); p != "" {
			proxy.Set("obfs-password", p)
		}
	}
	if params.Str("ports") != "" || params.Str("mport") != "" {
		proxy.Set("ports", orStr(params.Str("ports"), params.Str("mport")))
	}
	if v := params.Str("hop-interval"); v != "" {
		proxy.Set("hop-interval", v)
	}
	if v := params.Str("up"); v != "" {
		proxy.Set("up", v)
	}
	if v := params.Str("down"); v != "" {
		proxy.Set("down", v)
	}
	switch firstNonEmpty(params.GetValue("fast_open"), params.GetValue("fast-open")) {
	case "1", "true":
		proxy.Set("fast-open", true)
	case "0", "false":
		proxy.Set("fast-open", false)
	}
	realmID := params.Str("realm-id")
	realmToken := orStr(params.Str("realm-token"), params.Str("token"))
	realmServer := orStr(params.Str("realm-server"), params.Str("server-url"))
	stun := params.Str("stun-servers")
	if realmID != "" || realmToken != "" || realmServer != "" || stun != "" {
		ro := js.Obj("enable", true)
		proxy.Set("realm-opts", ro)
		if realmID != "" {
			ro.Set("realm-id", realmID)
		}
		if realmToken != "" {
			ro.Set("token", realmToken)
		}
		if realmServer != "" {
			ro.Set("server-url", realmServer)
		}
		if stun != "" {
			var items []string
			for _, s := range strings.Split(stun, ",") {
				if s = js.Trim(s); s != "" {
					items = append(items, s)
				}
			}
			ro.Set("stun-servers", js.StrArr(items))
		}
	}
	if v := params.Str("dp"); v != "" {
		proxy.Set("dialer-proxy", v)
	}
	return proxy
}

func setTriState(proxy *js.Object, keys []string, value string) {
	switch value {
	case "1", "true":
		for _, k := range keys {
			proxy.Set(k, true)
		}
	case "0", "false":
		for _, k := range keys {
			proxy.Set(k, false)
		}
	}
}

func parseTuicURL(u string) *js.Object {
	body := u[7:]
	atIndex := strings.LastIndex(body, "@")
	if atIndex == -1 {
		return nil
	}
	token := body[:atIndex]
	rawUUID, rawPassword := token, ""
	if sep := strings.Index(token, ":"); sep != -1 {
		rawUUID, rawPassword = token[:sep], token[sep+1:]
	}
	uuid := js.SafeDecodeURIComponent(rawUUID)
	password := js.SafeDecodeURIComponent(rawPassword)
	server, port := clashParseHostPort(cutServerPart(body[atIndex+1:]))
	params := parseQueryParams(u)
	name := extractName(u)
	proxy := js.Obj("name", orStr(name, "TUIC-"+server), "type", "tuic", "server", server, "port", port, "uuid", uuid, "password", password)
	if v := params.Str("sni"); v != "" {
		proxy.Set("servername", v)
		proxy.Set("sni", v)
	}
	if v := params.Str("alpn"); v != "" {
		proxy.Set("alpn", splitToArray(v, ","))
	}
	if params.Str("allowInsecure") == "1" || params.Str("insecure") == "1" || params.Str("allow_insecure") == "1" {
		proxy.Set("skip-cert-verify", true)
	}
	if v := firstNonEmpty(params.GetValue("congestion_control"), params.GetValue("congestion-control"), params.GetValue("congestion-controller")); v != "" {
		proxy.Set("congestion-controller", v)
	}
	if v := firstNonEmpty(params.GetValue("udp_relay_mode"), params.GetValue("udp-relay-mode")); v != "" {
		proxy.Set("udp-relay-mode", v)
	}
	setTriState(proxy, []string{"udp-over-stream"}, firstNonEmpty(params.GetValue("udp_over_stream"), params.GetValue("udp-over-stream")))
	setTriState(proxy, []string{"zero-rtt-handshake", "reduce-rtt"}, firstNonEmpty(params.GetValue("zero_rtt_handshake"),
		params.GetValue("zero-rtt-handshake"), params.GetValue("reduce_rtt"), params.GetValue("reduce-rtt")))
	if v := params.Str("heartbeat"); v != "" {
		proxy.Set("heartbeat", v)
	}
	if v := orStr(params.Str("heartbeat_interval"), params.Str("heartbeat-interval")); v != "" {
		proxy.Set("heartbeat-interval", v)
	}
	if v := orStr(params.Str("request_timeout"), params.Str("request-timeout")); v != "" {
		proxy.Set("request-timeout", js.ToNumber(v))
	}
	if v := params.Str("cwnd"); v != "" {
		proxy.Set("cwnd", js.ToNumber(v))
	}
	if v := orStr(params.Str("bbr_profile"), params.Str("bbr-profile")); v != "" {
		proxy.Set("bbr-profile", v)
	}
	if v := orStr(params.Str("max_udp_relay_packet_size"), params.Str("max-udp-relay-packet-size")); v != "" {
		proxy.Set("max-udp-relay-packet-size", js.ToNumber(v))
	}
	if v := orStr(params.Str("max_open_streams"), params.Str("max-open-streams")); v != "" {
		proxy.Set("max-open-streams", js.ToNumber(v))
	}
	setTriState(proxy, []string{"disable-sni"}, firstNonEmpty(params.GetValue("disable_sni"), params.GetValue("disable-sni")))
	setTriState(proxy, []string{"fast-open"}, firstNonEmpty(params.GetValue("fast_open"), params.GetValue("fast-open")))
	if v := params.Str("dp"); v != "" {
		proxy.Set("dialer-proxy", v)
	}
	proxy.Set("udp", true)
	return proxy
}

func parseWireguardURL(u string) *js.Object {
	body := u[len("wireguard://"):]
	atIndex := strings.Index(body, "@")
	if atIndex == -1 {
		return nil
	}
	privateKey := js.SafeDecodeURIComponent(body[:atIndex])
	server, port := clashParseHostPort(cutServerPart(body[atIndex+1:]))
	params := parseQueryParams(u)
	name := extractName(u)
	proxy := js.Obj("name", orStr(name, "WireGuard-"+server), "type", "wireguard", "server", server, "port", port,
		"private-key", privateKey, "remote-dns-resolve", true, "udp", true)
	if pk := orStr(params.Str("publickey"), params.Str("public-key")); pk != "" {
		proxy.Set("public-key", pk)
	}
	if address := params.Str("address"); address != "" {
		var addrs []string
		for _, a := range strings.Split(address, ",") {
			if a = js.Trim(a); a != "" {
				addrs = append(addrs, a)
			}
		}
		ipv6 := ""
		for _, a := range addrs {
			if strings.Contains(a, ":") {
				ipv6 = a
				break
			}
		}
		if len(addrs) > 0 {
			proxy.Set("ip", js.StrArr(addrs))
		}
		if ipv6 != "" {
			proxy.Set("ipv6", ipv6)
		}
	}
	if ips := orStr(params.Str("allowedips"), params.Str("allowed-ips")); ips != "" {
		proxy.Set("allowed-ips", js.StrArr(splitTrim(ips, ",")))
	}
	if reserved := params.Str("reserved"); reserved != "" {
		arr := js.Arr()
		allNum := true
		for _, n := range strings.Split(reserved, ",") {
			v := js.ParseInt(js.Trim(n), 0)
			if v != v {
				allNum = false
			}
			arr.Push(v)
		}
		if allNum {
			proxy.Set("reserved", arr)
		}
	}
	if v := params.Str("mtu"); v != "" {
		proxy.Set("mtu", js.ParseInt(v, 0))
	}
	if v := params.Str("dns"); v != "" {
		proxy.Set("dns", js.StrArr(splitTrim(v, ",")))
	}
	if v := params.Str("keepalive"); v != "" {
		proxy.Set("persistent-keepalive", js.ParseInt(v, 0))
	}
	if v := orStr(params.Str("presharedkey"), params.Str("preshared-key")); v != "" {
		proxy.Set("preshared-key", v)
	}
	return proxy
}

func parseSnellURLClash(u string) *js.Object {
	body := u[len("snell://"):]
	psk, serverPart := "", body
	if atIndex := strings.Index(body, "@"); atIndex != -1 {
		psk = js.SafeDecodeURIComponent(body[:atIndex])
		serverPart = body[atIndex+1:]
	}
	server, port := clashParseHostPort(cutServerPart(serverPart))
	params := parseQueryParams(u)
	name := extractName(u)
	if psk == "" {
		psk = orStr(params.Str("psk"), params.Str("password"))
	}
	proxy := js.Obj("name", orStr(name, "Snell-"+server), "type", "snell", "server", server, "port", port, "psk", psk)
	if v := params.Str("version"); v != "" {
		proxy.Set("version", js.ParseInt(v, 0))
	}
	if v, ok := params.Get("reuse"); ok {
		proxy.Set("reuse", v == "true")
	}
	if v, ok := params.Get("tfo"); ok {
		proxy.Set("tfo", v == "true")
	}
	obfs, obfsHost := params.Str("obfs"), params.Str("obfs-host")
	if obfs != "" || obfsHost != "" {
		oo := js.NewObject()
		proxy.Set("obfs-opts", oo)
		if obfs != "" {
			oo.Set("mode", obfs)
		}
		if obfsHost != "" {
			oo.Set("host", obfsHost)
		}
	}
	if params.Str("udp-relay") == "true" {
		proxy.Set("udp", true)
	}
	if e := params.Str("ecn"); e == "true" || e == "1" {
		proxy.Set("ecn", true)
	}
	return proxy
}

func parseAnytlsURL(u string) *js.Object {
	body := u[9:]
	password, serverPart := "", body
	if atIndex := strings.Index(body, "@"); atIndex != -1 {
		password = js.SafeDecodeURIComponent(body[:atIndex])
		serverPart = body[atIndex+1:]
	}
	server, port := clashParseHostPort(cutServerPart(serverPart))
	if port != port {
		port = 443
	}
	params := parseQueryParams(u)
	name := extractName(u)
	proxy := js.Obj("name", orStr(name, "AnyTLS-"+server), "type", "anytls", "server", server, "port", port, "password", password)
	if sni := orStr(params.Str("sni"), params.Str("peer")); sni != "" {
		proxy.Set("servername", sni)
		proxy.Set("sni", sni)
	}
	if v := params.Str("alpn"); v != "" {
		proxy.Set("alpn", splitToArray(v, ","))
	}
	if params.Str("insecure") == "1" || params.Str("allowInsecure") == "1" {
		proxy.Set("skip-cert-verify", true)
	}
	if v := firstNonEmpty(params.GetValue("pinnedPeerCertSha256"), params.GetValue("pinned-peer-cert-sha256"),
		params.GetValue("peer-cert-sha256"), params.GetValue("certSha256")); v != "" {
		proxy.Set("pinnedPeerCertSha256", v)
	}
	proxy.Set("udp", true)
	return proxy
}

// splitUserInfo 解析 user:pass，均做 decodeURIComponent；失败时返回 ok=false（上游抛出异常后返回 null）。
func splitUserInfo(userInfo string) (string, string, bool) {
	var username, password string
	var err error
	if c := strings.Index(userInfo, ":"); c != -1 {
		if username, err = js.DecodeURIComponent(userInfo[:c]); err != nil {
			return "", "", false
		}
		if password, err = js.DecodeURIComponent(userInfo[c+1:]); err != nil {
			return "", "", false
		}
	} else if username, err = js.DecodeURIComponent(userInfo); err != nil {
		return "", "", false
	}
	return username, password, true
}

func parseHTTPSURL(u string, isTLS bool) *js.Object {
	prefix := 7
	if isTLS {
		prefix = 8
	}
	body := u[prefix:]
	userInfo, serverPart := "", body
	if atIndex := strings.Index(body, "@"); atIndex != -1 {
		userInfo, serverPart = body[:atIndex], body[atIndex+1:]
	}
	server, port := clashParseHostPort(cutServerPart(serverPart))
	params := parseQueryParams(u)
	name := extractName(u)
	username, password, ok := splitUserInfo(userInfo)
	if !ok {
		return nil
	}
	proxy := js.Obj("name", orStr(name, "HTTPS-"+server), "type", "http", "server", server, "port", port,
		"username", username, "password", password)
	if isTLS {
		proxy.Set("tls", true)
	}
	sniFromParams(proxy, params)
	if params.Str("allowInsecure") == "1" || params.Str("insecure") == "1" {
		proxy.Set("skip-cert-verify", true)
	}
	proxy.Set("udp", false)
	return proxy
}

func parseSocks5URL(u string) *js.Object {
	body := u[9:]
	atIndex := strings.Index(body, "@")
	if atIndex == -1 {
		return nil
	}
	userInfo := body[:atIndex]
	server, port := clashParseHostPort(cutServerPart(body[atIndex+1:]))
	params := parseQueryParams(u)
	name := extractName(u)
	username, password, ok := splitUserInfo(userInfo)
	if !ok {
		return nil
	}
	tlsVal := params.Str("tls")
	useTLS := tlsVal == "1" || tlsVal == "true" || params.Str("secure") == "1"
	typ := "socks5"
	if useTLS {
		typ = "socks5-tls"
	}
	proxy := js.Obj("name", orStr(name, "SOCKS5-"+server), "type", typ, "server", server, "port", port,
		"username", username, "password", password, "udp", false)
	sniFromParams(proxy, params)
	if params.Str("allowInsecure") == "1" || params.Str("insecure") == "1" {
		proxy.Set("skip-cert-verify", true)
	}
	return proxy
}

func parseSsrURL(u string) *js.Object {
	decoded := base64DecodeLoose(u[6:])
	mainParts := strings.Split(decoded, ":")
	if len(mainParts) < 6 {
		return nil
	}
	server := mainParts[0]
	port := js.ParseInt(mainParts[1], 0)
	passwordPart := mainParts[5]
	passwordEnd := strings.Index(passwordPart, "/")
	passwordB64 := passwordPart
	if passwordEnd != -1 {
		passwordB64 = passwordPart[:passwordEnd]
	}
	proxy := js.Obj("name", "SSR-"+server, "type", "ssr", "server", server, "port", port, "protocol", mainParts[2],
		"cipher", mainParts[3], "obfs", mainParts[4], "password", base64DecodeLoose(passwordB64), "udp", true)
	if passwordEnd != -1 {
		// 对应 substring(passwordEndIndex + 2)（按 UTF-16 计）
		paramsStr := js.Substring(passwordPart, js.IndexUTF16(passwordPart, passwordEnd)+2, js.UTF16Len(passwordPart))
		params := js.ParseSearchParams(paramsStr)
		if v := params.Str("obfsparam"); v != "" {
			proxy.Set("obfs-param", base64DecodeLoose(v))
		}
		if v := params.Str("protoparam"); v != "" {
			proxy.Set("protocol-param", base64DecodeLoose(v))
		}
		if v := params.Str("remarks"); v != "" {
			proxy.Set("name", base64DecodeLoose(v))
		}
		if v := params.Str("group"); v != "" {
			proxy.Set("group", base64DecodeLoose(v))
		}
		if v := params.Str("udpport"); v != "" {
			proxy.Set("udpport", v)
		}
	}
	return proxy
}

func parseSsdURL(u string) *js.Object {
	cfgVal, err := js.JSONParse(base64DecodeLoose(u[6:]))
	if err != nil || js.IsNullish(cfgVal) {
		return nil
	}
	servers := objGet(cfgVal, "servers")
	if !js.Truthy(servers) || !js.Truthy(objGet(servers, "length")) {
		return nil
	}
	s := objGet(servers, "0")
	if js.IsNullish(s) {
		return nil
	}
	var pluginOpts any
	if po := objGet(s, "plugin_options"); js.Truthy(po) {
		pluginOpts = js.Obj("host", po)
	}
	return js.Obj(
		"name", js.Or(objGet(s, "remarks"), "SSD-"+js.ToString(objGet(s, "server"))),
		"type", "ss",
		"server", objGet(s, "server"),
		"port", objGet(s, "port"),
		"cipher", js.Or(objGet(cfgVal, "encryption"), objGet(s, "encryption"), "aes-256-gcm"),
		"password", js.Or(objGet(cfgVal, "password"), objGet(s, "password")),
		"plugin", objGet(s, "plugin"),
		"plugin-opts", pluginOpts,
	)
}

var reMieruScheme = js.MustRegExp(`^mierus:\/\/`, "i")

func parseMieruURL(u string) *js.Object {
	body := u[strings.Index(u, "//")+2:]
	beforeHash := body
	if h := strings.Index(body, "#"); h >= 0 {
		beforeHash = body[:h]
	}
	authority, query := beforeHash, ""
	if q := strings.Index(beforeHash, "?"); q >= 0 {
		authority, query = beforeHash[:q], beforeHash[q+1:]
	}
	params := js.ParseSearchParams(query)
	var server string
	var port float64
	n := js.UTF16Len(authority)
	if strings.HasPrefix(authority, "[") {
		end := js.IndexUTF16(authority, strings.Index(authority, "]"))
		server = js.Slice(authority, 1, end)
		port = js.ToNumber(js.Slice(authority, end+2, n))
	} else {
		split := js.IndexUTF16(authority, strings.LastIndex(authority, ":"))
		server = js.Slice(authority, 0, split)
		port = js.ToNumber(js.Slice(authority, split+1, n))
	}
	proxy := js.Obj("name", orStr(extractName(u), "Mieru-"+server), "type", "mieru", "server", server, "port", port)
	for _, key := range []string{"username", "password", "transport", "handshake-mode", "profile"} {
		if v, ok := params.Get(key); ok {
			proxy.Set(key, v)
		}
	}
	for _, key := range []string{"udp", "multiplexing"} {
		if v, ok := params.Get(key); ok {
			proxy.Set(key, v == "true")
		}
	}
	if v, ok := params.Get("mtu"); ok {
		proxy.Set("mtu", js.ToNumber(v))
	}
	return proxy
}

// URLToClashProxy 对应 urlToClashProxy；不支持的链接返回 nil。
func URLToClashProxy(urlVal any) (result *js.Object) {
	u, ok := urlVal.(string)
	if !ok || u == "" {
		return nil
	}
	defer func() {
		if recover() != nil {
			result = nil
		}
	}()
	if reMieruScheme.Test(u) {
		return parseMieruURL(u)
	}
	lower := js.ToLower(u)
	switch {
	case strings.HasPrefix(lower, "vless://"):
		return parseVlessURL(u)
	case strings.HasPrefix(lower, "trojan://"):
		return parseTrojanURL(u)
	case strings.HasPrefix(lower, "vmess://"):
		return parseVmessURL(u)
	case strings.HasPrefix(lower, "ssr://"):
		return parseSsrURL(u)
	case strings.HasPrefix(lower, "ssd://"):
		return parseSsdURL(u)
	case strings.HasPrefix(lower, "ss://"):
		return parseSsURL(u)
	case strings.HasPrefix(lower, "hysteria2://"), strings.HasPrefix(lower, "hy2://"):
		return parseHysteria2URL(u)
	case strings.HasPrefix(lower, "tuic://"):
		return parseTuicURL(u)
	case strings.HasPrefix(lower, "snell://"):
		return parseSnellURLClash(u)
	case strings.HasPrefix(lower, "wireguard://"):
		return parseWireguardURL(u)
	case strings.HasPrefix(lower, "anytls://"):
		return parseAnytlsURL(u)
	case strings.HasPrefix(lower, "https://"):
		return parseHTTPSURL(u, true)
	case strings.HasPrefix(lower, "http://"):
		return parseHTTPSURL(u, false)
	case strings.HasPrefix(lower, "socks5://"):
		return parseSocks5URL(u)
	}
	return nil
}

// ClashOptions 对应 urlsToClashProxies 的 options（未设置的字段为 nil，对应 undefined）。
type ClashOptions = *js.Object

var reHasFlagOrGlobe = js.MustRegExp(`([\u{1F1E6}-\u{1F1FF}]{2}|[\u{1F30D}-\u{1F30F}])`, "u")

var nativeUDPTypes = map[string]bool{"hysteria2": true, "hy2": true, "tuic": true, "hysteria": true, "wireguard": true}

// URLsToClashProxies 对应 urlsToClashProxies。
func URLsToClashProxies(urls []string, options *js.Object) []*js.Object {
	out := []*js.Object{}
	if urls == nil {
		return out
	}
	opt := options.Get
	for _, u := range urls {
		proxy := URLToClashProxy(u)
		if proxy == nil {
			continue
		}
		if v := opt("enableTfo"); v != nil {
			proxy.Set("tfo", v)
		}
		if v := opt("enableUdp"); v != nil {
			typ := js.ToLower(js.ToString(js.Or(proxy.Get("type"), "")))
			if js.Truthy(v) || nativeUDPTypes[typ] {
				proxy.Set("udp", true)
			} else {
				proxy.Set("udp", false)
			}
		}
		if js.Truthy(opt("skipCertVerify")) {
			proxy.Set("skip-cert-verify", true)
		}
		if v := opt("supportX25519mlkem768"); v != nil && js.Truthy(proxy.Get("reality-opts")) {
			if ro, ok := proxy.Get("reality-opts").(*js.Object); ok {
				if js.Truthy(v) {
					ro.Set("support-x25519mlkem768", true)
				} else {
					ro.Delete("support-x25519mlkem768")
				}
			}
		}
		meta := ExtractNodeMetadata(proxy.Get("name"))
		proxy.Set("metadata", meta)
		if !js.StrictEquals(opt("addFlagEmoji"), false) && js.Truthy(meta.Get("flag")) &&
			!reHasFlagOrGlobe.Test(js.ToString(proxy.Get("name"))) {
			proxy.Set("name", js.ToString(meta.Get("flag"))+" "+js.ToString(proxy.Get("name")))
		}
		if isVirtualInfoProxy(proxy) || !IsLocalProxyEndpoint(proxy) {
			out = append(out, proxy)
		}
	}
	return out
}

// GenerateClashConfig 对应 generateClashConfig（简化版 YAML，每个节点一行 JSON）。
func GenerateClashConfig(urls []string, options *js.Object) string {
	proxies := URLsToClashProxies(urls, options)
	if len(proxies) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("proxies:\n")
	for _, p := range proxies {
		rest := p.Clone()
		rest.Delete("metadata")
		sb.WriteString("  - " + js.MustStringify(rest) + "\n")
	}
	return sb.String()
}
