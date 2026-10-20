package control

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/grant"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/ent/user"
	"github.com/imzyb/MiGate/internal/subscription"
	pipeline "github.com/imzyb/MiGate/services/subscriptions"
	"gopkg.in/yaml.v3"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (s *Server) mySubscription(c *gin.Context) {
	u := c.MustGet("account").(*ent.User)
	links, err := s.userLinks(c, u)
	if err != nil {
		failure(c, 500, "subscription_unavailable")
		return
	}
	base := safeURL(s.Config.PublicURL, "/sub/"+u.SubscriptionToken)
	c.JSON(200, gin.H{"url": base, "formats": []string{"base64", "clash", "singbox", "surge", "loon", "quanx", "egern"}, "node_count": len(links), "quota": u.Quota, "used": u.Used, "expires": u.Expires})
}
func (s *Server) serveSubscription(c *gin.Context) {
	if !s.Config.Installed {
		failure(c, 503, "setup_required")
		return
	}
	u, err := s.Store.User.Query().Where(user.SubscriptionTokenEQ(c.Param("token"))).Only(c.Request.Context())
	if err != nil || !u.Enabled || u.Expires > 0 && u.Expires <= time.Now().Unix() || u.Quota <= u.Used {
		failure(c, 403, "subscription_inactive")
		return
	}
	links, err := s.userLinks(c, u)
	if err != nil {
		failure(c, 503, "subscription_unavailable")
		return
	}
	c.Header("Subscription-Userinfo", fmt.Sprintf("upload=0; download=%d; total=%d; expire=%d", u.Used, u.Quota, u.Expires))
	target := c.DefaultQuery("target", "base64")
	template, e := s.userOutputTemplate(c.Request.Context(), u.ID, target)
	if e != nil {
		failure(c, 422, "output_template_invalid")
		return
	}
	if template != nil {
		body, e := renderOutputTemplate(links, *template)
		if e != nil {
			failure(c, 422, "format_conversion_failed")
			return
		}
		contentType := "application/json"
		if template.Target == "clash" {
			contentType = "application/yaml; charset=utf-8"
		}
		c.Data(200, contentType, body)
		return
	}
	switch target {
	case "egern":
		body, e := egernOutput(links)
		if e != nil {
			failure(c, 422, "format_conversion_failed")
			return
		}
		c.Data(200, "application/yaml; charset=utf-8", body)
	case "surge", "loon", "quanx":
		body, e := iniOutput(links, target)
		if e != nil {
			failure(c, 422, "format_conversion_failed")
			return
		}
		c.Data(200, "text/plain; charset=utf-8", body)
	case "base64":
		c.Data(200, "text/plain; charset=utf-8", []byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))))
	case "clash", "mihomo":
		body, e := clashOutput(links)
		if e != nil {
			failure(c, 422, "format_conversion_failed")
			return
		}
		c.Data(200, "application/yaml; charset=utf-8", body)
	case "singbox":
		body, e := singboxOutput(links)
		if e != nil {
			failure(c, 422, "format_conversion_failed")
			return
		}
		c.Data(200, "application/json", body)
	default:
		failure(c, 400, "unsupported_target")
	}
}
func (s *Server) userLinks(c *gin.Context, u *ent.User) ([]string, error) {
	if !u.Enabled || u.Expires > 0 && u.Expires <= time.Now().Unix() || u.Used >= u.Quota {
		return []string{}, nil
	}
	items, err := s.Store.Grant.Query().Where(grant.UserIDEQ(u.ID), grant.EnabledEQ(true), grant.StatusEQ("applied")).All(c.Request.Context())
	if err != nil {
		return nil, err
	}
	links := []string{}
	for _, g := range items {
		dvc, e := s.Store.Device.Get(c.Request.Context(), g.DeviceID)
		if e != nil {
			return nil, e
		}
		if dvc.Status != "approved" {
			continue
		}
		r, e := s.Store.Resource.Query().Where(resource.KindEQ("device-inbound"), resource.KeyEQ(fmt.Sprintf("%d:%d", g.DeviceID, g.InboundID))).Only(c.Request.Context())
		if e != nil {
			continue
		}
		d := r.Data
		if enabled, exists := d["enabled"].(bool); exists && !enabled {
			continue
		}
		host := text(d["public_host"])
		if host == "" {
			continue
		}
		protocol := text(d["protocol"])
		port := int(number(d["port"]))
		uuid := text(g.Credential["uuid"])
		password := text(g.Credential["password"])
		name := text(d["remark"])
		if name == "" {
			name = fmt.Sprintf("Gate-%d-%d", g.DeviceID, g.InboundID)
		}
		q := url.Values{}
		security := text(d["security"])
		network := text(d["network"])
		if network == "" {
			network = "tcp"
		}
		q.Set("type", network)
		if security != "" && security != "none" {
			q.Set("security", security)
		}
		for field, key := range map[string]string{"tls_sni": "sni", "tls_alpn": "alpn", "ws_host": "host", "reality_public_key": "pbk", "reality_short_id": "sid", "tls_fingerprint": "fp", "ws_path": "path", "grpc_service_name": "serviceName"} {
			if v := text(d[field]); v != "" {
				q.Set(key, v)
			}
		}
		if security == "reality" {
			q.Set("flow", "xtls-rprx-vision")
		}
		node := url.URL{Scheme: protocol, Host: net.JoinHostPort(host, strconv.Itoa(port)), RawQuery: q.Encode(), Fragment: name}
		if protocol == "trojan" {
			node.User = url.User(password)
		} else {
			node.User = url.User(uuid)
		}
		if protocol == "hysteria2" || protocol == "tuic" {
			q.Del("type")
			q.Set("security", "tls")
			if protocol == "hysteria2" {
				node.User = url.User(password)
			} else {
				node.User = url.UserPassword(uuid, password)
			}
			for field, key := range map[string]string{"hy2_obfs": "obfs", "hy2_obfs_password": "obfs-password", "tuic_congestion_control": "congestion_control"} {
				if value := text(d[field]); value != "" {
					q.Set(key, value)
				}
			}
			if d["tuic_zero_rtt"] == true {
				q.Set("zero_rtt_handshake", "1")
			}
			if certificateID := int(number(d["certificate_id"])); certificateID > 0 {
				certificate, e := s.Store.Resource.Query().Where(resource.KindEQ("device-certificate"), resource.KeyEQ(fmt.Sprintf("%d:%d", g.DeviceID, certificateID))).Only(c.Request.Context())
				if e != nil {
					return nil, fmt.Errorf("certificate metadata unavailable")
				}
				block, _ := pem.Decode([]byte(text(certificate.Data["certificate_pem"])))
				if block == nil {
					return nil, fmt.Errorf("certificate metadata invalid")
				}
				leaf, e := x509.ParseCertificate(block.Bytes)
				if e != nil {
					return nil, e
				}
				if time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
					continue
				}
				certPin, spkiPin := sha256.Sum256(leaf.Raw), sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
				q.Set("pinSHA256", fmt.Sprintf("%x", certPin))
				q.Set("spki", base64.StdEncoding.EncodeToString(spkiPin[:]))
			}
			node.RawQuery = q.Encode()
		}
		if protocol == "vmess" {
			v := map[string]any{"v": "2", "ps": name, "add": host, "port": port, "id": uuid, "aid": 0, "net": network, "tls": security, "sni": text(d["tls_sni"]), "path": text(d["ws_path"]), "host": text(d["ws_host"])}
			if network == "grpc" {
				v["path"] = text(d["grpc_service_name"])
			}
			if alpn := text(d["tls_alpn"]); alpn != "" {
				v["alpn"] = alpn
			}
			if fp := text(d["tls_fingerprint"]); fp != "" {
				v["fp"] = fp
			}
			b, _ := json.Marshal(v)
			links = append(links, "vmess://"+base64.StdEncoding.EncodeToString(b))
		} else {
			links = append(links, node.String())
		}
	}
	// Assigned profiles transform only already-authorized Gate links.
	profiles, e := s.Store.Resource.Query().Where(resource.KindEQ("profiles")).Order(ent.Asc("id")).All(c.Request.Context())
	if e != nil {
		return nil, e
	}
	for _, profile := range profiles {
		assigned := false
		if ids, ok := profile.Data["user_ids"].([]any); ok {
			for _, id := range ids {
				assigned = assigned || int(number(id)) == u.ID
			}
		}
		if !assigned {
			continue
		}
		if enabled, ok := profile.Data["enabled"].(bool); ok && !enabled {
			continue
		}
		operators, e := s.profileOperators(c.Request.Context(), profile.Data)
		if e != nil {
			return nil, e
		}
		links, e = pipeline.Run(links, operators, c.DefaultQuery("target", "base64"))
		if e != nil {
			return nil, e
		}
	}
	return links, nil
}
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
