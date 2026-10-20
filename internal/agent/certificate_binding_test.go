package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/packages/contracts"
)

func TestCertificateBindingAtomicStateAndGuards(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(3), DNSNames: []string{"gate.invalid"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	private, _ := x509.MarshalPKCS8PrivateKey(key)
	cert := ManagedCertificate{ID: 7, Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), Key: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))}
	fixture := func() *Agent {
		return &Agent{State: State{Certificates: []ManagedCertificate{cert}, Inbounds: []db.Inbound{{ID: 1, Protocol: "vless", Security: "tls", CertificateID: 2, TLSSNI: "gate.invalid", Port: 24446, Enabled: true}, {ID: 2, Protocol: "tuic", CertificateID: 2, TLSSNI: "gate.invalid", Port: 24447, Enabled: false}}, Versions: map[string]int64{}}}
	}
	payload := func(second int64, previous int64) map[string]any {
		return map[string]any{"certificate_id": 7, "inbounds": []map[string]any{{"id": 1, "previous_certificate_id": 2}, {"id": second, "previous_certificate_id": previous}}}
	}
	for _, mode := range []string{"missing", "stale-binding", "wrong-host", "not-tls", "newer-revision", "unknown-field", "duplicate"} {
		a := fixture()
		p := payload(2, 2)
		switch mode {
		case "missing":
			p = payload(99, 2)
		case "stale-binding":
			p = payload(2, 3)
		case "wrong-host":
			a.State.Inbounds[1].TLSSNI = "other.invalid"
		case "not-tls":
			a.State.Inbounds[1].Protocol = "vless"
			a.State.Inbounds[1].Security = "reality"
		case "newer-revision":
			a.State.Versions["inbound:2"] = 20
		case "unknown-field":
			p["command"] = "restart"
		case "duplicate":
			p = payload(1, 2)
		}
		before, _ := json.Marshal(a.State)
		if a.bindCertificate(contracts.Task{Revision: 10, Payload: p}) == nil {
			t.Fatal("invalid binding accepted", mode)
		}
		after, _ := json.Marshal(a.State)
		if string(before) != string(after) {
			t.Fatal("partially mutated failed batch", mode)
		}
	}
	a := fixture()
	if err := a.bindCertificate(contracts.Task{Revision: 10, Payload: payload(2, 2)}); err != nil {
		t.Fatal(err)
	}
	if a.State.Inbounds[0].CertificateID != 7 || a.State.Inbounds[1].CertificateID != 7 || !a.State.Inbounds[0].Enabled || a.State.Inbounds[1].Enabled || a.State.Inbounds[0].Port != 24446 || a.State.Versions["inbound:2"] != 10 {
		t.Fatal("binding changed unrelated fields")
	}
}
