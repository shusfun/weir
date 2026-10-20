package subscription

import "testing"

func TestInventoryIdentityPreservesTransport(t *testing.T) {
	a := "vless://test@example.com:443?security=tls&sni=a.example&type=ws&path=%2Fone#first"
	b := "vless://test@example.com:443?path=%2Fone&type=ws&sni=a.example&security=tls#renamed"
	if InventoryIdentity(a) != InventoryIdentity(b) {
		t.Fatal("display name/query order changed identity")
	}
	for _, other := range []string{
		"vless://test@example.com:443?security=tls&sni=b.example&type=ws&path=%2Fone",
		"vless://test@example.com:443?security=tls&sni=a.example&type=ws&path=%2Ftwo",
		"vless://other@example.com:443?security=tls&sni=a.example&type=ws&path=%2Fone",
		"vless://test@example.com:443?security=tls&sni=a.example&type=ws&path=%2Fone&allowInsecure=1",
	} {
		if InventoryIdentity(a) == InventoryIdentity(other) {
			t.Fatal("connection options lost")
		}
	}
}
