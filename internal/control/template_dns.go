package control

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// Resolver addresses are emitted to the user's client; the center never fetches
// them. Credentials, fragments, queries and unknown schemes are unsupported.
func parseTemplateDNS(raw string) (*url.URL, error) {
	if _, err := netip.ParseAddr(raw); err == nil {
		return &url.URL{Scheme: "udp", Host: raw}, nil
	}
	if len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\t ") {
		return nil, fmt.Errorf("invalid DNS address")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, fmt.Errorf("invalid DNS address")
	}
	if u.Scheme != "https" && u.Scheme != "tls" {
		return nil, fmt.Errorf("unsupported DNS transport")
	}
	if u.Scheme == "tls" && u.Path != "" || u.Scheme == "https" && (u.Path == "" || u.RawPath != "") {
		return nil, fmt.Errorf("invalid DNS path")
	}
	if strings.Contains(u.Hostname(), "%") || strings.ContainsAny(u.Hostname(), "#/?\\") {
		return nil, fmt.Errorf("invalid DNS host")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid DNS port")
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("invalid DNS port")
	}
	if _, err := netip.ParseAddr(u.Hostname()); err != nil {
		for _, label := range strings.Split(u.Hostname(), ".") {
			if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return nil, fmt.Errorf("invalid DNS hostname")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
					return nil, fmt.Errorf("invalid DNS hostname")
				}
			}
		}
	}
	return u, nil
}

func validateTemplateDNS(addresses, bootstrap []string) error {
	if len(bootstrap) > 8 || len(bootstrap) > 0 && len(addresses) == 0 {
		return fmt.Errorf("invalid DNS bootstrap")
	}
	for _, address := range bootstrap {
		if _, err := netip.ParseAddr(address); err != nil {
			return fmt.Errorf("DNS bootstrap requires IP")
		}
	}
	for _, address := range addresses {
		u, err := parseTemplateDNS(address)
		if err != nil {
			return err
		}
		if _, err := netip.ParseAddr(u.Hostname()); err != nil && u.Scheme != "udp" && len(bootstrap) == 0 {
			return fmt.Errorf("DNS hostname requires bootstrap IP")
		}
	}
	return nil
}

func templateDNSServer(address, tag string, bootstrap bool) map[string]any {
	u, _ := parseTemplateDNS(address)
	host := u.Hostname()
	if u.Scheme == "udp" {
		host = address
	}
	kind := u.Scheme
	server := map[string]any{"tag": tag, "type": kind, "server": host}
	if u.Port() != "" {
		port, _ := strconv.Atoi(u.Port())
		server["server_port"] = port
	}
	if kind == "https" {
		server["path"] = u.Path
	}
	if kind != "udp" {
		server["tls"] = map[string]any{"enabled": true, "server_name": host}
		if _, err := netip.ParseAddr(host); err != nil && bootstrap {
			server["domain_resolver"] = "dns-bootstrap-0"
		}
	}
	return server
}
