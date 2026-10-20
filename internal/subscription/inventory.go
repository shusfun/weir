package subscription

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DecodeInventory imports node inventories, not client listeners or routing.
// It never resolves providers or evaluates YAML tags/templates. Unsupported
// node fields fail the entire import so credentials/settings aren't lost.
func DecodeInventory(body []byte) ([]string, error) {
	return decodeInventory(body, false)
}

func decodeInventory(body []byte, plusAsSpace bool) ([]string, error) {
	if len(body) > MaxBodyBytes {
		return nil, fmt.Errorf("inventory size limit")
	}
	input := strings.TrimSpace(string(body))
	if decoded, ok := tryDecodeBase64(input); ok {
		input = strings.TrimSpace(decoded)
	}
	var root map[string]any
	if strings.HasPrefix(input, "{") {
		if err := json.Unmarshal([]byte(input), &root); err != nil {
			return nil, fmt.Errorf("invalid inventory JSON")
		}
	} else if strings.HasPrefix(input, "proxies:") || strings.Contains(input, "\nproxies:") || strings.HasPrefix(input, "---") {
		decoder := yaml.NewDecoder(strings.NewReader(input))
		if err := decoder.Decode(&root); err != nil {
			return nil, fmt.Errorf("invalid inventory YAML")
		}
		var next any
		if err := decoder.Decode(&next); err != io.EOF {
			return nil, fmt.Errorf("multiple inventory documents not supported")
		}
	} else {
		for _, first := range strings.Split(input, "\n") {
			first = strings.TrimSpace(first)
			if first == "" || strings.HasPrefix(first, "#") || strings.HasPrefix(first, ";") {
				continue
			}
			if strings.HasPrefix(first, "[") {
				return decodeINIInventory(input)
			}
			break
		}
		links := []string{}
		for _, line := range strings.Split(input, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}
			scheme, _, hasScheme := strings.Cut(line, "://")
			if !hasScheme || strings.ContainsAny(scheme, "=, \t[]") {
				link, err := inventoryINILine(line)
				if err != nil {
					return nil, err
				}
				links = append(links, link)
				continue
			}
			if plusAsSpace {
				line = normalizeURINamePlus(line)
			}
			links = append(links, line)
		}
		return validateInventoryLinks(links)
	}
	format, key := "clash", "proxies"
	if _, ok := root["outbounds"]; ok || root["endpoints"] != nil {
		format, key = "singbox", "outbounds"
	}
	if providers, exists := root["proxy-providers"]; exists && providers != nil {
		return nil, fmt.Errorf("external providers require separate sources")
	}
	items, ok := root[key].([]any)
	if format == "singbox" && root[key] == nil && root["endpoints"] != nil {
		items = []any{}
		ok = true
	}
	if !ok || len(items) > 10000 {
		return nil, fmt.Errorf("missing or excessive inventory nodes")
	}
	links := []string{}
	if format == "singbox" {
		if raw, exists := root["endpoints"]; exists {
			endpoints, ok := raw.([]any)
			if !ok || len(endpoints) > 10000 {
				return nil, fmt.Errorf("invalid endpoints")
			}
			for _, item := range endpoints {
				p, e := objectValue(item)
				if e != nil {
					return nil, e
				}
				link, e := wireGuardEndpointInventory(p)
				if e != nil {
					return nil, e
				}
				links = append(links, link)
			}
		}
	}
	for _, raw := range items {
		p, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid inventory node")
		}
		if format == "singbox" {
			switch stringValue(p["type"]) {
			case "selector", "urltest", "direct", "block", "dns":
				continue
			}
		}
		link, err := inventoryNode(p, format)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return validateInventoryLinks(links)
}

