package control

import (
	"encoding/json"
	"testing"

	"github.com/imzyb/MiGate/internal/subscription"
)

func TestQUICConversionRejectsLostFields(t *testing.T) {
	for _, raw := range []string{
		"tuic://id:secret@example.com:443?obfs=salamander&obfs-password=x",
		"tuic://id:secret@example.com:443?congestion_control=unknown",
		"hysteria2://secret@example.com:443?zero_rtt_handshake=1",
		"hysteria2://secret@example.com:443?obfs=salamander",
		"hysteria2://secret@example.com:443?type=tcp",
		"hysteria2://secret@example.com:443/unsupported",
		"hysteria2://secret@example.com:443?custom=value",
	} {
		if _, err := singboxOutput([]string{raw}); err == nil {
			t.Fatalf("silently converted unsupported fields: %s", raw)
		}
		if _, err := clashOutput([]string{raw}); err == nil {
			t.Fatalf("silently converted unsupported fields: %s", raw)
		}
	}
}

func TestQUICParsingCredentialsTLSAndNames(t *testing.T) {
	for _, raw := range []string{"tuic://id@example.com:443", "hysteria2://example.com:443"} {
		if _, err := subscription.ParseLink(raw); err == nil {
			t.Fatal("missing credentials accepted")
		}
	}
	n, err := subscription.ParseLink("hy2://secret@example.com:443?insecure=1#A+B%2520")
	if err != nil {
		t.Fatal(err)
	}
	var s subscription.Settings
	json.Unmarshal([]byte(n.SettingsJSON), &s)
	if !s.TLS || s.SNI != "example.com" || !s.AllowInsecure || n.Remark != "A+B%20" {
		t.Fatal("URI semantics lost")
	}
}
