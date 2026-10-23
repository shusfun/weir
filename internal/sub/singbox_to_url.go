package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/utils/singbox-to-url.js

func toArray(v any) *js.Array {
	if js.IsNullish(v) {
		return js.Arr()
	}
	if a, ok := v.(*js.Array); ok {
		return a
	}
	return js.Arr(v)
}

func b64url(input any) string {
	str, ok := input.(string)
	if !ok {
		str = js.MustStringify(input)
	}
	return strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(js.Base64EncodeUTF8(str)), "=")
}

type paramList [][2]any

func tlsParams(outbound *js.Object) paramList {
	tls := js.AsObject(js.Or(outbound.Get("tls"), js.NewObject()))
	var params paramList
	if js.Truthy(objGet(tls, "enabled")) {
		if v := objGet(tls, "server_name"); js.Truthy(v) {
			params = append(params, [2]any{"sni", v})
		}
		if js.Truthy(objGet(tls, "insecure")) {
			params = append(params, [2]any{"allowInsecure", "1"})
		}
		if v := objGet(tls, "alpn"); js.Truthy(v) {
			params = append(params, [2]any{"alpn", toArray(v).Join(",")})
		}
		reality := js.Or(objGet(tls, "reality"), js.NewObject())
		if js.Truthy(objGet(reality, "enabled")) {
			params = append(params, [2]any{"security", "reality"})
			if v := objGet(reality, "public_key"); js.Truthy(v) {
				params = append(params, [2]any{"pbk", v})
			}
			if v := objGet(reality, "short_id"); js.Truthy(v) {
				params = append(params, [2]any{"sid", v})
			}
		} else {
			params = append(params, [2]any{"security", "tls"})
		}
	}
	if v := outbound.Get("flow"); js.Truthy(v) {
		params = append(params, [2]any{"flow", v})
	}
	transport := js.Or(outbound.Get("transport"), js.NewObject())
	if t := objGet(transport, "type"); js.Truthy(t) && !js.StrictEquals(t, "tcp") {
		params = append(params, [2]any{"type", t})
		if v := objGet(transport, "path"); js.Truthy(v) {
			params = append(params, [2]any{"path", v})
		}
		if v := objGet(transport, "host"); js.Truthy(v) {
			params = append(params, [2]any{"host", v})
		}
		if v := objGet(transport, "service_name"); js.Truthy(v) {
			params = append(params, [2]any{"serviceName", v})
		}
	}
	return params
}

func buildQuery(params paramList) string {
	search := js.NewSearchParams()
	for _, kv := range params {
		v := kv[1]
		if v != nil && !js.IsNull(v) && !js.StrictEquals(v, "") {
			search.Set(js.ToString(kv[0]), js.ToString(v))
		}
	}
	if qs := search.String(); qs != "" {
		return "?" + qs
	}
	return ""
}

// SingboxOutboundToURL 对应 singboxOutboundToUrl；不支持的类型返回 nil（null）。
func SingboxOutboundToURL(obVal any) any {
	ob, ok := obVal.(*js.Object)
	if !ok {
		// 数组也是 object，但其属性访问均为 undefined，最终因缺少 server 返回 null
		return js.Null
	}
	typ := js.ToLower(js.ToString(js.Or(ob.Get("type"), "")))
	server := ob.Get("server")
	port := js.Coalesce(ob.Get("server_port"), ob.Get("port"))
	name := js.Or(ob.Get("tag"), "")
	if !js.Truthy(server) || !js.Truthy(port) {
		return js.Null
	}
	hash := ""
	if js.Truthy(name) {
		hash = "#" + enc(name)
	}
	hostPort := js.ToString(server) + ":" + js.ToString(port)
	switch typ {
	case "vless":
		params := tlsParams(ob)
		if e := ob.Get("encryption"); js.Truthy(e) {
			params = append(params, [2]any{"encryption", e})
		} else {
			params = append(params, [2]any{"encryption", "none"})
		}
		return "vless://" + js.ToString(ob.Get("uuid")) + "@" + hostPort + buildQuery(params) + hash
	case "vmess":
		transport := ob.Get("transport")
		tlsV := ob.Get("tls")
		tlsStr := ""
		if js.Truthy(tlsV) && js.Truthy(objGet(tlsV, "enabled")) {
			tlsStr = "tls"
		}
		obj := js.Obj(
			"v", "2",
			"ps", name,
			"add", server,
			"port", js.ToString(port),
			"id", ob.Get("uuid"),
			"aid", js.ToString(js.Coalesce(ob.Get("alter_id"), 0.0)),
			"scy", js.Or(ob.Get("security"), "auto"),
			"net", js.Or(andGet(transport, "type"), "tcp"),
			"type", "none",
			"host", js.Or(andGet(transport, "host"), ""),
			"path", js.Or(andGet(transport, "path"), ""),
			"tls", tlsStr,
			"sni", js.Or(andGet(tlsV, "server_name"), ""),
		)
		return "vmess://" + b64url(js.MustStringify(obj))
	case "trojan":
		params := tlsParams(ob)
		return "trojan://" + enc(js.Or(ob.Get("password"), "")) + "@" + hostPort + buildQuery(params) + hash
	case "shadowsocks":
		method := js.Or(ob.Get("method"), "aes-256-gcm")
		pwd := js.Or(ob.Get("password"), "")
		userinfo := strings.TrimRight(js.Base64EncodeUTF8(js.ToString(method)+":"+js.ToString(pwd)), "=")
		return "ss://" + userinfo + "@" + hostPort + hash
	case "hysteria2", "hy2":
		params := tlsParams(ob)
		obfs := ob.Get("obfs")
		if js.Truthy(obfs) && js.Truthy(objGet(obfs, "type")) {
			params = append(params, [2]any{"obfs", objGet(obfs, "type")})
			if p := objGet(obfs, "password"); js.Truthy(p) {
				params = append(params, [2]any{"obfs-password", p})
			}
		}
		return "hysteria2://" + enc(js.Or(ob.Get("password"), "")) + "@" + hostPort + buildQuery(params) + hash
	case "tuic":
		params := tlsParams(ob)
		if cc := ob.Get("congestion_control"); js.Truthy(cc) {
			params = append(params, [2]any{"congestion_control", cc})
		}
		auth := js.ToString(js.Or(ob.Get("uuid"), "")) + ":" + js.ToString(js.Or(ob.Get("password"), ""))
		return "tuic://" + auth + "@" + hostPort + buildQuery(params) + hash
	case "anytls":
		params := tlsParams(ob)
		return "anytls://" + enc(js.Or(ob.Get("password"), "")) + "@" + hostPort + buildQuery(params) + hash
	}
	return js.Null
}

// andGet 对应 `(v && v.key)`：v 为假值时返回 v 本身。
func andGet(v any, key string) any {
	if !js.Truthy(v) {
		return v
	}
	return objGet(v, key)
}

// enc 对应 encodeURIComponent(v)（非字符串先做 String 转换）。
func enc(v any) string { return js.EncodeURIComponent(js.ToString(v)) }

// ExtractSingboxNodes 对应 extractSingboxNodes。
func ExtractSingboxNodes(config any) []string {
	cfg, ok := config.(*js.Object)
	if !ok {
		return []string{}
	}
	out := []string{}
	for _, ob := range toArray(cfg.Get("outbounds")).Items {
		if u, ok := SingboxOutboundToURL(ob).(string); ok && u != "" {
			out = append(out, u)
		}
	}
	return out
}
