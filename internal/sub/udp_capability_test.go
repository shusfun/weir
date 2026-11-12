package sub

import (
	"testing"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

func TestUDPDefaultsPreserveProtocolCapabilities(t *testing.T) {
	cases := []struct {
		link string
		want any
	}{
		{"http://user:password@proxy.example.com:8080#HTTP", nil},
		{"vless://6f1c2a8e-3b4d-4c5e-9f60-7a8b9c0d1e2f@proxy.example.com:443?security=tls#VLESS", nil},
		{"hysteria2://password@proxy.example.com:443#HY2", true},
		{"socks5://user:password@proxy.example.com:1080#SOCKS", false},
	}
	for _, tc := range cases {
		t.Run(tc.link, func(t *testing.T) {
			got := URLsToClashProxies([]string{tc.link}, js.Obj("enableUdp", false, "addFlagEmoji", false))
			if len(got) != 1 || got[0].Get("udp") != tc.want {
				t.Fatalf("udp capability: %#v", got)
			}
		})
	}
}
