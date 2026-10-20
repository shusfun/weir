package control

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCertificateReceiptNameAndSecretAllowlist(t *testing.T) {
	view := certificateResourceData(map[string]any{"id": 7, "name": "Gate 测试证书", "certificate_pem": "public-cert", "sealed_key": "encrypted-private", "key_pem": "raw-private", "unexpected": "hidden"})
	b, _ := json.Marshal(view)
	if len(view) != 3 || view["name"] != "Gate 测试证书" || strings.Contains(string(b), "private") || strings.Contains(string(b), "hidden") {
		t.Fatal("certificate receipt lost name or exposed private fields")
	}
	if certificateResourceData(map[string]any{"id": 8})["name"] != "" {
		t.Fatal("legacy receipt name default incompatible")
	}
}
