package subscription

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func surgeWireGuardSections(input string) (map[string]map[string]string, error) {
	sections := map[string]map[string]string{}
	current := ""
	for _, line := range strings.Split(input, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			current = ""
			if strings.HasPrefix(strings.ToLower(line), "[wireguard ") && strings.HasSuffix(line, "]") {
				current = strings.TrimSpace(line[len("[WireGuard ") : len(line)-1])
				if current == "" || sections[current] != nil {
					return nil, fmt.Errorf("duplicate WireGuard section")
				}
				sections[current] = map[string]string{}
			}
			continue
		}
		if current != "" {
			key, value, ok := strings.Cut(line, "=")
			key = strings.ToLower(strings.TrimSpace(key))
			_, duplicate := sections[current][key]
			if !ok || duplicate {
				return nil, fmt.Errorf("invalid WireGuard section")
			}
			switch key {
			case "private-key", "self-ip", "self-ip-v6", "dns-server", "mtu", "peer":
			default:
				return nil, fmt.Errorf("unsupported WireGuard section option")
			}
			sections[current][key] = strings.TrimSpace(value)
		}
	}
	return sections, nil
}
func wireGuardINILine(line string, sections map[string]map[string]string) (string, error) {
	name, body, ok := strings.Cut(line, "=")
	if !ok {
		return "", fmt.Errorf("invalid WireGuard node")
	}
	parts, e := wireGuardParts(body)
	if e != nil || len(parts) < 2 || strings.ToLower(parts[0]) != "wireguard" {
		return "", fmt.Errorf("invalid WireGuard node")
	}
	p := map[string]any{"name": strings.TrimSpace(name), "type": "wireguard"}
	if len(parts) > 1 && strings.Contains(parts[1], "=") && !strings.HasPrefix(parts[1], "section-name") {
		return modernLoonWireGuard(parts[1:], p)
	}
	if len(parts) == 2 {
		key, value, ok := strings.Cut(parts[1], "=")
		if !ok || strings.TrimSpace(key) != "section-name" {
			return "", fmt.Errorf("invalid WireGuard reference")
		}
		s := sections[strings.TrimSpace(value)]
		if s == nil {
			return "", fmt.Errorf("WireGuard section missing")
		}
		p["private-key"] = s["private-key"]
		addresses := []any{}
		for _, field := range []string{"self-ip", "self-ip-v6"} {
			if s[field] != "" {
				addresses = append(addresses, s[field])
			}
		}
		p["ip"] = addresses
		if s["dns-server"] != "" {
			dns := []any{}
			for _, v := range strings.Split(s["dns-server"], ",") {
				dns = append(dns, strings.TrimSpace(v))
			}
			p["dns"] = dns
		}
		if s["mtu"] != "" {
			n, e := strconv.Atoi(s["mtu"])
			if e != nil {
				return "", fmt.Errorf("invalid WireGuard MTU")
			}
			p["mtu"] = n
		}
		peer := s["peer"]
		if !strings.HasPrefix(peer, "(") || !strings.HasSuffix(peer, ")") {
			return "", fmt.Errorf("invalid WireGuard peer")
		}
		fields, e := iniParts(peer[1 : len(peer)-1])
		if e != nil {
			return "", e
		}
		seen := map[string]bool{}
		for _, field := range fields {
			k, v, ok := strings.Cut(field, "=")
			k = strings.TrimSpace(k)
			if !ok || seen[k] {
				return "", fmt.Errorf("invalid WireGuard peer option")
			}
			seen[k] = true
			v, e = iniString(v)
			if e != nil {
				return "", e
			}
			switch k {
			case "public-key", "preshared-key":
				p[k] = v
			case "endpoint":
				host, port, e := net.SplitHostPort(v)
				if e != nil {
					return "", fmt.Errorf("invalid WireGuard endpoint")
				}
				n, e := strconv.Atoi(port)
				if e != nil {
					return "", fmt.Errorf("invalid WireGuard port")
				}
				p["server"], p["port"] = host, n
			case "allowed-ips":
				values := []any{}
				for _, value := range strings.Split(v, ",") {
					values = append(values, strings.TrimSpace(value))
				}
				p[k] = values
			case "keepalive":
				n, e := strconv.Atoi(v)
				if e != nil {
					return "", fmt.Errorf("invalid keepalive")
				}
				p["persistent-keepalive"] = n
			case "client-id":
				if e = wireGuardReserved(p, v); e != nil {
					return "", e
				}
			default:
				return "", fmt.Errorf("unsupported WireGuard peer field")
			}
		}
	} else {
		if len(parts) < 4 {
			return "", fmt.Errorf("invalid Loon WireGuard node")
		}
		p["server"] = parts[1]
		port, e := strconv.Atoi(parts[2])
		if e != nil {
			return "", fmt.Errorf("invalid port")
		}
		p["port"] = port
		private, e := iniString(parts[3])
		if e != nil {
			return "", e
		}
		p["private-key"] = private
		seen := map[string]bool{}
		for _, part := range parts[4:] {
			k, v, ok := strings.Cut(part, "=")
			k = strings.TrimSpace(k)
			if !ok || seen[k] {
				return "", fmt.Errorf("invalid Loon WireGuard option")
			}
			seen[k] = true
			v, e = iniString(v)
			if e != nil {
				return "", e
			}
			switch k {
			case "public-key":
				p[k] = v
			case "self-ip":
				p["ip"] = []any{v}
			case "mtu":
				n, e := strconv.Atoi(v)
				if e != nil {
					return "", fmt.Errorf("invalid MTU")
				}
				p[k] = n
			case "client-id":
				if e = wireGuardReserved(p, v); e != nil {
					return "", e
				}
			default:
				return "", fmt.Errorf("unsupported Loon WireGuard field")
			}
		}
	}
	return extendedInventory(p, "clash")
}

