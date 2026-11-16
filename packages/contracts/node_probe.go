package contracts

import (
	"errors"
	"net"
	"strings"
)

// NodeTestClient 保留单个代理端点；XHTTP 使用受约束的原生 Xray 客户端。
type NodeTestClient struct {
	Proxies []map[string]any  `json:"proxies"`
	Target  string            `json:"target"`
	XHTTP   *XHTTPProbeClient `json:"xhttp,omitempty"`
}

// XHTTPProbeClient exposes only a single TLS VLESS endpoint, never arbitrary
// Xray configuration, listeners, routing, files or a direct fallback.
type XHTTPProbeClient struct {
	Server      string   `json:"server"`
	Port        int      `json:"port"`
	UUID        string   `json:"uuid"`
	SNI         string   `json:"sni"`
	Host        string   `json:"host,omitempty"`
	Path        string   `json:"path"`
	Mode        string   `json:"mode"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
}

func (c NodeTestClient) Kernel() string {
	if c.XHTTP != nil {
		return "xray"
	}
	return "mihomo"
}

func (c NodeTestClient) Validate() error {
	if c.XHTTP == nil {
		if len(c.Proxies) != 1 || c.Target == "" || len(c.Target) > 256 {
			return errors.New("probe_client_invalid")
		}
		return nil
	}
	x := c.XHTTP
	host := func(v string) bool {
		return len(v) > 0 && len(v) <= 253 && (net.ParseIP(v) != nil || taskProbeHost.MatchString(v))
	}
	if len(c.Proxies) != 0 || c.Target != "node-test" || !host(x.Server) || !host(x.SNI) || x.Host != "" && !host(x.Host) || x.Port < 1 || x.Port > 65535 || len(x.UUID) != 36 || !strings.HasPrefix(x.Path, "/") || len(x.Path) > 2048 || strings.ContainsAny(x.Path, "\r\n\x00") || len(x.ALPN) > 4 {
		return errors.New("probe_client_invalid")
	}
	for i, b := range x.UUID {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if b != '-' {
				return errors.New("probe_client_invalid")
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", b) {
			return errors.New("probe_client_invalid")
		}
	}
	switch x.Mode {
	case "auto", "packet-up", "stream-up", "stream-one":
	default:
		return errors.New("probe_client_invalid")
	}
	switch x.Fingerprint {
	case "", "chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized":
	default:
		return errors.New("probe_client_invalid")
	}
	for _, a := range x.ALPN {
		if a != "h2" && a != "http/1.1" {
			return errors.New("probe_client_invalid")
		}
	}
	return nil
}

// Loopback 只重定向网络端点，保留证书名称、插件与协议参数。
func (c NodeTestClient) Loopback(port int) error {
	if c.XHTTP != nil {
		if port < 1 || port > 65535 || c.Validate() != nil {
			return errors.New("测试客户端网络端点无效")
		}
		c.XHTTP.Server, c.XHTTP.Port = "127.0.0.1", port
		return nil
	}
	if len(c.Proxies) != 1 || c.Target == "" || port < 1 || port > 65535 {
		return errors.New("测试客户端缺少节点")
	}
	p := c.Proxies[0]
	if p["name"] != c.Target || p["server"] == nil {
		return errors.New("测试客户端网络端点无效")
	}
	p["server"], p["port"] = "127.0.0.1", port
	return nil
}

func (c NodeTestClient) Config(port int) map[string]any {
	if x := c.XHTTP; x != nil {
		return map[string]any{
			"log":       map[string]any{"loglevel": "warning"},
			"inbounds":  []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}},
			"outbounds": []any{map[string]any{"tag": "node-test", "protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": x.Server, "port": x.Port, "users": []any{map[string]any{"id": x.UUID, "encryption": "none"}}}}}, "streamSettings": map[string]any{"network": "xhttp", "security": "tls", "tlsSettings": map[string]any{"serverName": x.SNI, "fingerprint": x.Fingerprint, "alpn": x.ALPN}, "xhttpSettings": map[string]any{"path": x.Path, "mode": x.Mode, "host": x.Host}}}},
		}
	}
	return map[string]any{"mixed-port": port, "bind-address": "127.0.0.1", "allow-lan": false, "mode": "rule", "log-level": "warning", "ipv6": true,
		"proxies": c.Proxies, "rules": []string{"MATCH," + c.Target}}
}
