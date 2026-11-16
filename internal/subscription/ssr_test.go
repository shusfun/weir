package subscription

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestSSRPreservesParametersAndIPv6(t *testing.T) {
	enc := func(v string) string { return base64.RawURLEncoding.EncodeToString([]byte(v)) }
	makeLink := func(query string) string {
		return "ssr://" + enc("[2001:db8::1]:443:auth_aes128_sha1:aes-256-cfb:tls1.2_ticket_auth:"+enc("password")+"/?"+query)
	}
	n, err := ParseLink(makeLink("remarks=" + enc("名称") + "&protoparam=" + enc("1:secret") + "&obfsparam=" + enc("www.example.test")))
	if err != nil {
		t.Fatal(err)
	}
	var s Settings
	if err = json.Unmarshal([]byte(n.SettingsJSON), &s); err != nil {
		t.Fatal(err)
	}
	if n.Address != "2001:db8::1" || n.Password != "password" || n.Remark != "名称" || s.SSR == nil || s.SSR.ProtocolParam != "1:secret" || s.SSR.ObfsParam != "www.example.test" {
		t.Fatalf("lost SSR parameters: %+v %+v", n, s)
	}
	for _, raw := range []string{makeLink("unknown=" + enc("value")), makeLink("remarks=&remarks="), "ssr://" + enc("example.test:70000:origin:aes-128-cfb:plain:"+enc("pw")), "ssr://" + strings.Repeat("a", 20000)} {
		if _, err := ParseLink(raw); err == nil {
			t.Fatal("accepted invalid SSR input")
		}
	}
}
