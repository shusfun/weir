//go:build e2e

package e2e

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func TestBrowserSessionIssuedFromGateReceipt(t *testing.T) {
	e := setup(t)
	e.seedSingbox()
	e.startGate("browser-receipt")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	certPEM, keyPEM := browserReceiptCertificate(t)
	cert := e.post("/api/v1/admin/certs", map[string]any{"name": "browser receipt", "domain": "127.0.0.1", "cert_pem": certPEM, "key_pem": keyPEM}, map[string]string{"traceparent": "00-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-cccccccccccccccc-01"})
	gatePath := "/api/v1/admin/gates/" + strconv.Itoa(e.gateID)
	e.putJSON(gatePath+"/browser-test", map[string]any{"enabled": true, "cert_id": cert["id"], "host": "127.0.0.1"})
	e.applied("browser test service")
	var publicKey ed25519.PublicKey
	eventually(t, 20*time.Second, "gate browser receipt", func() bool {
		body, _ := e.get(gatePath+"/browser-test", nil)
		var view map[string]any
		if json.Unmarshal(body, &view) != nil {
			return false
		}
		got, _ := view["observation"].(map[string]any)
		boot, _ := got["boot"].(string)
		config, _ := view["config"].(map[string]any)
		raw, _ := config["public_key"].(string)
		key, err := base64.RawURLEncoding.DecodeString(raw)
		if got["healthy"] != true || !contracts.BrowserTestIDValid(boot) || err != nil || len(key) != ed25519.PublicKeySize {
			return false
		}
		publicKey = key
		return true
	})
	created := e.post(gatePath+"/browser-tests", map[string]any{"purpose": "https"}, map[string]string{"traceparent": "00-dddddddddddddddddddddddddddddddd-eeeeeeeeeeeeeeee-01"})
	id, _ := created["id"].(string)
	token, _ := created["token"].(string)
	if created["status"] != "pending" || !contracts.BrowserTestIDValid(id) || token == "" {
		t.Fatalf("session was not issued from the receipt: %v", created)
	}
	if _, err := contracts.VerifyBrowserTest(token, publicKey, time.Now()); err != nil {
		t.Fatal(err)
	}
	e.applied("browser session token")
	eventually(t, 20*time.Second, "gate observed issued session", func() bool {
		body, _ := e.get("/api/v1/browser-tests/"+id, nil)
		var view map[string]any
		if json.Unmarshal(body, &view) != nil {
			return false
		}
		return view["status"] == "ready" && view["gate_observed_bytes"] == float64(0)
	})
}

func (e *env) putJSON(path string, body any) {
	e.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		e.t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, e.url+path, bytes.NewReader(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.admin.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 {
		e.t.Fatalf("%s = %d %v", path, resp.StatusCode, out)
	}
}

func browserReceiptCertificate(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}
