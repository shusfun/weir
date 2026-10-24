package subout

import (
	"fmt"
	"github.com/shusfun/weir/internal/subscription"
	"net"
	"regexp"
	"strconv"
	"strings"
)

func wireGuardINI(n subscription.ParsedLink, s subscription.Settings, name, target string) (string, string, error) {
	v := s.WireGuard
	if v == nil || target == "quanx" {
		return "", "", fmt.Errorf("WireGuard unsupported by selected target")
	}
	v4, v6 := "", ""
	for _, address := range v.Address {
		dst := &v4
		if strings.Contains(address, ":") {
			dst = &v6
		}
		if *dst != "" {
			return "", "", fmt.Errorf("target supports one address per family")
		}
		*dst = address
	}
	if target == "loon" {
		// Preserve the pinned builtin syntax for its existing subset. The fixed
		// template renderer has a richer peers grammar; keepalive has no mapping.
		if v.Keepalive > 0 {
			return "", "", fmt.Errorf("Loon WireGuard keepalive unsupported")
		}
		if len(v.Address) != 1 || v6 != "" || v.PresharedKey != "" || len(v.AllowedIPs) > 0 || len(v.DNS) > 0 {
			return modernLoonWireGuardOutput(n, s, name, v4, v6)
		}
		parts := []string{name + " = wireguard", n.Address, strconv.Itoa(n.Port), n.Password, "self-ip=" + v4, "public-key=" + v.PublicKey}
		if v.MTU > 0 {
			parts = append(parts, "mtu="+strconv.Itoa(v.MTU))
		}
		if len(v.Reserved) > 0 {
			values := []string{}
			for _, b := range v.Reserved {
				values = append(values, strconv.Itoa(b))
			}
			parts = append(parts, "client-id="+strings.Join(values, "/"))
		}
		return strings.Join(parts, ", "), "", nil
	}
	sectionName := "WG_" + regexp.MustCompile(`[^a-zA-Z0-9_\p{Han}]`).ReplaceAllString(name, "_")
	// Include a stable numeric suffix via the caller's unique node name; section
	// identifiers must not collide after punctuation normalization.
	sectionName += "_" + digest(name)[:10]
	lines := []string{"[WireGuard " + sectionName + "]", "private-key = " + n.Password}
	if v4 != "" {
		lines = append(lines, "self-ip = "+v4)
	}
	if v6 != "" {
		lines = append(lines, "self-ip-v6 = "+v6)
	}
	if len(v.DNS) > 0 {
		lines = append(lines, "dns-server = "+strings.Join(v.DNS, ", "))
	}
	if v.MTU > 0 {
		lines = append(lines, "mtu = "+strconv.Itoa(v.MTU))
	}
	allowed := v.AllowedIPs
	if len(allowed) == 0 {
		allowed = []string{"0.0.0.0/0", "::/0"}
	}
	peer := []string{"public-key = " + v.PublicKey, `allowed-ips = "` + strings.Join(allowed, ", ") + `"`, "endpoint = " + net.JoinHostPort(n.Address, strconv.Itoa(n.Port))}
	if v.PresharedKey != "" {
		peer = append(peer, "preshared-key = "+v.PresharedKey)
	}
	if v.Keepalive > 0 {
		peer = append(peer, "keepalive = "+strconv.Itoa(v.Keepalive))
	}
	if len(v.Reserved) > 0 {
		values := []string{}
		for _, b := range v.Reserved {
			values = append(values, strconv.Itoa(b))
		}
		peer = append(peer, "client-id = "+strings.Join(values, "/"))
	}
	lines = append(lines, "peer = ("+strings.Join(peer, ", ")+")")
	return name + " = wireguard, section-name = " + sectionName, strings.Join(lines, "\n"), nil
}

func modernLoonWireGuardOutput(n subscription.ParsedLink, s subscription.Settings, name, v4, v6 string) (string, string, error) {
	v := s.WireGuard
	parts := []string{name + " = wireguard"}
	if v4 != "" {
		parts = append(parts, "interface-ip="+v4)
	}
	if v6 != "" {
		parts = append(parts, "interface-ipV6="+v6)
	}
	parts = append(parts, `private-key="`+n.Password+`"`)
	if v.MTU > 0 {
		parts = append(parts, "mtu="+strconv.Itoa(v.MTU))
	}
	seen := map[bool]bool{}
	for _, dns := range v.DNS {
		ipv6 := strings.Contains(dns, ":")
		if seen[ipv6] {
			return "", "", fmt.Errorf("Loon supports one DNS per family")
		}
		seen[ipv6] = true
		key := "dns="
		if ipv6 {
			key = "dnsV6="
		}
		parts = append(parts, key+dns)
	}
	peer := []string{`public-key="` + v.PublicKey + `"`}
	if v.PresharedKey != "" {
		peer = append(peer, `preshared-key="`+v.PresharedKey+`"`)
	}
	if len(v.Reserved) > 0 {
		values := []string{}
		for _, b := range v.Reserved {
			values = append(values, strconv.Itoa(b))
		}
		peer = append(peer, "reserved=["+strings.Join(values, ",")+"]")
	}
	if len(v.AllowedIPs) > 0 {
		peer = append(peer, `allowed-ips="`+strings.Join(v.AllowedIPs, ", ")+`"`)
	}
	peer = append(peer, "endpoint="+net.JoinHostPort(n.Address, strconv.Itoa(n.Port)))
	parts = append(parts, "peers=[{"+strings.Join(peer, ",")+"}]")
	return strings.Join(parts, ","), "", nil
}