func validateInventoryLinks(links []string) ([]string, error) {
	if len(links) == 0 || len(links) > 10000 {
		return nil, fmt.Errorf("empty or excessive inventory")
	}
	parsed, err := ParseLinks(links)
	if err != nil || len(parsed.Skipped) > 0 {
		return nil, fmt.Errorf("invalid inventory node")
	}
	for _, n := range parsed.Nodes {
		var settings Settings
		json.Unmarshal([]byte(n.SettingsJSON), &settings)
		if settings.VmessUnsupported || len(settings.UnsupportedFields) > 0 {
			return nil, fmt.Errorf("unsupported inventory node fields")
		}
	}
	return links, nil
}
func stringValue(value any) string { s, _ := value.(string); return s }
func requireKeys(p map[string]any, keys ...string) error {
	allowed := map[string]bool{}
	for _, key := range keys {
		allowed[key] = true
	}
	for key, value := range p {
		if !allowed[key] {
			return fmt.Errorf("unsupported inventory node field %s", key)
		}
		switch key {
		case "type", "server", "name", "tag", "password", "uuid", "username", "flow", "network", "cipher", "method", "security", "version", "servername", "server_name", "client-fingerprint", "fingerprint", "obfs-password", "congestion-controller", "congestion_control", "path", "service_name", "grpc-service-name", "Host", "public-key", "short-id", "public_key", "short_id":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("invalid string field %s", key)
			}
		case "enabled", "insecure", "udp", "skip-cert-verify", "reduce-rtt", "zero_rtt_handshake":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("invalid boolean field %s", key)
			}
		}
	}
	return nil
}
func objectValue(value any) (map[string]any, error) {
	p, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid inventory object")
	}
	return p, nil
}
func stringList(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	raw, ok := value.([]any)
	if !ok {
		if s, ok := value.(string); ok {
			return []string{s}, nil
		}
		return nil, fmt.Errorf("invalid string list")
	}
	result := []string{}
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("invalid list member")
		}
		result = append(result, s)
	}
	return result, nil
}