// Loon's template renderer uses nested peers / reserved arrays. Keep this
// grammar separate from general INI; neither scripts nor remote peers execute.
func wireGuardParts(input string) ([]string, error) {
	parts, stack := []string{}, []rune{}
	start, quoted, escaped := 0, false, false
	for i, r := range input {
		if escaped {
			escaped = false
			continue
		}
		if quoted && r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			quoted = !quoted
			continue
		}
		if quoted {
			continue
		}
		switch r {
		case '[', '{':
			stack = append(stack, r)
			if len(stack) > 3 {
				return nil, fmt.Errorf("WireGuard nesting limit")
			}
		case ']', '}':
			if len(stack) == 0 || (r == ']' && stack[len(stack)-1] != '[') || (r == '}' && stack[len(stack)-1] != '{') {
				return nil, fmt.Errorf("invalid WireGuard delimiter")
			}
			stack = stack[:len(stack)-1]
		case ',':
			if len(stack) == 0 {
				parts = append(parts, strings.TrimSpace(input[start:i]))
				start = i + 1
			}
		}
	}
	if quoted || escaped || len(stack) != 0 {
		return nil, fmt.Errorf("unclosed WireGuard field")
	}
	return append(parts, strings.TrimSpace(input[start:])), nil
}

func modernLoonWireGuard(parts []string, p map[string]any) (string, error) {
	seen := map[string]bool{}
	addresses, dns := []any{}, []any{}
	peerText := ""
	for _, part := range parts {
		key, raw, ok := strings.Cut(part, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok || seen[key] {
			return "", fmt.Errorf("invalid Loon WireGuard field")
		}
		seen[key] = true
		if key == "peers" {
			peerText = strings.TrimSpace(raw)
			continue
		}
		value, e := iniString(raw)
		if e != nil {
			return "", e
		}
		switch key {
		case "interface-ip", "interface-ipv6":
			address, _, _ := strings.Cut(value, "/")
			ip := net.ParseIP(address)
			if ip == nil || (key == "interface-ip") != (ip.To4() != nil) {
				return "", fmt.Errorf("invalid Loon interface family")
			}
			addresses = append(addresses, value)
		case "dns", "dnsv6":
			ip := net.ParseIP(value)
			if ip == nil || (key == "dns") != (ip.To4() != nil) {
				return "", fmt.Errorf("invalid Loon DNS family")
			}
			dns = append(dns, value)
		case "private-key":
			p["private-key"] = value
		case "mtu":
			n, e := strconv.Atoi(value)
			if e != nil {
				return "", fmt.Errorf("invalid WireGuard MTU")
			}
			p["mtu"] = n
		default:
			return "", fmt.Errorf("unsupported Loon WireGuard field")
		}
	}
	p["ip"] = addresses
	if len(dns) > 0 {
		p["dns"] = dns
	}
	if !strings.HasPrefix(peerText, "[{") || !strings.HasSuffix(peerText, "}]") {
		return "", fmt.Errorf("one WireGuard peer required")
	}
	fields, e := wireGuardParts(peerText[2 : len(peerText)-2])
	if e != nil {
		return "", e
	}
	seen = map[string]bool{}
	for _, field := range fields {
		key, raw, ok := strings.Cut(field, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok || seen[key] {
			return "", fmt.Errorf("invalid Loon peer field")
		}
		seen[key] = true
		if key == "reserved" {
			v := strings.TrimSpace(raw)
			if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
				return "", fmt.Errorf("invalid WireGuard reserved array")
			}
			values := strings.Split(v[1:len(v)-1], ",")
			for i := range values {
				values[i] = strings.TrimSpace(values[i])
			}
			if e := wireGuardReserved(p, strings.Join(values, "/")); e != nil {
				return "", e
			}
			continue
		}
		value, e := iniString(raw)
		if e != nil {
			return "", e
		}
		switch key {
		case "public-key", "preshared-key":
			p[key] = value
		case "endpoint":
			host, port, e := net.SplitHostPort(value)
			if e != nil {
				return "", fmt.Errorf("invalid WireGuard endpoint")
			}
			n, e := strconv.Atoi(port)
			if e != nil {
				return "", fmt.Errorf("invalid WireGuard port")
			}
			p["server"], p["port"] = host, n
		case "allowed-ips":
			values := []any{}
			for _, v := range strings.Split(value, ",") {
				values = append(values, strings.TrimSpace(v))
			}
			p[key] = values
		default:
			return "", fmt.Errorf("unsupported Loon peer field")
		}
	}
	return extendedInventory(p, "clash")
}
func wireGuardReserved(p map[string]any, value string) error {
	parts := strings.Split(value, "/")
	bytes := []any{}
	for _, v := range parts {
		n, e := strconv.Atoi(v)
		if e != nil {
			return fmt.Errorf("invalid WireGuard client ID")
		}
		bytes = append(bytes, n)
	}
	p["reserved"] = bytes
	return nil
}
