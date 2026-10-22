// Package subout 把分享链接列表渲染成各客户端的订阅格式（Clash / mihomo、sing-box、Surge / Loon / QuanX、Egern、Base64）。
// 输入只有链接文本：中心生成的自有节点与 MiSub 引擎处理后的外部节点走同一条输出路径。
package subout

import (
	"encoding/json"
	"fmt"

	"github.com/imzyb/MiGate/internal/subscription"
	"gopkg.in/yaml.v3"
)

func clashOutput(links []string) ([]byte, error) {
	nodes, err := subscription.ParseLinks(links)
	if err != nil {
		return nil, err
	}
	proxies := []map[string]any{}
	if len(nodes.Skipped) > 0 {
		return nil, fmt.Errorf("unrecognized nodes")
	}
	names := []string{}
	usedNames := map[string]bool{"PROXY": true, "DIRECT": true, "REJECT": true}
	for i, n := range nodes.Nodes {
		var settings subscription.Settings
		json.Unmarshal([]byte(n.SettingsJSON), &settings)
		if !supportedSubscriptionSettings(n.Protocol, settings) {
			return nil, fmt.Errorf("unsupported transport")
		}
		name := n.Remark
		if name == "" {
			name = fmt.Sprintf("Node-%d", i+1)
		}
		name = uniqueNodeName(name, usedNames)
		p := map[string]any{"name": name, "type": n.Protocol, "server": n.Address, "port": n.Port, "udp": true}
		if n.Protocol == "snell" || n.Protocol == "wireguard" {
			if err := extendedClash(p, n, settings); err != nil {
				return nil, err
			}
			proxies = append(proxies, p)
			names = append(names, name)
			continue
		}
		switch n.Protocol {
		case "vless", "vmess":
			p["uuid"] = n.Username
			if n.Protocol == "vmess" {
				p["alterId"] = 0
				p["cipher"] = settings.Method
			}
		case "trojan", "anytls":
			p["password"] = n.Password
		case "http", "socks5":
			p["username"] = n.Username
			p["password"] = n.Password
		case "hysteria2", "tuic":
			p["password"] = n.Password
			if n.Protocol == "tuic" {
				p["uuid"] = n.Username
				if settings.CongestionControl != "" {
					p["congestion-controller"] = settings.CongestionControl
				}
				p["reduce-rtt"] = settings.ZeroRTT
			}
			if settings.Obfs != "" {
				p["obfs"] = settings.Obfs
				p["obfs-password"] = settings.ObfsPassword
			}
			if settings.CertificatePin != "" {
				p["fingerprint"] = settings.CertificatePin
			}
		case "ss", "shadowsocks":
			p["type"] = "ss"
			p["cipher"] = settings.Method
			p["password"] = n.Password
		default:
			return nil, fmt.Errorf("unsupported protocol %s", n.Protocol)
		}
		if settings.TLS || settings.Security == "tls" || settings.Reality {
			p["tls"] = true
		}
		if settings.SNI != "" {
			p["servername"] = settings.SNI
		}
		if settings.Network != "" && settings.Network != "tcp" && n.Protocol != "hysteria2" && n.Protocol != "tuic" {
			p["network"] = settings.Network
		}
		if settings.Flow != "" {
			p["flow"] = settings.Flow
		}
		if settings.Reality {
			p["reality-opts"] = map[string]any{"public-key": settings.PublicKey, "short-id": settings.ShortID}
			p["client-fingerprint"] = settings.Fingerprint
		}
		if settings.Network == "ws" {
			p["ws-opts"] = map[string]any{"path": settings.Path, "headers": map[string]any{"Host": settings.Host}}
		}
		proxies = append(proxies, p)
		if settings.Network == "grpc" {
			p["grpc-opts"] = map[string]any{"grpc-service-name": settings.ServiceName}
		}
		if len(settings.ALPN) > 0 {
			p["alpn"] = settings.ALPN
		}
		if settings.AllowInsecure {
			p["skip-cert-verify"] = true
		}
		if settings.Fingerprint != "" {
			p["client-fingerprint"] = settings.Fingerprint
		}
		names = append(names, name)
	}
	return yaml.Marshal(map[string]any{"mixed-port": 7890, "mode": "rule", "proxies": proxies, "proxy-groups": []any{map[string]any{"name": "PROXY", "type": "select", "proxies": names}}, "rules": []string{"MATCH,PROXY"}})
}
func singboxOutput(links []string) ([]byte, error) {
	nodes, err := subscription.ParseLinks(links)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	endpoints := []map[string]any{}
	if len(nodes.Skipped) > 0 {
		return nil, fmt.Errorf("unrecognized nodes")
	}
	names := []string{}
	usedNames := map[string]bool{"proxy": true}
	for i, n := range nodes.Nodes {
		var settings subscription.Settings
		json.Unmarshal([]byte(n.SettingsJSON), &settings)
		if !supportedSubscriptionSettings(n.Protocol, settings) {
			return nil, fmt.Errorf("unsupported transport")
		}
		name := n.Remark
		if name == "" {
			name = fmt.Sprintf("Node-%d", i+1)
		}
		name = uniqueNodeName(name, usedNames)
		p := map[string]any{"tag": name, "type": n.Protocol, "server": n.Address, "server_port": n.Port}
		if n.Protocol == "wireguard" {
			ep, e := wireGuardEndpoint(n, settings, name)
			if e != nil {
				return nil, e
			}
			endpoints = append(endpoints, ep)
			names = append(names, name)
			continue
		}
		switch n.Protocol {
		case "vless", "vmess":
			p["uuid"] = n.Username
			if n.Protocol == "vmess" {
				p["security"] = settings.Method
			}
		case "trojan", "anytls":
			p["password"] = n.Password
		case "http", "socks5":
			if n.Protocol == "socks5" {
				if settings.TLS || settings.Security == "tls" {
					return nil, fmt.Errorf("sing-box SOCKS TLS outbound unsupported")
				}
				p["type"] = "socks"
				p["version"] = "5"
			}
			p["username"] = n.Username
			p["password"] = n.Password
		case "hysteria2", "tuic":
			p["password"] = n.Password
			if n.Protocol == "tuic" {
				p["uuid"] = n.Username
				if settings.CongestionControl != "" {
					p["congestion_control"] = settings.CongestionControl
				}
				p["zero_rtt_handshake"] = settings.ZeroRTT
			}
			if settings.Obfs != "" {
				p["obfs"] = map[string]any{"type": settings.Obfs, "password": settings.ObfsPassword}
			}
		case "ss", "shadowsocks":
			p["type"] = "shadowsocks"
			p["method"] = settings.Method
			p["password"] = n.Password
		default:
			return nil, fmt.Errorf("unsupported protocol")
		}
		if settings.Flow != "" {
			p["flow"] = settings.Flow
		}
		if settings.TLS || settings.Reality || settings.Security == "tls" {
			tls := map[string]any{"enabled": true, "server_name": settings.SNI}
			if len(settings.ALPN) > 0 {
				tls["alpn"] = settings.ALPN
			}
			if settings.AllowInsecure {
				tls["insecure"] = true
			}
			if settings.SPKI != "" {
				tls["certificate_public_key_sha256"] = []string{settings.SPKI}
			} else if settings.CertificatePin != "" {
				return nil, fmt.Errorf("certificate pin conversion requires SPKI")
			}
			if settings.Reality {
				tls["reality"] = map[string]any{"enabled": true, "public_key": settings.PublicKey, "short_id": settings.ShortID}
				tls["utls"] = map[string]any{"enabled": true, "fingerprint": settings.Fingerprint}
			} else if settings.Fingerprint != "" {
				tls["utls"] = map[string]any{"enabled": true, "fingerprint": settings.Fingerprint}
			}
			p["tls"] = tls
		}
		if settings.Network == "ws" {
			p["transport"] = map[string]any{"type": "ws", "path": settings.Path, "headers": map[string]string{"Host": settings.Host}}
		}
		out = append(out, p)
		if settings.Network == "grpc" {
			p["transport"] = map[string]any{"type": "grpc", "service_name": settings.ServiceName}
		}
		names = append(names, name)
	}
	out = append([]map[string]any{{"type": "selector", "tag": "proxy", "outbounds": names}}, out...)
	cfg := map[string]any{"outbounds": out, "route": map[string]any{"final": "proxy"}}
	if len(endpoints) > 0 {
		cfg["endpoints"] = endpoints
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func supportedSubscriptionSettings(protocol string, s subscription.Settings) bool {
	if protocol == "snell" {
		return s.Snell != nil && s.WireGuard == nil && len(s.UnsupportedFields) == 0
	}
	if protocol == "wireguard" {
		return s.WireGuard != nil && s.Snell == nil && len(s.UnsupportedFields) == 0
	}
	if s.Snell != nil || s.WireGuard != nil {
		return false
	}
	if s.VmessUnsupported || len(s.UnsupportedFields) > 0 {
		return false
	}
	if s.Security != "" && s.Security != "none" && s.Security != "tls" && s.Security != "reality" {
		return false
	}
	if s.Host != "" && s.Network != "ws" || s.Path != "" && s.Network != "ws" || s.ServiceName != "" && s.Network != "grpc" || s.Flow != "" && protocol != "vless" || !s.Reality && (s.PublicKey != "" || s.ShortID != "") {
		return false
	}
	tls := s.TLS || s.Reality || s.Security == "tls"
	if s.Flow != "" && (s.Flow != "xtls-rprx-vision" || !tls || s.Network != "" && s.Network != "tcp") {
		return false
	}
	if !tls && (s.SNI != "" || len(s.ALPN) > 0 || s.AllowInsecure || s.Fingerprint != "") {
		return false
	}
	if protocol == "shadowsocks" && (tls || s.Network != "" && s.Network != "tcp") {
		return false
	}
	if protocol == "http" || protocol == "socks5" || protocol == "anytls" {
		if s.Reality || s.Network != "" && s.Network != "tcp" {
			return false
		}
	}
	if protocol == "hysteria2" || protocol == "tuic" {
		if s.Reality || s.Security != "" && s.Security != "tls" || s.Host != "" || s.Path != "" || s.Flow != "" || s.PublicKey != "" || s.ShortID != "" || s.ServiceName != "" || s.Fingerprint != "" {
			return false
		}
		if protocol == "hysteria2" && (s.CongestionControl != "" || s.ZeroRTT || s.Obfs != "" && s.Obfs != "salamander" || s.ObfsPassword != "" && s.Obfs == "" || s.Obfs == "salamander" && s.ObfsPassword == "") {
			return false
		}
		if protocol == "tuic" && (s.Obfs != "" || s.ObfsPassword != "" || s.CongestionControl != "" && s.CongestionControl != "cubic" && s.CongestionControl != "bbr" && s.CongestionControl != "new_reno") {
			return false
		}
		return s.Network == "" || s.Network == "udp" || s.Network == "quic"
	}
	if s.Obfs != "" || s.ObfsPassword != "" || s.CongestionControl != "" || s.ZeroRTT || s.SPKI != "" || s.CertificatePin != "" {
		return false
	}
	return s.Network == "" || s.Network == "tcp" || s.Network == "ws" || s.Network == "grpc"
}

func uniqueNodeName(name string, used map[string]bool) string {
	original := name
	for suffix := 2; used[name]; suffix++ {
		name = fmt.Sprintf("%s (%d)", original, suffix)
	}
	used[name] = true
	return name
}