func inventoryNode(p map[string]any, format string) (string, error) {
	kind := stringValue(p["type"])
	if kind == "snell" || kind == "wireguard" {
		return extendedInventory(p, format)
	}
	if kind == "ss" {
		kind = "shadowsocks"
	}
	if kind == "socks" {
		kind = "socks5"
	}
	common := []string{"type", "server"}
	if format == "clash" {
		common = append(common, "name", "port", "udp")
		if kind != "shadowsocks" {
			common = append(common, "tls", "servername", "alpn", "skip-cert-verify", "client-fingerprint", "fingerprint")
		}
		if kind == "vless" || kind == "vmess" || kind == "trojan" {
			common = append(common, "network", "ws-opts", "grpc-opts")
		}
	} else {
		common = append(common, "tag", "server_port")
		if kind != "shadowsocks" {
			common = append(common, "tls")
		}
		if kind == "vless" || kind == "vmess" || kind == "trojan" {
			common = append(common, "transport")
		}
	}
	switch kind {
	case "vless":
		common = append(common, "uuid", "flow")
		if format == "clash" {
			common = append(common, "reality-opts")
		}
	case "vmess":
		common = append(common, "uuid")
		if format == "clash" {
			common = append(common, "cipher", "alterId")
		} else {
			common = append(common, "security", "alter_id")
		}
	case "trojan", "anytls":
		common = append(common, "password")
	case "http", "socks5":
		common = append(common, "username", "password")
		if kind == "socks5" && format == "singbox" {
			common = append(common, "version")
		}
	case "shadowsocks":
		common = append(common, "password")
		if format == "clash" {
			common = append(common, "cipher")
		} else {
			common = append(common, "method")
		}
	case "hysteria2":
		common = append(common, "password", "obfs")
		if format == "clash" {
			common = append(common, "obfs-password")
		}
	case "tuic":
		common = append(common, "uuid", "password")
		if format == "clash" {
			common = append(common, "congestion-controller", "reduce-rtt")
		} else {
			common = append(common, "congestion_control", "zero_rtt_handshake")
		}
	default:
		return "", fmt.Errorf("unsupported inventory protocol")
	}
	if err := requireKeys(p, common...); err != nil {
		return "", err
	}
	q := url.Values{}
	network := stringValue(p["network"])
	if network != "" {
		q.Set("type", network)
	}
	if flow := stringValue(p["flow"]); flow != "" {
		if kind != "vless" {
			return "", fmt.Errorf("flow requires VLESS")
		}
		q.Set("flow", flow)
	}
	name, port := stringValue(p["name"]), p["port"]
	if format == "singbox" {
		name, port = stringValue(p["tag"]), p["server_port"]
	}
	portNumber, err := strconv.Atoi(fmt.Sprint(port))
	if err != nil || portNumber <= 0 || portNumber > 65535 {
		return "", fmt.Errorf("invalid inventory port")
	}
	host := stringValue(p["server"])
	if host == "" {
		return "", fmt.Errorf("missing inventory server")
	}
	username, password := stringValue(p["uuid"]), stringValue(p["password"])
	if kind == "http" || kind == "socks5" {
		username = stringValue(p["username"])
	}
	if format == "clash" {
		if udp, exists := p["udp"]; exists && udp != true {
			return "", fmt.Errorf("UDP setting cannot be represented in URI")
		}
		if p["tls"] == true {
			q.Set("security", "tls")
		} else if p["tls"] == false {
			if kind == "anytls" || kind == "hysteria2" || kind == "tuic" {
				return "", fmt.Errorf("protocol requires TLS")
			}
			q.Set("security", "none")
		} else if value, exists := p["tls"]; exists && value != false {
			return "", fmt.Errorf("invalid TLS flag")
		}
		for field, key := range map[string]string{"servername": "sni", "client-fingerprint": "fp", "fingerprint": "pinSHA256"} {
			if value := stringValue(p[field]); value != "" {
				q.Set(key, value)
			}
		}
		if p["skip-cert-verify"] == true {
			q.Set("insecure", "1")
		}
		alpn, e := stringList(p["alpn"])
		if e != nil {
			return "", e
		}
		if len(alpn) > 0 {
			q.Set("alpn", strings.Join(alpn, ","))
		}
		if raw, exists := p["reality-opts"]; exists {
			r, e := objectValue(raw)
			if e != nil {
				return "", e
			}
			if e = requireKeys(r, "public-key", "short-id"); e != nil {
				return "", e
			}
			q.Set("security", "reality")
			q.Set("pbk", stringValue(r["public-key"]))
			q.Set("sid", stringValue(r["short-id"]))
		}
		if raw, exists := p["ws-opts"]; exists {
			if network != "ws" {
				return "", fmt.Errorf("misplaced WS options")
			}
			ws, e := objectValue(raw)
			if e != nil {
				return "", e
			}
			if e = requireKeys(ws, "path", "headers"); e != nil {
				return "", e
			}
			q.Set("path", stringValue(ws["path"]))
			if raw, exists = ws["headers"]; exists {
				headers, e := objectValue(raw)
				if e != nil {
					return "", e
				}
				if e = requireKeys(headers, "Host"); e != nil {
					return "", e
				}
				q.Set("host", stringValue(headers["Host"]))
			}
		}
		if raw, exists := p["grpc-opts"]; exists {
			if network != "grpc" {
				return "", fmt.Errorf("misplaced gRPC options")
			}
			g, e := objectValue(raw)
			if e != nil {
				return "", e
			}
			if e = requireKeys(g, "grpc-service-name"); e != nil {
				return "", e
			}
			q.Set("serviceName", stringValue(g["grpc-service-name"]))
		}
		if p["obfs"] != nil {
			if _, ok := p["obfs"].(string); !ok {
				return "", fmt.Errorf("invalid obfs type")
			}
			q.Set("obfs", stringValue(p["obfs"]))
			q.Set("obfs-password", stringValue(p["obfs-password"]))
		}
		if v := stringValue(p["congestion-controller"]); v != "" {
			q.Set("congestion_control", v)
		}
		if p["reduce-rtt"] == true {
			q.Set("zero_rtt_handshake", "1")
		}
	} else {
		if version := stringValue(p["version"]); version != "" && version != "5" {
			return "", fmt.Errorf("only SOCKS5 supported")
		}
		if raw, exists := p["tls"]; exists {
			tls, e := objectValue(raw)
			if e != nil {
				return "", e
			}
			if e = requireKeys(tls, "enabled", "server_name", "alpn", "insecure", "utls", "reality", "certificate_public_key_sha256"); e != nil {
				return "", e
			}
			if tls["enabled"] != true {
				return "", fmt.Errorf("disabled TLS object cannot retain settings")
			}
			q.Set("security", "tls")
			q.Set("sni", stringValue(tls["server_name"]))
			if tls["insecure"] == true {
				q.Set("insecure", "1")
			}
			alpn, e := stringList(tls["alpn"])
			if e != nil {
				return "", e
			}
			if len(alpn) > 0 {
				q.Set("alpn", strings.Join(alpn, ","))
			}
			for field, keys := range map[string][]string{"utls": {"enabled", "fingerprint"}, "reality": {"enabled", "public_key", "short_id"}} {
				if raw, exists := tls[field]; exists {
					nested, e := objectValue(raw)
					if e != nil {
						return "", e
					}
					if e = requireKeys(nested, keys...); e != nil {
						return "", e
					}
					if nested["enabled"] != true {
						return "", fmt.Errorf("disabled TLS feature cannot retain fields")
					}
					if field == "utls" {
						q.Set("fp", stringValue(nested["fingerprint"]))
					} else {
						q.Set("security", "reality")
						q.Set("pbk", stringValue(nested["public_key"]))
						q.Set("sid", stringValue(nested["short_id"]))
					}
				}
			}
			if raw, exists := tls["certificate_public_key_sha256"]; exists {
				pins, e := stringList(raw)
				if e != nil || len(pins) != 1 {
					return "", fmt.Errorf("exactly one SPKI pin supported")
				}
				q.Set("spki", pins[0])
			}
		}
		if raw, exists := p["transport"]; exists {
			tr, e := objectValue(raw)
			if e != nil {
				return "", e
			}
			if e = requireKeys(tr, "type", "path", "headers", "service_name"); e != nil {
				return "", e
			}
			network = stringValue(tr["type"])
			q.Set("type", network)
			switch network {
			case "ws":
				q.Set("path", stringValue(tr["path"]))
				if tr["service_name"] != nil {
					return "", fmt.Errorf("misplaced service name")
				}
				if raw, exists = tr["headers"]; exists {
					headers, e := objectValue(raw)
					if e != nil {
						return "", e
					}
					if e = requireKeys(headers, "Host"); e != nil {
						return "", e
					}
					q.Set("host", stringValue(headers["Host"]))
				}
			case "grpc":
				if tr["path"] != nil || tr["headers"] != nil {
					return "", fmt.Errorf("misplaced WS fields")
				}
				q.Set("serviceName", stringValue(tr["service_name"]))
			default:
				return "", fmt.Errorf("unsupported inventory transport")
			}
		}
		if raw, exists := p["obfs"]; exists {
			obfs, e := objectValue(raw)
			if e != nil {
				return "", e
			}
			if e = requireKeys(obfs, "type", "password"); e != nil {
				return "", e
			}
			q.Set("obfs", stringValue(obfs["type"]))
			q.Set("obfs-password", stringValue(obfs["password"]))
		}
		if v := stringValue(p["congestion_control"]); v != "" {
			q.Set("congestion_control", v)
		}
		if p["zero_rtt_handshake"] == true {
			q.Set("zero_rtt_handshake", "1")
		}
	}
	scheme := kind
	if q.Get("security") == "tls" {
		if kind == "http" {
			scheme = "https"
		}
		if kind == "socks5" {
			scheme = "socks5-tls"
		}
	}
	if scheme == "shadowsocks" {
		scheme = "ss"
	}
	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(portNumber)), RawQuery: q.Encode(), Fragment: name}
	switch kind {
	case "vless":
		u.User = url.User(username)
	case "trojan", "anytls", "hysteria2":
		u.User = url.User(password)
	case "tuic":
		u.User = url.UserPassword(username, password)
	case "http", "socks5":
		u.User = url.UserPassword(username, password)
	case "shadowsocks":
		method := stringValue(p["cipher"])
		if format == "singbox" {
			method = stringValue(p["method"])
		}
		if method == "" {
			return "", fmt.Errorf("missing SS method")
		}
		u.User = url.User(base64.RawStdEncoding.EncodeToString([]byte(method + ":" + password)))
	case "vmess":
		method, aid := stringValue(p["cipher"]), p["alterId"]
		if format == "singbox" {
			method, aid = stringValue(p["security"]), p["alter_id"]
		}
		if aid != nil && fmt.Sprint(aid) != "0" {
			return "", fmt.Errorf("legacy VMess unsupported")
		}
		if q.Get("security") == "reality" || q.Get("spki") != "" || q.Get("pinSHA256") != "" {
			return "", fmt.Errorf("unsupported VMess TLS")
		}
		v := map[string]any{"v": "2", "ps": name, "add": host, "port": portNumber, "id": username, "aid": 0, "scy": method, "net": network, "tls": q.Get("security"), "sni": q.Get("sni"), "host": q.Get("host"), "path": q.Get("path"), "fp": q.Get("fp"), "alpn": q.Get("alpn")}
		if network == "grpc" {
			v["path"] = q.Get("serviceName")
		}
		if q.Get("insecure") == "1" {
			v["allowInsecure"] = "1"
		}
		b, _ := json.Marshal(v)
		return "vmess://" + base64.StdEncoding.EncodeToString(b), nil
	default:
		return "", fmt.Errorf("unsupported inventory protocol")
	}
	return u.String(), nil
}
