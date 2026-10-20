package subscription

// Adapted from MiSub 4952c16 functions/modules/utils/node-parser.js (MIT).
// Unlike its permissive parser, unknown/ambiguous options reject the import.
// Only local proxy sections are inventory; policy/rule sections are not applied.
import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func iniParts(line string) ([]string, error) {
	var parts []string
	start, quoted, escaped := 0, false, false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quoted {
			escaped = true
			continue
		}
		if r == '"' {
			quoted = !quoted
		}
		if r == ',' && !quoted {
			parts = append(parts, strings.TrimSpace(line[start:i]))
			start = i + 1
		}
	}
	if quoted || escaped {
		return nil, fmt.Errorf("unclosed INI quote")
	}
	return append(parts, strings.TrimSpace(line[start:])), nil
}

func iniString(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) {
		v, e := strconv.Unquote(s)
		if e != nil {
			return "", fmt.Errorf("invalid INI quote")
		}
		if strings.ContainsAny(v, "\r\n\x00") {
			return "", fmt.Errorf("invalid INI value")
		}
		return v, nil
	}
	if strings.ContainsAny(s, "\"\r\n\x00") {
		return "", fmt.Errorf("invalid INI value")
	}
	return s, nil
}

func decodeINIInventory(input string) ([]string, error) {
	wgSections, err := surgeWireGuardSections(input)
	if err != nil {
		return nil, err
	}
	links, section, sectioned := []string{}, "", false
	for index, line := range strings.Split(input, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("invalid INI section at line %d", index+1)
			}
			section, sectioned = strings.ToLower(strings.TrimSpace(line[1:len(line)-1])), true
			continue
		}
		if sectioned && section != "proxy" && section != "server_local" {
			if section == "server_remote" || section == "proxy-provider" {
				return nil, fmt.Errorf("remote INI providers require separate sources")
			}
			continue
		}
		_, body, _ := strings.Cut(line, "=")
		kind, _, _ := strings.Cut(body, ",")
		var link string
		var err error
		if strings.EqualFold(strings.TrimSpace(kind), "wireguard") {
			link, err = wireGuardINILine(line, wgSections)
		} else {
			link, err = inventoryINILine(line)
		}
		if err != nil {
			return nil, fmt.Errorf("invalid INI node at line %d: %w", index+1, err)
		}
		links = append(links, link)
		if len(links) > 10000 {
			return nil, fmt.Errorf("excessive inventory nodes")
		}
	}
	return validateInventoryLinks(links)
}

