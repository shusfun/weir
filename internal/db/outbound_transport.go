package db

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"strings"
	"unicode"
)

// Managed outbound forms use only transport fields understood by both core
// builders. Reject unknown/ignored fields instead of silently degrading TLS.
func ManagedOutboundSettings(out Outbound) (OutboundSettings, error) {
	var s OutboundSettings
	// Preserve legacy basic sing-box-only outbounds. Their advanced transport
	// fields remain unsupported until their core mapping is migrated.
	if (out.Protocol == "hysteria2" || out.Protocol == "tuic" || out.Protocol == "shadowtls") && (strings.TrimSpace(out.SettingsJSON) == "" || strings.TrimSpace(out.SettingsJSON) == "{}") {
		return s, nil
	}
	if len(out.SettingsJSON) > 16384 {
		return s, errors.New("outbound transport too large")
	}
	if strings.TrimSpace(out.SettingsJSON) != "" {
		if !strings.HasPrefix(strings.TrimSpace(out.SettingsJSON), "{") {
			return s, errors.New("invalid outbound transport object")
		}
		decoder := json.NewDecoder(strings.NewReader(out.SettingsJSON))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&s); err != nil {
			return s, errors.New("invalid outbound transport fields")
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return s, errors.New("multiple outbound transport values")
		}
	}
	if s.AllowInsecure {
		return s, errors.New("skip verification is unsupported by managed Xray; import a trusted certificate")
	}
	if s.TrustedCertificate != "" {
		if len(s.TrustedCertificate) > 8192 {
			return s, errors.New("trusted certificate too large")
		}
		block, rest := pem.Decode([]byte(s.TrustedCertificate))
		if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
			return s, errors.New("expected one public PEM certificate")
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return s, errors.New("invalid trusted certificate")
		}
	}
	protocol := NormalizeOutboundProtocol(out.Protocol)
	if protocol == "hysteria2" || protocol == "tuic" || protocol == "anytls" || protocol == "shadowtls" {
		return managedSingboxSettings(protocol, s)
	}
	if s.Obfs != "" || s.ObfsPassword != "" || s.CongestionControl != "" || s.ZeroRTT || s.SPKI != "" || s.ShadowTLSVersion != 0 {
		return s, errors.New("fields require sing-box-specific protocol")
	}
	switch protocol {
	case "freedom", "blackhole", "socks", "http", "https", "vless", "vmess", "trojan", "shadowsocks":
	default:
		return s, errors.New("unsupported managed outbound protocol")
	}
	network := strings.ToLower(strings.TrimSpace(s.Network))
	if network == "" {
		network = "tcp"
	}
	if network == "websocket" {
		network = "ws"
	}
	if network != "tcp" && network != "ws" && network != "grpc" {
		return s, errors.New("unsupported outbound transport")
	}
	security := strings.ToLower(strings.TrimSpace(s.Security))
	if security == "" {
		if s.Reality {
			security = "reality"
		} else if s.TLS || protocol == "https" || protocol == "trojan" {
			security = "tls"
		} else {
			security = "none"
		}
	}
	if security != "none" && security != "tls" && security != "reality" {
		return s, errors.New("unsupported outbound security")
	}
	if s.Reality && security != "reality" || s.TLS && security == "none" {
		return s, errors.New("conflicting outbound security")
	}
	if (protocol == "https" || protocol == "trojan") && security != "tls" {
		return s, errors.New("outbound protocol requires TLS")
	}
	for _, value := range []string{s.SNI, s.Host, s.Path, s.ServiceName, s.Fingerprint, s.Flow, s.PublicKey, s.ShortID, s.Method} {
		if len(value) > 2048 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return s, errors.New("invalid outbound transport value")
		}
	}
	if len(s.ALPN) > 8 {
		return s, errors.New("invalid outbound ALPN")
	}
	for _, value := range s.ALPN {
		if len(value) == 0 || len(value) > 64 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return s, errors.New("invalid outbound ALPN")
		}
	}
	if (protocol == "freedom" || protocol == "blackhole") && (security != "none" || network != "tcp" || s.Method != "") {
		return s, errors.New("builtin outbound cannot use transport")
	}
	if (protocol == "socks" || protocol == "http" || protocol == "https") && network != "tcp" {
		return s, errors.New("proxy outbound requires TCP")
	}
	if protocol == "socks" && security != "none" {
		return s, errors.New("SOCKS TLS not supported by both managed cores")
	}
	if protocol == "shadowsocks" && (security != "none" || network != "tcp") {
		return s, errors.New("Shadowsocks extended transport requires a separate adapter")
	}
	if s.TrustedCertificate != "" && security != "tls" {
		return s, errors.New("trusted certificate requires TLS")
	}
	if security == "reality" {
		if s.AllowInsecure || len(s.ALPN) > 0 {
			return s, errors.New("TLS-only fields on Reality")
		}
		key, err := base64.RawURLEncoding.DecodeString(s.PublicKey)
		short, e := hex.DecodeString(s.ShortID)
		if protocol != "vless" || network != "tcp" || err != nil || len(key) != 32 || e != nil || len(short) > 8 || s.SNI == "" {
			return s, errors.New("invalid outbound Reality configuration")
		}
	} else if s.PublicKey != "" || s.ShortID != "" {
		return s, errors.New("Reality fields without Reality")
	}
	if s.Flow != "" && (s.Flow != "xtls-rprx-vision" || protocol != "vless" || network != "tcp" || security == "none") {
		return s, errors.New("unsupported outbound flow")
	}
	if network != "ws" && (s.Path != "" || s.Host != "") {
		return s, errors.New("WebSocket fields without WebSocket")
	}
	if network != "grpc" && s.ServiceName != "" {
		return s, errors.New("gRPC service without gRPC")
	}
	if security == "none" && (s.SNI != "" || s.Fingerprint != "" || len(s.ALPN) > 0 || s.AllowInsecure) {
		return s, errors.New("TLS fields without TLS")
	}
	if protocol != "shadowsocks" && protocol != "vmess" && s.Method != "" {
		return s, errors.New("cipher field on incompatible outbound")
	}
	if protocol == "vmess" && s.Method != "" && s.Method != "auto" && s.Method != "none" && s.Method != "aes-128-gcm" && s.Method != "chacha20-poly1305" && s.Method != "zero" {
		return s, errors.New("unsupported VMess cipher")
	}
	if (protocol == "http" || protocol == "https") && security == "tls" && len(s.ALPN) == 0 {
		s.ALPN = []string{"http/1.1"}
	}
	s.Network = network
	s.Security = security
	s.TLS = security == "tls"
	s.Reality = security == "reality"
	return s, nil
}

