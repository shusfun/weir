package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/utils/clash-to-url.js

func base64URLSafeEncode(v any) string {
	return strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(js.Base64EncodeUTF8(js.ToString(v))), "=")
}

func formatURIServer(server string) string {
	if strings.Contains(server, ":") && !strings.HasPrefix(server, "[") {
		return "[" + server + "]"
	}
	return server
}

func appendHysteria2RealmParams(params *[]string, realmOpts any) {
	ro, ok := realmOpts.(*js.Object)
	if !ok {
		if a, isArr := realmOpts.(*js.Array); isArr && a != nil {
			// 数组也是 object，但不会带有这些属性
			return
		}
		return
	}
	if v := ro.Get("realm-id"); js.Truthy(v) {
		*params = append(*params, "realm-id="+enc(v))
	}
	if v := ro.Get("token"); js.Truthy(v) {
		*params = append(*params, "realm-token="+enc(v))
	}
	if v := ro.Get("server-url"); js.Truthy(v) {
		*params = append(*params, "realm-server="+enc(v))
	}
	if a, isArr := ro.Get("stun-servers").(*js.Array); isArr && a.Len() > 0 {
		*params = append(*params, "stun-servers="+js.EncodeURIComponent(a.Join(",")))
	}
}

// joinIfArray 对应 `Array.isArray(v) ? v.join(',') : v`。
func joinIfArray(v any) any {
	if a, ok := v.(*js.Array); ok {
		return a.Join(",")
	}
	return v
}