func inventoryINILine(line string) (string, error) {
	left, right, ok := strings.Cut(line, "=")
	if !ok {
		return "", fmt.Errorf("missing node assignment")
	}
	parts, err := iniParts(right)
	if err != nil {
		return "", err
	}
	if len(parts) > 0 && strings.EqualFold(parts[0], "wireguard") {
		return wireGuardINILine(line, nil)
	}
	left = strings.TrimSpace(left)
	p := map[string]any{}
	start := 0
	// QX has protocol=host:port; Surge/Loon have name=protocol,host,port.
	if len(parts) >= 3 && !strings.Contains(parts[1], "=") && !strings.Contains(parts[2], "=") {
		p["name"], p["type"], p["server"] = left, strings.ToLower(parts[0]), parts[1]
		port, e := strconv.Atoi(parts[2])
		if e != nil {
			return "", fmt.Errorf("invalid port")
		}
		p["port"] = port
		start = 3
	} else {
		if len(parts) == 0 {
			return "", fmt.Errorf("missing endpoint")
		}
		host, port, e := net.SplitHostPort(parts[0])
		if e != nil {
			return "", fmt.Errorf("invalid endpoint")
		}
		pn, e := strconv.Atoi(port)
		if e != nil {
			return "", fmt.Errorf("invalid port")
		}
		p["type"], p["server"], p["port"], p["name"] = strings.ToLower(left), host, pn, "Untitled"
		start = 1
	}
	kind := p["type"].(string)
	if kind == "ss" {
		kind = "shadowsocks"
	}
	if kind == "hy2" {
		kind = "hysteria2"
	}
	if kind == "https" {
		kind = "http"
		p["tls"] = true
	}
	if kind == "socks5-tls" {
		kind = "socks5"
		p["tls"] = true
	}
	p["type"] = kind
	options := map[string]string{}
	var positional []string
	for _, part := range parts[start:] {
		key, value, has := strings.Cut(part, "=")
		// A quoted positional password may itself contain '='.
		if strings.HasPrefix(part, `"`) {
			has = false
		}
		if !has {
			v, e := iniString(part)
			if e != nil {
				return "", e
			}
			positional = append(positional, v)
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		v, e := iniString(value)
		if e != nil {
			return "", e
		}
		if _, exists := options[key]; exists {
			return "", fmt.Errorf("duplicate INI option")
		}
		options[key] = v
	}
	if kind == "snell" {
		if len(positional) > 0 {
			return "", fmt.Errorf("Snell requires named PSK")
		}
		for key, v := range options {
			switch key {
			case "psk":
				p["psk"] = v
			case "version":
				n, e := strconv.Atoi(v)
				if e != nil {
					return "", fmt.Errorf("invalid Snell version")
				}
				p[key] = n
			case "reuse", "tfo", "udp-relay", "ecn":
				if v != "true" && v != "false" {
					return "", fmt.Errorf("invalid Snell boolean")
				}
				field := key
				if field == "udp-relay" {
					field = "udp"
				}
				p[field] = v == "true"
			case "obfs", "obfs-host":
			default:
				return "", fmt.Errorf("unsupported Snell option")
			}
		}
		if options["obfs"] != "" || options["obfs-host"] != "" {
			p["obfs-opts"] = map[string]any{"mode": options["obfs"], "host": options["obfs-host"]}
		}
		return extendedInventory(p, "clash")
	}
	set := func(key string, v any) error {
		if old, exists := p[key]; exists && fmt.Sprint(old) != fmt.Sprint(v) {
			return fmt.Errorf("conflicting INI options")
		}
		p[key] = v
		return nil
	}
	credential := "password"
	if kind == "vless" || kind == "vmess" {
		credential = "uuid"
	}
	if len(positional) > 0 {
		switch kind {
		case "shadowsocks", "vmess":
			if len(positional) != 2 {
				return "", fmt.Errorf("expected cipher and credential")
			}
			p["cipher"], p[credential] = positional[0], positional[1]
		case "http", "socks5":
			if len(positional) != 2 {
				return "", fmt.Errorf("expected username and password")
			}
			p["username"], p["password"] = positional[0], positional[1]
		default:
			if len(positional) != 1 {
				return "", fmt.Errorf("unexpected positional options")
			}
			p[credential] = positional[0]
		}
	}
	for key, v := range options {
		field := ""
		var value any = v
		switch key {
		case "tag":
			p["name"] = v
		case "password", "auth", "uuid":
			field = credential
		case "username":
			field = "username"
			if credential == "uuid" {
				field = "uuid"
			}
		case "encrypt-method", "cipher", "method":
			field = "cipher"
		case "sni", "tls-host":
			field = "servername"
		case "client-fingerprint":
			field = key
		case "flow", "vless-flow":
			field = "flow"
		case "reality", "public-key", "short-id":
			continue // validated together below
		case "alpn":
			field = "alpn"
			value = []any{v}
		case "tls", "over-tls", "skip-cert-verify", "tls-verification", "udp-relay", "tfo", "fast-open", "vmess-aead":
			if v != "true" && v != "false" {
				return "", fmt.Errorf("invalid INI boolean")
			}
			switch key {
			case "tls", "over-tls":
				field = "tls"
				value = v == "true"
			case "skip-cert-verify":
				field = key
				value = v == "true"
			case "tls-verification":
				field = "skip-cert-verify"
				value = v == "false"
			case "udp-relay":
				if v != "true" {
					return "", fmt.Errorf("UDP disable cannot be preserved")
				}
			case "tfo", "fast-open":
				if v != "false" {
					return "", fmt.Errorf("TCP fast open not adapted")
				}
			case "vmess-aead":
				if kind != "vmess" || v != "true" {
					return "", fmt.Errorf("legacy VMess not adapted")
				}
			}
		case "alterid":
			if kind != "vmess" || v != "0" {
				return "", fmt.Errorf("legacy VMess not adapted")
			}
		case "ws", "transport", "ws-path", "path", "ws-headers", "host", "obfs", "obfs-uri", "obfs-host":
			continue // validated as one transport below
		default:
			return "", fmt.Errorf("unsupported INI option")
		}
		if field != "" {
			if err := set(field, value); err != nil {
				return "", err
			}
		}
	}
	if reality, exists := options["reality"]; exists || options["public-key"] != "" || options["short-id"] != "" {
		if kind != "vless" || reality != "true" || options["public-key"] == "" {
			return "", fmt.Errorf("invalid INI REALITY options")
		}
		p["reality-opts"] = map[string]any{"public-key": options["public-key"], "short-id": options["short-id"]}
	}
	network := ""
	path, host := "", ""
	for key, v := range options {
		switch key {
		case "ws":
			if v != "true" && v != "false" {
				return "", fmt.Errorf("invalid WS flag")
			}
			if v == "true" {
				if network != "" && network != "ws" {
					return "", fmt.Errorf("conflicting transport")
				}
				network = "ws"
			}
		case "transport":
			if v != "ws" && v != "tcp" {
				return "", fmt.Errorf("unsupported INI transport")
			}
			if network != "" && network != v {
				return "", fmt.Errorf("conflicting transport")
			}
			network = v
		case "obfs":
			if v == "over-tls" {
				if err := set("tls", true); err != nil {
					return "", err
				}
			} else if v == "ws" || v == "wss" {
				if network != "" && network != "ws" {
					return "", fmt.Errorf("conflicting transport")
				}
				network = "ws"
				if v == "wss" {
					if err := set("tls", true); err != nil {
						return "", err
					}
				}
			} else {
				return "", fmt.Errorf("unsupported INI obfuscation")
			}
		case "ws-path", "path", "obfs-uri":
			if path != "" && path != v {
				return "", fmt.Errorf("conflicting WS path")
			}
			path = v
		case "host", "obfs-host", "ws-headers":
			if key == "ws-headers" {
				if !strings.HasPrefix(v, "Host:") {
					return "", fmt.Errorf("unsupported WS headers")
				}
				v = strings.TrimPrefix(v, "Host:")
			}
			if host != "" && host != v {
				return "", fmt.Errorf("conflicting WS host")
			}
			host = v
		}
	}
	if network == "ws" {
		if kind != "vmess" && kind != "vless" && kind != "trojan" {
			return "", fmt.Errorf("unsupported WS protocol")
		}
		p["network"] = "ws"
		ws := map[string]any{}
		if path != "" {
			ws["path"] = path
		}
		if host != "" {
			ws["headers"] = map[string]any{"Host": host}
		}
		p["ws-opts"] = ws
		if kind == "vmess" && options["obfs-host"] != "" {
			if err := set("servername", host); err != nil {
				return "", err
			}
		}
	} else if path != "" || host != "" {
		return "", fmt.Errorf("transport options without WS")
	}
	return inventoryNode(p, "clash")
}