func managedSingboxSettings(protocol string, s OutboundSettings) (OutboundSettings, error) {
	if s.Security != "" && s.Security != "tls" || s.Reality || s.PublicKey != "" || s.ShortID != "" || s.Flow != "" || s.Host != "" || s.Path != "" || s.ServiceName != "" || s.Method != "" {
		return s, errors.New("unsupported sing-box outbound fields")
	}
	if s.Network != "" && s.Network != "tcp" && ((protocol != "hysteria2" && protocol != "tuic") || (s.Network != "udp" && s.Network != "quic")) {
		return s, errors.New("unsupported sing-box outbound transport")
	}
	for _, value := range []string{s.SNI, s.Fingerprint, s.ObfsPassword} {
		if len(value) > 2048 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return s, errors.New("invalid outbound TLS value")
		}
	}
	if len(s.ALPN) > 8 {
		return s, errors.New("invalid outbound ALPN")
	}
	for _, value := range s.ALPN {
		if len(value) == 0 || len(value) > 64 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return s, errors.New("invalid outbound ALPN")
		}
	}
	if s.SPKI != "" {
		// sing-box 1.14.3 switches off chain / hostname / expiry verification
		// for certificate_public_key_sha256. That cannot satisfy the managed
		// TLS contract; use an independently verified public certificate.
		return s, errors.New("SPKI-only verification cannot preserve managed TLS verification")
	}
	if protocol != "hysteria2" && (s.Obfs != "" || s.ObfsPassword != "") || protocol == "hysteria2" && (s.Obfs != "" && s.Obfs != "salamander" || s.Obfs == "salamander" && s.ObfsPassword == "" || s.Obfs == "" && s.ObfsPassword != "") {
		return s, errors.New("invalid Hysteria2 obfuscation")
	}
	if protocol != "tuic" && (s.CongestionControl != "" || s.ZeroRTT) || protocol == "tuic" && s.CongestionControl != "" && s.CongestionControl != "cubic" && s.CongestionControl != "bbr" && s.CongestionControl != "new_reno" {
		return s, errors.New("invalid TUIC transport")
	}
	if (protocol == "hysteria2" || protocol == "tuic" || protocol == "shadowtls") && s.Fingerprint != "" {
		return s, errors.New("uTLS unsupported for this protocol")
	}
	if protocol == "shadowtls" {
		if s.ShadowTLSVersion == 0 {
			s.ShadowTLSVersion = 3
		}
		if s.ShadowTLSVersion != 3 {
			return s, errors.New("managed ShadowTLS requires version 3")
		}
	} else if s.ShadowTLSVersion != 0 {
		return s, errors.New("ShadowTLS fields on incompatible protocol")
	}
	s.Security = "tls"
	s.TLS = true
	s.Network = ""
	return s, nil
}
