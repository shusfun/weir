package subscription

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestQueryDecodedOnceAndPreservesSensitiveValues(t *testing.T) {
	parsed, err := ParseLink("hysteria2://synthetic@example.com:443?obfs=salamander&obfs-password=%20a%2Bb%252Fc%20&sni=example.com")
	if err != nil {
		t.Fatal(err)
	}
	var settings Settings
	if json.Unmarshal([]byte(parsed.SettingsJSON), &settings) != nil || settings.ObfsPassword != " a+b%2Fc " {
		t.Fatal("obfuscation credential was altered")
	}
	parsed, err = ParseLink("vless://synthetic@example.com:443?type=ws&path=%2Fedge%3Ftoken%3Da%2Bb%252Fc%20")
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal([]byte(parsed.SettingsJSON), &settings) != nil || settings.Path != "/edge?token=a+b%2Fc " {
		t.Fatal("transport path was altered")
	}
	payload := `{"add":"example.com","port":443,"id":"synthetic","net":"ws","path":"/a+b%2Fc"}`
	parsed, err = ParseLink("vmess://" + base64.StdEncoding.EncodeToString([]byte(payload)))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal([]byte(parsed.SettingsJSON), &settings) != nil || settings.Path != "/a+b%2Fc" {
		t.Fatal("VMess JSON path was URL-decoded")
	}
}

func TestQueryRejectsAmbiguousAndInvalidParameters(t *testing.T) {
	for _, query := range []string{
		"sni=one&sni=two", "sni=&sni=two", "sni=one&sni=one",
		"sni=one&peer=two", "type=ws&network=grpc", "security=tls&tls=none",
		"allowInsecure=1&insecure=0", "allowInsecure=maybe", "zero_rtt_handshake=2",
		"obfs-password=one&obfs_password=two", "serviceName=a&service_name=b",
	} {
		if _, err := ParseLink("trojan://synthetic@example.com:443?" + query); err == nil {
			t.Fatalf("ambiguous or invalid query accepted: %s", query)
		}
	}
	for _, value := range []string{"false", "0", "no", "off", "true", "1", "yes", "on"} {
		if _, err := ParseLink("trojan://synthetic@example.com:443?allowInsecure=" + value); err != nil {
			t.Fatalf("valid boolean rejected: %s", value)
		}
	}
}
