package db

import (
	"encoding/base64"
	"testing"
)

func TestManagedOutboundTransportRejectsDegradation(t *testing.T) {
	for _, tc := range []struct{ protocol, settings string }{
		{"vless", `{"network":"xhttp"}`},
		{"vless", `{"security":"tls","extra_secret":"must-not-ignore"}`},
		{"vless", `{} {}`}, {"vless", `{} broken`}, {"vless", `null`},
		{"trojan", `{"security":"none"}`}, {"https", `{"security":"none"}`},
		{"https", `{"security":"tls","allow_insecure":true}`},
		{"https", `{"security":"tls","trusted_certificate":"not a certificate"}`},
		{"socks", `{"security":"tls"}`}, {"http", `{"network":"ws"}`},
		{"shadowsocks", `{"network":"ws"}`},
		{"vless", `{"network":"ws","flow":"xtls-rprx-vision","security":"tls"}`},
		{"vless", `{"security":"none","sni":"example.com"}`},
		{"vless", `{"security":"tls","host":"cdn.example.com"}`},
		{"vless", `{"security":"tls","service_name":"grpc"}`},
		{"vless", `{"security":"tls","sni":"bad\r\nhost"}`},
		{"vless", `{"security":"reality","pbk":"invalid","sni":"example.com"}`},
		{"vmess", `{"flow":"xtls-rprx-vision","security":"tls"}`},
		{"hysteria2", `{"spki":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`},
		{"tuic", `{"spki":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`},
		{"anytls", `{"spki":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`},
	} {
		if _, err := ManagedOutboundSettings(Outbound{Protocol: tc.protocol, SettingsJSON: tc.settings}); err == nil {
			t.Fatalf("ignored/invalid transport accepted: %s %s", tc.protocol, tc.settings)
		}
	}
	for _, protocol := range []string{"https", "trojan"} {
		s, err := ManagedOutboundSettings(Outbound{Protocol: protocol})
		if err != nil || s.Security != "tls" || !s.TLS || (protocol == "https" && (len(s.ALPN) != 1 || s.ALPN[0] != "http/1.1")) {
			t.Fatal("required default TLS lost", protocol, err)
		}
	}
	for _, protocol := range []string{"vless", "vmess", "trojan"} {
		s, err := ManagedOutboundSettings(Outbound{Protocol: protocol, SettingsJSON: `{"network":"ws","security":"tls","sni":"example.com","host":"cdn.example.com","path":"/p+a%2B","alpn":["http/1.1"]}`})
		if err != nil || s.Path != "/p+a%2B" || s.Host != "cdn.example.com" {
			t.Fatal("valid WS transport lost", protocol, err)
		}
	}
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	s, err := ManagedOutboundSettings(Outbound{Protocol: "vless", SettingsJSON: `{"network":"tcp","security":"reality","sni":"example.com","pbk":"` + key + `","sid":"aabb","flow":"xtls-rprx-vision"}`})
	if err != nil || !s.Reality || s.Flow != "xtls-rprx-vision" {
		t.Fatal("valid Reality rejected", err)
	}
}
