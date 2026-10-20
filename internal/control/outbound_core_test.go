package control

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/internal/singbox"
	"github.com/imzyb/MiGate/internal/xray"
)

func TestManagedOutboundCoreCompatibility(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	trusted, _ := json.Marshal(map[string]any{"security": "tls", "sni": "example.com", "trusted_certificate": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))})
	for _, tc := range []struct{ protocol, settings string }{
		{"vmess", `{"network":"tcp","security":"none"}`},
		{"vmess", `{"network":"ws","security":"tls","sni":"example.com","host":"cdn.example.com","path":"/fixture"}`},
		{"vmess", `{"network":"grpc","security":"tls","service_name":"fixture","sni":"example.com"}`},
		{"vless", `{"network":"tcp","security":"reality","sni":"example.com","pbk":"` + base64.RawURLEncoding.EncodeToString(make([]byte, 32)) + `","sid":"aabb","fp":"chrome","flow":"xtls-rprx-vision"}`},
		{"trojan", `{"network":"ws","security":"tls","path":"/fixture","sni":"example.com"}`},
		{"https", `{"security":"tls","sni":"example.com"}`},
		{"https", string(trusted)},
	} {
		out := db.Outbound{ID: 1, Tag: "fixture", Protocol: tc.protocol, Address: "example.com", Port: 443, Username: "12345678-1234-1234-1234-123456789abc", Password: "synthetic", SettingsJSON: tc.settings, Enabled: true}
		if _, err := db.ManagedOutboundSettings(out); err != nil {
			t.Fatal(err)
		}
		x, err := xray.BuildConfigWithOutbounds(nil, []db.Outbound{out}, nil)
		if err != nil {
			t.Fatal(err)
		}
		s, err := singbox.BuildConfigWithOutbounds(nil, []db.Outbound{out}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, engine := range []struct {
			name, env string
			cfg       any
			args      []string
		}{
			{"xray", "MIGATE_XRAY_BINARY", x, []string{"run", "-test", "-config"}},
			{"singbox", "MIGATE_SINGBOX_BINARY", s, []string{"check", "-c"}},
		} {
			binary := os.Getenv(engine.env)
			if binary == "" {
				continue
			}
			file := filepath.Join(t.TempDir(), "config.json")
			body, err := json.Marshal(engine.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(file, body, 0600); err != nil {
				t.Fatal(err)
			}
			args := append(append([]string{}, engine.args...), file)
			if output, checkErr := exec.Command(binary, args...).CombinedOutput(); checkErr != nil {
				t.Fatalf("real %s rejected synthetic managed %s transport: %v: %s", engine.name, tc.protocol, checkErr, output)
			}
		}
	}
}
