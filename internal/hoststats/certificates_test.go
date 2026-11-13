package hoststats

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOnlyAppliedCertificatesDetermineHealth(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	for _, sample := range []struct {
		name string
		days int
	}{{"1.crt", 30}, {"2.crt", -1}, {"99.crt", -90}} {
		template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-365 * 24 * time.Hour), NotAfter: now.Add(time.Duration(sample.days) * 24 * time.Hour)}
		raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, sample.name), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), 0600); err != nil {
			t.Fatal(err)
		}
	}
	healthy := ObserveCertificates(dir, []int64{1}, now)
	if healthy.CertDays == nil || *healthy.CertDays < 29 || *healthy.CertID != 1 {
		t.Fatal(healthy)
	}
	expired := ObserveCertificates(dir, []int64{1, 2, 1}, now)
	if expired.CertDays == nil || *expired.CertDays >= 0 || *expired.CertID != 2 {
		t.Fatal(expired)
	}
	if missing := ObserveCertificates(dir, []int64{1, 3}, now); missing.CertDays != nil {
		t.Fatal("missing certificate shown healthy")
	}
	if empty := ObserveCertificates(dir, nil, now); empty.CertDays != nil {
		t.Fatal("unbound file included")
	}
}
