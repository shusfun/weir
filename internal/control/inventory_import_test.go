package control

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/imzyb/MiGate/internal/subscription"
	"gopkg.in/yaml.v3"
)

func TestStructuredInventoryRoundTrips(t *testing.T) {
	links := []string{
		"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#HK",
		"trojan://pa%3Ass@example.com:443?sni=example.org&type=ws&host=cdn.example.com&path=/ws#HK",
		"vless://uuid@example.com:443?security=reality&pbk=key&sid=abcd&fp=chrome&flow=xtls-rprx-vision#HK",
		"vless://uuid@example.com:443?security=tls&type=grpc&serviceName=svc&sni=example.org&alpn=h2#HK",
		"anytls://pa%3Ass@example.com:443?sni=example.org#HK",
		"https://us%40er:pa%3Ass@example.com:443?sni=example.org#HK",
		"socks5://u:p@example.com:1080#HK",
		"hysteria2://secret@example.com:443?obfs=salamander&obfs-password=hidden&sni=example.org#HK",
		"tuic://uuid:secret@example.com:443?congestion_control=bbr&zero_rtt_handshake=1&alpn=h3&sni=example.org#HK",
	}
	for _, format := range []string{"clash", "singbox"} {
		convert := clashOutput
		if format == "singbox" {
			convert = singboxOutput
		}
		for _, link := range links {
			first, err := convert([]string{link})
			if err != nil {
				t.Fatal(err)
			}
			imported, err := subscription.DecodeInventory(first)
			if err != nil {
				t.Fatalf("%s import failed: %v", format, err)
			}
			second, err := convert(imported)
			if err != nil {
				t.Fatalf("%s roundtrip rejected: %v", format, err)
			}
			var before, after any
			if format == "clash" {
				yaml.Unmarshal(first, &before)
				yaml.Unmarshal(second, &after)
			} else {
				json.Unmarshal(first, &before)
				json.Unmarshal(second, &after)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("%s structured roundtrip changed fields", format)
			}
		}
	}
}
