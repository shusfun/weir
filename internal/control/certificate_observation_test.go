package control

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestCertificateObservationValidityAndRedaction(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	for _, test := range []struct {
		before, after    time.Time
		status, severity string
	}{
		{now.Add(-time.Hour), now.Add(30 * 24 * time.Hour), "resolved", "info"},
		{now.Add(-time.Hour), now.Add(2 * 24 * time.Hour), "observed", "warning"},
		{now.Add(-time.Hour), now, "observed", "critical"},
		{now.Add(time.Hour), now.Add(2 * time.Hour), "observed", "critical"},
	} {
		cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: test.before, NotAfter: test.after, DNSNames: []string{"secret-host.invalid"}}
		der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		data := certificateObservation(map[string]any{"certificate_pem": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), "key_pem": "must-never-leak"}, now)
		if data["status"] != test.status || data["severity"] != test.severity {
			t.Fatal("validity state wrong", data)
		}
		if _, exists := data["certificate_pem"]; exists {
			t.Fatal("certificate leaked into observation")
		}
		if _, exists := data["key_pem"]; exists {
			t.Fatal("private key leaked")
		}
	}
	if data := certificateObservation(map[string]any{"certificate_pem": "invalid"}, now); data["status"] != "unknown" {
		t.Fatal("invalid metadata shown healthy")
	}
}
