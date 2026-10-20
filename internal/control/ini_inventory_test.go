package control

import (
	"encoding/json"
	"testing"

	"github.com/imzyb/MiGate/internal/subscription"
)

func TestINIInventoryOutputRoundtrip(t *testing.T) {
	links := []string{
		"ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#HK",
		"vmess://eyJ2IjoiMiIsInBzIjoiSEsiLCJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiIxMTExMTExMS0xMTExLTQxMTEtYTExMS0xMTExMTExMTExMTEiLCJhaWQiOjAsInNjeSI6ImFlcy0xMjgtZ2NtIiwibmV0Ijoid3MiLCJ0bHMiOiJ0bHMiLCJzbmkiOiJjZG4uZXhhbXBsZS5jb20iLCJwYXRoIjoiL3dzIiwiaG9zdCI6ImNkbi5leGFtcGxlLmNvbSJ9",
		"trojan://secret@example.com:443?sni=example.org#HK",
		"trojan://secret@example.com:443?sni=example.org&type=ws&path=/ws&host=cdn.example.com#HK",
		"anytls://secret@example.com:443?sni=example.org#HK",
		"https://user:secret@example.com:443?sni=example.org#HK",
		"socks5-tls://user:secret@example.com:443?sni=example.org#HK",
		"hysteria2://secret@example.com:443?sni=example.org#HK",
		"vless://11111111-1111-4111-a111-111111111111@example.com:443?security=reality&pbk=testkey&sid=abcd&fp=chrome&flow=xtls-rprx-vision#HK",
	}
	count := 0
	for _, target := range []string{"surge", "loon", "quanx"} {
		for _, link := range links {
			out, e := iniOutput([]string{link}, target)
			if e != nil {
				continue
			}
			imported, e := subscription.DecodeInventory(out)
			if e != nil {
				t.Fatalf("%s roundtrip: %v", target, e)
			}
			before, _ := subscription.ParseLinks([]string{link})
			after, _ := subscription.ParseLinks(imported)
			if len(after.Nodes) != 1 {
				t.Fatal("node missing")
			}
			a, b := before.Nodes[0], after.Nodes[0]
			if a.Protocol != b.Protocol || a.Address != b.Address || a.Port != b.Port || a.Username != b.Username || a.Password != b.Password {
				t.Fatalf("%s credentials changed for %s", target, a.Protocol)
			}
			var x, y subscription.Settings
			json.Unmarshal([]byte(a.SettingsJSON), &x)
			json.Unmarshal([]byte(b.SettingsJSON), &y)
			if x.SNI != y.SNI || x.Host != y.Host || x.Path != y.Path || x.TLS != y.TLS || x.AllowInsecure != y.AllowInsecure || x.Method != y.Method || x.PublicKey != y.PublicKey || x.ShortID != y.ShortID || x.Flow != y.Flow {
				t.Fatalf("%s transport/TLS changed for %s: before=%+v after=%+v", target, a.Protocol, x, y)
			}
			count++
		}
	}
	if count < 19 {
		t.Fatalf("only %d format/protocol roundtrips", count)
	}
}
