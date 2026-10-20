package control

import (
	"encoding/json"
	"fmt"

	"github.com/imzyb/MiGate/internal/subscription"
	"gopkg.in/yaml.v3"
)

// Field mapping follows MiSub 4952c16 render-egern.js (MIT). No unknown
// protocol fallback: emitting an unauthenticated proxy would lose credentials.
func egernOutput(links []string) ([]byte, error) {
	parsed, err := subscription.ParseLinks(links)
	if err != nil || len(parsed.Skipped) > 0 {
		return nil, fmt.Errorf("invalid Egern nodes")
	}
	proxies := []any{}
	names := []string{}
	used := map[string]bool{"PROXY": true, "DIRECT": true, "REJECT": true}
	for i, n := range parsed.Nodes {
		var s subscription.Settings
		if err = json.Unmarshal([]byte(n.SettingsJSON), &s); err != nil {
			return nil, err
		}
		if !supportedSubscriptionSettings(n.Protocol, s) || s.SPKI != "" || s.CertificatePin != "" || s.Obfs != "" || s.ObfsPassword != "" || s.ZeroRTT || s.Fingerprint != "" {
			return nil, fmt.Errorf("unsupported Egern fields")
		}
		if s.Flow != "" && n.Protocol != "vless" || s.Reality && n.Protocol != "vless" || s.Reality && s.Network != "" && s.Network != "tcp" {
			return nil, fmt.Errorf("unsupported Egern Reality")
		}
		if s.CongestionControl != "" && n.Protocol != "tuic" {
			return nil, fmt.Errorf("unsupported Egern congestion")
		}
		if len(s.ALPN) > 0 && n.Protocol != "tuic" {
			return nil, fmt.Errorf("unsupported Egern ALPN")
		}
		name := n.Remark
		if name == "" {
			name = fmt.Sprintf("Node-%d", i+1)
		}
		name = uniqueNodeName(name, used)
		names = append(names, name)
		p := map[string]any{"name": name, "server": n.Address, "port": n.Port}
		kind := n.Protocol
		if kind == "trojan" && !s.TLS {
			return nil, fmt.Errorf("Egern Trojan requires TLS")
		}
		switch kind {
		case "trojan":
			p["password"] = n.Password
			p["skip_tls_verify"] = s.AllowInsecure
			if s.SNI != "" {
				p["sni"] = s.SNI
			}
		case "vless":
			p["user_id"] = n.Username
			if s.Flow != "" {
				p["flow"] = s.Flow
			}
		case "vmess":
			p["user_id"] = n.Username
			p["security"] = s.Method
		case "shadowsocks":
			p["method"] = s.Method
			p["password"] = n.Password
		case "hysteria2":
			p["auth"] = n.Password
			p["skip_tls_verify"] = s.AllowInsecure
			if s.SNI != "" {
				p["sni"] = s.SNI
			}
		case "tuic":
			p["uuid"] = n.Username
			p["password"] = n.Password
			p["version"] = 5
			cc := s.CongestionControl
			if cc == "" {
				cc = "cubic"
			}
			p["congestion_control"] = cc
			p["skip_tls_verify"] = s.AllowInsecure
			if s.SNI != "" {
				p["sni"] = s.SNI
			}
			if len(s.ALPN) > 0 {
				p["alpn"] = s.ALPN
			}
		case "anytls":
			if s.AllowInsecure {
				return nil, fmt.Errorf("Egern AnyTLS insecure option not mapped")
			}
			p["password"] = n.Password
			p["udp_relay"] = true
			if s.SNI != "" {
				p["sni"] = s.SNI
			}
		default:
			return nil, fmt.Errorf("unsupported Egern protocol")
		}
		if kind == "vless" || kind == "vmess" || kind == "trojan" {
			var tls map[string]any
			if s.TLS || s.Reality {
				tls = map[string]any{"skip_tls_verify": s.AllowInsecure, "sni": s.SNI}
			}
			transport := map[string]any{}
			switch s.Network {
			case "ws":
				path := s.Path
				if path == "" {
					path = "/"
				}
				if kind == "trojan" {
					ws := map[string]any{"enabled": true, "path": path}
					if s.Host != "" {
						ws["host"] = s.Host
					}
					p["websocket"] = ws
				} else {
					headers := map[string]string{}
					if s.Host != "" {
						headers["Host"] = s.Host
					}
					ws := map[string]any{"path": path, "headers": headers}
					key := "ws"
					if tls != nil {
						key = "wss"
						for k, v := range tls {
							ws[k] = v
						}
					}
					transport[key] = ws
				}
			case "grpc":
				service := s.ServiceName
				if service == "" {
					service = "grpc"
				}
				transport["grpc"] = map[string]any{"service_name": service}
				if tls != nil {
					transport["tls"] = tls
				}
			default:
				if tls != nil {
					transport["tls"] = tls
				}
			}
			if s.Reality && tls != nil {
				tls["reality"] = map[string]any{"public_key": s.PublicKey, "short_id": s.ShortID}
			}
			if len(transport) > 0 {
				p["transport"] = transport
			}
		}
		proxies = append(proxies, map[string]any{kind: p})
	}
	return yaml.Marshal(map[string]any{"general": map[string]any{"dns_server": []string{"1.1.1.1"}, "ipv6": false}, "proxies": proxies, "policy_groups": []any{map[string]any{"select": map[string]any{"name": "PROXY", "policies": names}}}, "rules": []any{map[string]any{"default": map[string]any{"policy": "PROXY"}}}})
}