func extendedClash(p map[string]any, n subscription.ParsedLink, s subscription.Settings) error {
	if n.Protocol == "snell" {
		v := s.Snell
		if v == nil {
			return fmt.Errorf("missing Snell settings")
		}
		delete(p, "udp")
		p["psk"] = n.Password
		if v.Version > 0 {
			p["version"] = v.Version
		}
		for key, value := range map[string]*bool{"reuse": v.Reuse, "tfo": v.TFO, "udp": v.UDP, "ecn": v.ECN} {
			if value != nil {
				p[key] = *value
			}
		}
		if v.Obfs != "" {
			obfs := map[string]any{"mode": v.Obfs}
			if v.ObfsHost != "" {
				obfs["host"] = v.ObfsHost
			}
			p["obfs-opts"] = obfs
		}
		return nil
	}
	v := s.WireGuard
	if v == nil {
		return fmt.Errorf("missing WireGuard settings")
	}
	p["private-key"], p["public-key"], p["ip"], p["remote-dns-resolve"] = n.Password, v.PublicKey, v.Address, true
	for _, address := range v.Address {
		if strings.Contains(address, ":") {
			p["ipv6"] = address
			break
		}
	}
	if v.PresharedKey != "" {
		p["preshared-key"] = v.PresharedKey
	}
	if len(v.AllowedIPs) > 0 {
		p["allowed-ips"] = v.AllowedIPs
	}
	if len(v.Reserved) > 0 {
		p["reserved"] = v.Reserved
	}
	if v.MTU > 0 {
		p["mtu"] = v.MTU
	}
	if len(v.DNS) > 0 {
		p["dns"] = v.DNS
	}
	if v.Keepalive > 0 {
		p["persistent-keepalive"] = v.Keepalive
	}
	return nil
}
func wireGuardEndpoint(n subscription.ParsedLink, s subscription.Settings, name string) (map[string]any, error) {
	v := s.WireGuard
	if v == nil {
		return nil, fmt.Errorf("missing WireGuard settings")
	}
	// Current sing-box uses endpoints; the fixed upstream's removed legacy
	// outbound shape is deliberately not restored. Per-interface DNS isn't a
	// sing-box endpoint field and cannot be silently converted to global DNS.
	if len(v.DNS) > 0 {
		return nil, fmt.Errorf("WireGuard interface DNS is not supported by sing-box endpoint output")
	}
	allowed := v.AllowedIPs
	if len(allowed) == 0 {
		allowed = []string{"0.0.0.0/0", "::/0"}
	}
	peer := map[string]any{"address": n.Address, "port": n.Port, "public_key": v.PublicKey, "allowed_ips": allowed}
	if v.PresharedKey != "" {
		peer["pre_shared_key"] = v.PresharedKey
	}
	if len(v.Reserved) > 0 {
		peer["reserved"] = v.Reserved
	}
	if v.Keepalive > 0 {
		peer["persistent_keepalive_interval"] = v.Keepalive
	}
	ep := map[string]any{"type": "wireguard", "tag": name, "system": false, "address": v.Address, "private_key": n.Password, "peers": []any{peer}}
	if v.MTU > 0 {
		ep["mtu"] = v.MTU
	}
	return ep, nil
}
func snellINI(n subscription.ParsedLink, s subscription.Settings, name, target string) (string, error) {
	v := s.Snell
	if v == nil || target == "quanx" || v.ECN != nil || v.UDP != nil {
		return "", fmt.Errorf("Snell fields unavailable in selected INI format")
	}
	psk, e := iniValue(n.Password)
	if e != nil {
		return "", e
	}
	parts := []string{name + " = snell", n.Address, strconv.Itoa(n.Port), "psk=" + psk}
	if v.Version > 0 {
		parts = append(parts, "version="+strconv.Itoa(v.Version))
	}
	for _, entry := range []struct {
		key string
		v   *bool
	}{{"reuse", v.Reuse}, {"tfo", v.TFO}} {
		if entry.v != nil {
			parts = append(parts, entry.key+"="+strconv.FormatBool(*entry.v))
		}
	}
	if v.Obfs != "" {
		parts = append(parts, "obfs="+v.Obfs)
	}
	if v.ObfsHost != "" {
		host, e := iniValue(v.ObfsHost)
		if e != nil {
			return "", e
		}
		parts = append(parts, "obfs-host="+host)
	}
	return strings.Join(parts, ", "), nil
}
