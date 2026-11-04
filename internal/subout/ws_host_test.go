package subout

import (
	"encoding/json"
	"testing"
)

func TestSingboxWebSocketDoesNotSendEmptyHost(t *testing.T) {
	b, _, err := Render([]string{"vless://11111111-1111-4111-8111-111111111111@127.0.0.1:443?security=tls&type=ws&path=%2Fweir&sni=example.com#ws"}, "singbox")
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err = json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range d.Outbounds {
		if o["type"] != "vless" {
			continue
		}
		found = true
		tr := o["transport"].(map[string]any)
		if tr["headers"] != nil {
			t.Fatal("empty Host header would cause an HTTP 400")
		}
		if tr["path"] != "/weir" {
			t.Fatal("WebSocket path lost")
		}
	}
	if !found {
		t.Fatal("VLESS client missing")
	}
}
