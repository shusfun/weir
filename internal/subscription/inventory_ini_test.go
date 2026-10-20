package subscription

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestINIInventoryQuotedCredentialsAndTransport(t *testing.T) {
	cases := []struct{ body, kind, password, name string }{
		{"# exported config\n; inventory only\n[Proxy]\nHTTP = http, example.com, 8080, username=u, password=p", "http", "p", "HTTP"},
		{`n = trojan, example.com, 443, password="https://synthetic.invalid/?x=1"`, "trojan", "https://synthetic.invalid/?x=1", "n"},
		{`香港 = ss, example.com, 8388, encrypt-method=aes-128-gcm, password="a,b=\"x\"", udp-relay=true`, "shadowsocks", `a,b="x"`, "香港"},
		{`[Proxy]
香港 = Shadowsocks, example.com, 8388, aes-128-gcm, "a,b=+%25"
[Rule]
FINAL, DIRECT`, "shadowsocks", "a,b=+%25", "香港"},
		{`[server_local]
trojan=[2001:db8::1]:443, password=p=+%25, tls-host=edge.example, tls-verification=false, tag=香港
[policy]
static=PROXY, 香港`, "trojan", "p=+%25", "香港"},
		{`Trojan = trojan, example.com, 443, password=p, ws=true, ws-path="/a,b=1", ws-headers=Host:cdn.example, sni=tls.example`, "trojan", "p", "Trojan"},
		{`http=example.com:443, username=u, password=p, over-tls=true, tag=HTTP`, "http", "p", "HTTP"},
	}
	for _, tc := range cases {
		links, e := DecodeInventory([]byte(tc.body))
		if e != nil {
			t.Fatalf("%s: %v", tc.name, e)
		}
		parsed, e := ParseLinks(links)
		if e != nil || len(parsed.Nodes) != 1 {
			t.Fatal("missing imported node")
		}
		n := parsed.Nodes[0]
		if n.Protocol != tc.kind || n.Password != tc.password || n.Remark != tc.name {
			t.Fatal("connection or name lost")
		}
		if tc.name == "Trojan" {
			var s Settings
			json.Unmarshal([]byte(n.SettingsJSON), &s)
			if s.Network != "ws" || s.Path != "/a,b=1" || s.Host != "cdn.example" || s.SNI != "tls.example" {
				t.Fatal("WS or TLS fields lost")
			}
		}
	}
}

func TestINIInventoryRejectsLossAndAmbiguity(t *testing.T) {
	for _, body := range []string{
		`n = ss, example.com, 8388, encrypt-method=aes-128-gcm, password=p, unknown=SECRET`,
		`n = ss, example.com, 8388, encrypt-method=aes-128-gcm, password=p, password=q`,
		`n = ss, example.com, 8388, encrypt-method=aes-128-gcm, password=p, udp-relay=false`,
		`n = vmess, example.com, 443, username=id, vmess-aead=false`,
		`n = vmess, example.com, 443, username=id, alterId=1`,
		`n = trojan, example.com, 443, password=p, ws-path=/discarded`,
		`n = trojan, example.com, 443, password=p, ws=true, transport=tcp`,
		`n = trojan, example.com, 443, password=p, skip-cert-verify=maybe`,
		`n = trojan, example.com, 443, password="SECRET\nvalue"`,
		`n = snell, example.com, 443, psk=SECRET, unknown=SECRET`,
		"[server_remote]\nhttps://example.com/SECRET\n[server_local]\ntrojan=example.com:443,password=p,tag=n",
		`n = trojan, example.com, 443, password="SECRET`,
	} {
		if _, e := DecodeInventory([]byte(body)); e == nil {
			t.Fatal("unsupported INI accepted")
		} else if strings.Contains(e.Error(), "SECRET") {
			t.Fatal("secret exposed in parser error")
		}
	}
}