// ConvertClashProxyToURL 对应 convertClashProxyToUrl；无法转换时返回 ok=false（null）。
func ConvertClashProxyToURL(proxyVal any) (result string, ok bool) {
	defer func() {
		// 上游整体包在 try/catch 中，任何运行期异常都返回 null
		if r := recover(); r != nil {
			result, ok = "", false
		}
	}()
	proxy, isObj := proxyVal.(*js.Object)
	if !isObj {
		if !js.IsObjectLike(proxyVal) {
			// 访问 undefined/null 的属性抛出异常；原始值的属性均为 undefined
			if js.IsNullish(proxyVal) {
				return "", false
			}
		}
		proxy = js.NewObject()
	}
	typ := js.ToLower(js.ToString(js.Or(proxy.Get("type"), "")))
	name := js.Or(proxy.Get("name"), "Untitled")
	serverVal := proxy.Get("server")
	server, isStr := serverVal.(string)
	if !isStr {
		// formatUriServer 对非字符串调用 includes 会抛出异常
		return "", false
	}
	uriServer := formatURIServer(server)
	port := proxy.Get("port")
	if server == "" || !js.Truthy(port) {
		return "", false
	}
	p := js.ToString(port)
	get := proxy.Get
	encName := enc(name)

	if typ == "mieru" {
		params := js.NewSearchParams()
		for _, key := range []string{"username", "password", "transport", "udp", "multiplexing", "handshake-mode", "mtu", "profile"} {
			if v := get(key); !js.IsNullish(v) {
				params.Set(key, js.ToString(v))
			}
		}
		return "mierus://" + formatURIServer(server) + ":" + p + "?" + params.String() + "#" + encName, true
	}

	if typ == "http" || typ == "https" {
		useTLS := typ == "https" || js.StrictEquals(get("tls"), true)
		scheme := "http"
		if useTLS {
			scheme = "https"
		}
		userInfo := ""
		if js.Truthy(get("username")) {
			userInfo = enc(get("username"))
			if js.Truthy(get("password")) {
				userInfo += ":" + enc(get("password"))
			}
			userInfo += "@"
		}
		params := js.NewSearchParams()
		if useTLS {
			if sni := js.Or(get("sni"), get("servername")); js.Truthy(sni) {
				params.Set("sni", js.ToString(sni))
			}
			if v := get("skip-cert-verify"); v != nil {
				if js.Truthy(v) {
					params.Set("allowInsecure", "1")
				} else {
					params.Set("allowInsecure", "0")
				}
			}
			if fp := js.Or(get("client-fingerprint"), get("fingerprint")); js.Truthy(fp) {
				params.Set("fp", js.ToString(fp))
			}
			if alpn := get("alpn"); js.Truthy(alpn) {
				params.Set("alpn", js.ToString(joinIfArray(alpn)))
			}
		}
		query := params.String()
		if query != "" {
			query = "?" + query
		}
		return scheme + "://" + userInfo + uriServer + ":" + p + query + "#" + encName, true
	}

	if typ == "ss" || typ == "shadowsocks" {
		userInfo := js.Base64EncodeUTF8(js.ToString(get("cipher")) + ":" + js.ToString(get("password")))
		u := "ss://" + userInfo + "@" + uriServer + ":" + p
		if plugin := get("plugin"); js.Truthy(plugin) {
			params := []string{"plugin=" + enc(plugin)}
			if pluginOpts := get("plugin-opts"); js.Truthy(pluginOpts) {
				if v := objGet(pluginOpts, "enabled"); v != nil {
					params = append(params, "enabled="+js.ToString(v))
				}
				if v := objGet(pluginOpts, "padding"); v != nil {
					params = append(params, "padding="+js.ToString(v))
				}
				if v := objGet(pluginOpts, "mode"); js.Truthy(v) {
					params = append(params, "obfs="+enc(v))
				}
				if v := objGet(pluginOpts, "host"); js.Truthy(v) {
					params = append(params, "obfs-host="+enc(v))
				}
			}
			u += "?" + strings.Join(params, "&")
		}
		return u + "#" + encName, true
	}

	if typ == "ssr" || typ == "shadowsocksr" {
		password := base64URLSafeEncode(get("password"))
		obfs := js.ToString(js.Or(get("obfs"), "plain"))
		protocol := js.ToString(js.Or(get("protocol"), "origin"))
		params := "obfs=" + obfs + "&obfsparam=" + base64URLSafeEncode(js.Or(get("obfs-param"), "")) +
			"&protocol=" + protocol + "&protoparam=" + base64URLSafeEncode(js.Or(get("protocol-param"), "")) +
			"&remarks=" + base64URLSafeEncode(name)
		body := server + ":" + p + ":" + protocol + ":" + js.ToString(js.Or(get("cipher"), "none")) + ":" + obfs + ":" + password + "/?" + params
		return "ssr://" + base64URLSafeEncode(body), true
	}

	if typ == "vmess" {
		uuid := js.Or(get("uuid"), get("UUID"), "")
		network := js.Or(get("network"), "tcp")
		tlsStr := ""
		if js.Truthy(get("tls")) {
			tlsStr = "tls"
		}
		cfg := js.Obj(
			"v", "2", "ps", name, "add", server, "port", port, "id", uuid,
			"aid", js.Or(get("alterId"), 0.0), "net", network, "type", "none", "host", "", "path", "",
			"tls", tlsStr, "sni", js.Or(get("sni"), get("servername"), ""),
			"fp", js.Or(get("client-fingerprint"), ""),
		)
		switch {
		case js.StrictEquals(network, "ws"):
			if wsOpts := js.Or(get("ws-opts"), get("wsOpts")); js.Truthy(wsOpts) {
				cfg.Set("path", js.Or(objGet(wsOpts, "path"), ""))
				if h := objGet(objGet(wsOpts, "headers"), "Host"); js.Truthy(h) {
					cfg.Set("host", h)
				}
			}
		case js.StrictEquals(network, "grpc"):
			if grpcOpts := js.Or(get("grpc-opts"), get("grpcOpts")); js.Truthy(grpcOpts) {
				cfg.Set("path", js.Or(objGet(grpcOpts, "grpc-service-name"), ""))
			}
		case js.StrictEquals(network, "h2") || js.StrictEquals(network, "http"):
			ns := js.ToString(network)
			if opts := js.Or(get(ns+"-opts"), get(ns+"Opts")); js.Truthy(opts) {
				cfg.Set("path", js.Or(objGet(opts, "path"), ""))
				host := objGet(opts, "host")
				if a, isArr := host.(*js.Array); isArr {
					cfg.Set("host", a.Join(","))
				} else {
					cfg.Set("host", js.Or(host, ""))
				}
			}
		case js.StrictEquals(network, "quic"):
			if quicOpts := js.Or(get("quic-opts"), get("quicOpts")); js.Truthy(quicOpts) {
				cfg.Set("type", js.Or(objGet(objGet(quicOpts, "header"), "type"), "none"))
				cfg.Set("host", js.Or(objGet(quicOpts, "security"), ""))
				cfg.Set("path", js.Or(objGet(quicOpts, "key"), ""))
			}
		}
		return "vmess://" + js.Base64EncodeUTF8(js.MustStringify(cfg)), true
	}

	appendReality := func(params *[]string) bool {
		realityOpts := get("reality-opts")
		if !js.Truthy(realityOpts) {
			return false
		}
		*params = append(*params, "security=reality")
		if v := objGet(realityOpts, "public-key"); js.Truthy(v) {
			*params = append(*params, "pbk="+enc(v))
		}
		if v := objGet(realityOpts, "short-id"); js.Truthy(v) {
			*params = append(*params, "sid="+enc(v))
		}
		if v := objGet(realityOpts, "spider-x"); js.Truthy(v) {
			*params = append(*params, "spx="+enc(v))
		}
		sxm := js.Coalesce(objGet(realityOpts, "support-x25519mlkem768"), objGet(realityOpts, "support_x25519mlkem768"))
		if sxm != nil {
			if js.StrictEquals(sxm, false) || js.StrictEquals(sxm, "false") || js.StrictEquals(sxm, 0.0) || js.StrictEquals(sxm, "0") {
				*params = append(*params, "support-x25519mlkem768=false")
			} else if js.StrictEquals(sxm, true) || js.StrictEquals(sxm, "true") || js.StrictEquals(sxm, 1.0) || js.StrictEquals(sxm, "1") {
				*params = append(*params, "support-x25519mlkem768=true")
			}
		}
		return true
	}

	if typ == "trojan" {
		var params []string
		network := js.Or(get("network"), "tcp")
		if !js.StrictEquals(network, "tcp") {
			params = append(params, "type="+enc(network))
		}
		if wsOpts := js.Or(get("wsOpts"), get("ws-opts")); js.Truthy(wsOpts) {
			if v := objGet(wsOpts, "path"); js.Truthy(v) {
				params = append(params, "path="+enc(v))
			}
			if h := objGet(objGet(wsOpts, "headers"), "Host"); js.Truthy(h) {
				params = append(params, "host="+enc(h))
			}
		}
		if grpcOpts := js.Or(get("grpcOpts"), get("grpc-opts")); js.Truthy(grpcOpts) {
			if v := objGet(grpcOpts, "grpc-service-name"); js.Truthy(v) {
				params = append(params, "serviceName="+enc(v))
			}
			if v := objGet(grpcOpts, "grpc-mode"); js.Truthy(v) {
				params = append(params, "mode="+enc(v))
			}
		}
		if sni := js.Coalesce(get("servername"), get("sni")); sni != nil {
			params = append(params, "sni="+enc(sni))
		}
		if v := get("client-fingerprint"); js.Truthy(v) {
			params = append(params, "fp="+enc(v))
		}
		if v := get("dialer-proxy"); js.Truthy(v) {
			params = append(params, "dp="+enc(v))
		}
		if js.Truthy(get("skipCertVerify")) || js.Truthy(get("skip-cert-verify")) {
			params = append(params, "allowInsecure=1")
		}
		appendReality(&params)
		query := ""
		if len(params) > 0 {
			query = "?" + strings.Join(params, "&")
		}
		return "trojan://" + enc(get("password")) + "@" + uriServer + ":" + p + query + "#" + encName, true
	}

	if typ == "vless" {
		uuid := js.Or(get("uuid"), get("UUID"))
		if !js.Truthy(uuid) {
			return "", false
		}
		params := []string{"encryption=none"}
		if v := get("network"); js.Truthy(v) {
			params = append(params, "type="+js.ToString(v))
		}
		if wsOpts := js.Or(get("wsOpts"), get("ws-opts")); js.Truthy(wsOpts) {
			if v := objGet(wsOpts, "path"); js.Truthy(v) {
				params = append(params, "path="+enc(v))
			}
			if h := objGet(objGet(wsOpts, "headers"), "Host"); js.Truthy(h) {
				params = append(params, "host="+enc(h))
			}
		}
		if grpcOpts := js.Or(get("grpcOpts"), get("grpc-opts")); js.Truthy(grpcOpts) {
			if v := objGet(grpcOpts, "grpc-service-name"); js.Truthy(v) {
				params = append(params, "serviceName="+enc(v))
			}
			if v := objGet(grpcOpts, "grpc-mode"); js.Truthy(v) {
				params = append(params, "mode="+enc(v))
			}
		}
		if hu := js.Or(get("httpupgrade-opts"), get("httpupgradeOpts")); js.Truthy(hu) {
			if v := objGet(hu, "path"); js.Truthy(v) {
				params = append(params, "path="+enc(v))
			}
			if v := objGet(hu, "host"); js.Truthy(v) {
				params = append(params, "host="+enc(v))
			}
		}
		if xo := js.Or(get("xhttp-opts"), get("xhttpOpts")); js.Truthy(xo) {
			if v := objGet(xo, "path"); js.Truthy(v) {
				params = append(params, "path="+enc(v))
			}
			if h := js.Or(objGet(xo, "host"), objGet(objGet(xo, "headers"), "Host")); js.Truthy(h) {
				params = append(params, "host="+enc(h))
			}
			if v := objGet(xo, "mode"); js.Truthy(v) {
				params = append(params, "mode="+enc(v))
			}
		}
		if !appendReality(&params) && js.Truthy(get("tls")) {
			params = append(params, "security=tls")
		}
		if v := get("flow"); js.Truthy(v) {
			params = append(params, "flow="+js.ToString(v))
		}
		sniVal := get("sni")
		if proxy.Get("servername") != nil {
			sniVal = get("servername")
		}
		if sniVal != nil {
			params = append(params, "sni="+enc(sniVal))
		}
		if v := get("client-fingerprint"); js.Truthy(v) {
			params = append(params, "fp="+enc(v))
		}
		if v := get("dialer-proxy"); js.Truthy(v) {
			params = append(params, "dp="+enc(v))
		}
		return "vless://" + js.ToString(uuid) + "@" + uriServer + ":" + p + "?" + strings.Join(params, "&") + "#" + encName, true
	}

	if typ == "hysteria2" || typ == "hy2" || typ == "hy" {
		var params []string
		password := js.Or(get("password"), get("auth"), "")
		if v := get("obfs"); js.Truthy(v) {
			params = append(params, "obfs="+enc(v))
		}
		if v := get("obfs-password"); js.Truthy(v) {
			params = append(params, "obfs-password="+enc(v))
		}
		if sni := js.Coalesce(get("servername"), get("sni")); sni != nil {
			params = append(params, "sni="+enc(sni))
		}
		if js.Truthy(get("skipCertVerify")) || js.Truthy(get("skip-cert-verify")) {
			params = append(params, "insecure=1")
		}
		if v := get("ports"); v != nil {
			params = append(params, "ports="+enc(v))
		}
		if v := get("hop-interval"); v != nil {
			params = append(params, "hop-interval="+enc(v))
		}
		if get("up") != nil || get("up-mbps") != nil {
			params = append(params, "up="+enc(js.Coalesce(get("up"), get("up-mbps"))))
		}
		if get("down") != nil || get("down-mbps") != nil {
			params = append(params, "down="+enc(js.Coalesce(get("down"), get("down-mbps"))))
		}
		if v := get("fast-open"); v != nil {
			params = append(params, "fast_open="+boolDigit(v))
		}
		if v := get("dialer-proxy"); js.Truthy(v) {
			params = append(params, "dp="+enc(v))
		}
		appendHysteria2RealmParams(&params, get("realm-opts"))
		query := ""
		if len(params) > 0 {
			query = "?" + strings.Join(params, "&")
		}
		return "hysteria2://" + enc(password) + "@" + uriServer + ":" + p + query + "#" + encName, true
	}

	if typ == "hysteria" {
		var params []string
		password := js.Or(get("password"), get("auth"), "")
		if js.StrictEquals(get("protocol"), "udp") {
			params = append(params, "protocol=udp")
		}
		if v := get("sni"); v != nil {
			params = append(params, "sni="+enc(v))
		}
		if js.Truthy(get("skipCertVerify")) || js.Truthy(get("skip-cert-verify")) {
			params = append(params, "insecure=1")
		}
		if js.Truthy(get("up")) || js.Truthy(get("up-mbps")) {
			params = append(params, "up="+js.ToString(js.Or(get("up"), get("up-mbps"))))
		}
		if js.Truthy(get("down")) || js.Truthy(get("down-mbps")) {
			params = append(params, "down="+js.ToString(js.Or(get("down"), get("down-mbps"))))
		}
		query := ""
		if len(params) > 0 {
			query = "?" + strings.Join(params, "&")
		}
		return "hysteria://" + enc(password) + "@" + uriServer + ":" + p + query + "#" + encName, true
	}

	if typ == "socks5" {
		auth := ""
		if js.Truthy(get("username")) && js.Truthy(get("password")) {
			auth = enc(get("username")) + ":" + enc(get("password")) + "@"
		}
		return "socks5://" + auth + server + ":" + p + "#" + encName, true
	}

	if typ == "snell" {
		var params []string
		if v := get("version"); js.Truthy(v) {
			params = append(params, "version="+js.ToString(v))
		}
		if v := get("reuse"); v != nil {
			params = append(params, "reuse="+js.ToString(v))
		}
		if v := get("tfo"); v != nil {
			params = append(params, "tfo="+js.ToString(v))
		}
		if obfsOpts := js.Or(get("obfs-opts"), get("pluginOpts")); js.Truthy(obfsOpts) {
			if v := objGet(obfsOpts, "mode"); js.Truthy(v) {
				params = append(params, "obfs="+js.ToString(v))
			}
			if v := objGet(obfsOpts, "host"); js.Truthy(v) {
				params = append(params, "obfs-host="+enc(v))
			}
		}
		if js.Truthy(get("ecn")) {
			params = append(params, "ecn=true")
		}
		psk := js.Or(get("psk"), get("password"), "")
		query := ""
		if len(params) > 0 {
			query = "?" + strings.Join(params, "&")
		}
		return "snell://" + enc(psk) + "@" + uriServer + ":" + p + query + "#" + encName, true
	}

	if typ == "naive" || js.StrictEquals(get("protocol"), "naive") {
		username := js.Or(get("username"), "")
		password := js.Or(get("password"), "")
		auth := ""
		if js.Truthy(username) && js.Truthy(password) {
			auth = enc(username) + ":" + enc(password) + "@"
		}
		var params []string
		if v := get("padding"); v != nil {
			params = append(params, "padding="+js.ToString(v))
		}
		if v := get("extra-headers"); js.Truthy(v) {
			params = append(params, "extra-headers="+enc(v))
		}
		query := ""
		if len(params) > 0 {
			query = "?" + strings.Join(params, "&")
		}
		scheme := "naive+https"
		if js.Truthy(get("quic")) {
			scheme = "naive+quic"
		}
		return scheme + "://" + auth + server + ":" + p + query + "#" + encName, true
	}

	if typ == "anytls" {
		password := js.Or(get("password"), "")
		var params []string
		if v := get("sni"); v != nil {
			params = append(params, "sni="+enc(v))
		}
		if alpn := get("alpn"); js.Truthy(alpn) {
			params = append(params, "alpn="+enc(joinIfArray(alpn)))
		}
		if js.Truthy(get("skip-cert-verify")) {
			params = append(params, "insecure=1")
		}
		if pin := js.Or(get("pinnedPeerCertSha256"), get("pinned-peer-cert-sha256"), get("peer-cert-sha256"), get("certSha256")); js.Truthy(pin) {
			params = append(params, "pinnedPeerCertSha256="+enc(pin))
		}
		if v := get("padding"); v != nil {
			params = append(params, "padding="+js.ToString(v))
		}
		query := ""
		if len(params) > 0 {
			query = "?" + strings.Join(params, "&")
		}
		return "anytls://" + enc(password) + "@" + uriServer + ":" + p + query + "#" + encName, true
	}

	if typ == "tuic" {
		uuid := js.Or(get("uuid"), "")
		password := js.Or(get("password"), get("token"), "")
		auth := enc(uuid)
		if js.Truthy(password) {
			auth = enc(uuid) + ":" + enc(password)
		}
		var params []string
		if v := get("sni"); v != nil {
			params = append(params, "sni="+enc(v))
		}
		if alpn := get("alpn"); js.Truthy(alpn) {
			params = append(params, "alpn="+enc(joinIfArray(alpn)))
		}
		if js.Truthy(get("skip-cert-verify")) {
			params = append(params, "allow_insecure=1")
		}
		if cc := js.Or(get("congestion-controller"), get("congestion-control"), get("congestion")); js.Truthy(cc) {
			params = append(params, "congestion_control="+enc(cc))
		}
		if v := get("udp-relay-mode"); js.Truthy(v) {
			params = append(params, "udp_relay_mode="+enc(v))
		}
		if v := get("udp-over-stream"); v != nil {
			params = append(params, "udp_over_stream="+boolDigit(v))
		}
		if v := get("zero-rtt-handshake"); v != nil {
			params = append(params, "zero_rtt_handshake="+boolDigit(v))
		} else if v := get("reduce-rtt"); v != nil {
			params = append(params, "zero_rtt_handshake="+boolDigit(v))
		}
		if v := get("heartbeat"); js.Truthy(v) {
			params = append(params, "heartbeat="+enc(v))
		}
		if v := get("heartbeat-interval"); js.Truthy(v) {
			params = append(params, "heartbeat_interval="+enc(v))
		}
		if v := get("request-timeout"); js.Truthy(v) {
			params = append(params, "request_timeout="+enc(v))
		}
		if v := get("cwnd"); js.Truthy(v) {
			params = append(params, "cwnd="+enc(v))
		}
		if v := get("bbr-profile"); js.Truthy(v) {
			params = append(params, "bbr_profile="+enc(v))
		}
		if v := get("max-udp-relay-packet-size"); js.Truthy(v) {
			params = append(params, "max_udp_relay_packet_size="+enc(v))
		}
		if v := get("max-open-streams"); js.Truthy(v) {
			params = append(params, "max_open_streams="+enc(v))
		}
		if v := get("disable-sni"); v != nil {
			params = append(params, "disable_sni="+boolDigit(v))
		}
		if v := get("fast-open"); v != nil {
			params = append(params, "fast_open="+boolDigit(v))
		}
		if v := get("dialer-proxy"); js.Truthy(v) {
			params = append(params, "dp="+enc(v))
		}
		query := ""
		if len(params) > 0 {
			query = "?" + strings.Join(params, "&")
		}
		return "tuic://" + auth + "@" + uriServer + ":" + p + query + "#" + encName, true
	}

	if typ == "wireguard" {
		if !js.Truthy(get("private-key")) || !js.Truthy(get("server")) || !js.Truthy(get("port")) {
			return "", false
		}
		params := js.NewSearchParams()
		if pk := js.Or(get("public-key"), get("publicKey")); js.Truthy(pk) {
			params.Set("publickey", js.ToString(pk))
		}
		var addresses []any
		if ipv4 := js.Or(get("ip"), get("local-address")); js.Truthy(ipv4) {
			if a, isArr := ipv4.(*js.Array); isArr {
				for _, x := range a.Items {
					if js.Truthy(x) {
						addresses = append(addresses, x)
					}
				}
			} else {
				addresses = append(addresses, ipv4)
			}
		}
		if ipv6 := get("ipv6"); js.Truthy(ipv6) {
			if a, isArr := ipv6.(*js.Array); isArr {
				for _, x := range a.Items {
					if js.Truthy(x) {
						addresses = append(addresses, x)
					}
				}
			} else {
				addresses = append(addresses, ipv6)
			}
		}
		if len(addresses) > 0 {
			params.Set("address", (&js.Array{Items: uniqueValues(addresses)}).Join(","))
		}
		if ips := js.Or(get("allowed-ips"), get("allowedIPs")); js.Truthy(ips) {
			params.Set("allowedips", js.ToString(joinIfArray(ips)))
		}
		if v := get("reserved"); js.Truthy(v) {
			params.Set("reserved", js.ToString(joinIfArray(v)))
		}
		if v := get("mtu"); js.Truthy(v) {
			params.Set("mtu", js.ToString(v))
		}
		if v := get("dns"); js.Truthy(v) {
			params.Set("dns", js.ToString(joinIfArray(v)))
		}
		if v := get("persistent-keepalive"); js.Truthy(v) {
			params.Set("keepalive", js.ToString(v))
		}
		if v := js.Or(get("preshared-key"), get("presharedKey")); js.Truthy(v) {
			params.Set("presharedkey", js.ToString(v))
		}
		serverAddr := server
		if strings.Contains(serverAddr, ":") && !strings.HasPrefix(serverAddr, "[") {
			serverAddr = "[" + serverAddr + "]"
		}
		return "wireguard://" + enc(get("private-key")) + "@" + serverAddr + ":" + js.ToString(get("port")) + "?" + params.String() + "#" + encName, true
	}
	return "", false
}

// boolDigit 对应 `v ? '1' : '0'`。
func boolDigit(v any) string {
	if js.Truthy(v) {
		return "1"
	}
	return "0"
}

// uniqueValues 对应 [...new Set(values)]（SameValueZero 语义）。
func uniqueValues(values []any) []any {
	var out []any
	for _, v := range values {
		dup := false
		for _, o := range out {
			if sameValueZero(o, v) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, v)
		}
	}
	return out
}

func sameValueZero(a, b any) bool {
	if fa, ok := a.(float64); ok {
		if fb, ok := b.(float64); ok && fa != fa && fb != fb {
			return true
		}
	}
	return js.StrictEquals(a, b)
}
