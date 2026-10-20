package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"github.com/imzyb/MiGate/internal/db"
	"math/big"
	"testing"
	"time"
)

func TestManagedCertificateRejectsInvalidMaterial(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	makeCert := func(before, after time.Time) ManagedCertificate {
		template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"gate.invalid"}, NotBefore: before, NotAfter: after, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		private, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return ManagedCertificate{ID: 1, Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), Key: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))}
	}
	now := time.Now()
	valid := makeCert(now.Add(-time.Hour), now.Add(time.Hour))
	if _, err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []ManagedCertificate{makeCert(now.Add(-2*time.Hour), now.Add(-time.Hour)), makeCert(now.Add(time.Hour), now.Add(2*time.Hour)), {ID: 1, Certificate: valid.Certificate, Key: "wrong key"}, {ID: 0, Certificate: valid.Certificate, Key: valid.Key}} {
		if _, err := bad.Validate(); err == nil {
			t.Fatal("invalid certificate accepted")
		}
	}
	expired := makeCert(now.Add(-2*time.Hour), now.Add(-time.Hour))
	if _, err := expired.parse(); err != nil {
		t.Fatal("retained expired certificate prevents structural validation", err)
	}
	a := &Agent{stats: meterFixture{}, singStats: meterFixture{}, State: State{Certificates: []ManagedCertificate{expired}, Inbounds: []db.Inbound{{ID: 1, Core: db.CoreSingbox, CertificateID: 1, Enabled: true}, {ID: 2, Core: db.CoreXray, Enabled: true}}, Grants: []LocalGrant{{GrantID: 1, InboundID: 1, Enabled: true, DesiredEnabled: true, Allocated: 1000}, {GrantID: 2, InboundID: 2, Enabled: true, DesiredEnabled: true, Allocated: 1000}}}}
	if !a.collectLocked(context.Background()) || a.State.Grants[0].Enabled || !a.State.Grants[1].Enabled {
		t.Fatal("expired certificate did not disable only its own inbound")
	}
	a.State.Certificates = []ManagedCertificate{valid}
	if !a.collectLocked(context.Background()) || !a.State.Grants[0].Enabled {
		t.Fatal("renewed certificate did not restore eligible authorization")
	}
}
