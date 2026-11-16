package center

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/user"
	"github.com/shusfun/weir/packages/contracts"
)

func TestBrowserSessionsBootCancellationAndOwnership(t *testing.T) {
	s, admin, fg := setupGate(t)
	ctx := t.Context()
	gid := int(fg.id)
	s.Store.Ent.Gate.UpdateOneID(gid).SetLastSeenAt(time.Now()).ExecX(ctx)
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	sealed, err := s.seal(base64.RawURLEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	config := browserConfig{Plan: contracts.BrowserTestPlan{GateID: gid, Port: 23001, RTCPort: 23002, CertID: 1, Host: "gate.example.test", Origin: "https://center.example.test", PublicKey: base64.RawURLEncoding.EncodeToString(pub)}, Key: sealed}
	s.Store.Ent.Resource.Create().SetKind(browserConfigKind).SetKey(itoa(gid)).SetOwnerID(gid).SetData(toMap(config)).ExecX(ctx)
	observation := contracts.BrowserTestObservation{Healthy: true, Boot: strings.Repeat("a", 32), ObservedAt: time.Now()}
	if err = s.Store.Transient.Set(ctx, "browser-test:"+itoa(gid), jsonText(observation), time.Minute); err != nil {
		t.Fatal(err)
	}
	created := mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+itoa(gid)+"/browser-tests", map[string]any{"purpose": "https"}, 201)
	id := created["id"].(string)
	token := created["token"].(string)
	if _, err = contracts.VerifyBrowserTest(token, pub, time.Now()); err != nil {
		t.Fatal(err)
	}
	row := s.Store.Ent.Resource.Query().Where(resource.KindEQ(browserSessionKind), resource.KeyEQ(id)).OnlyX(ctx)
	if strings.Contains(row.String(), token) {
		t.Fatal("plaintext session token stored")
	}
	path := "/api/v1/browser-tests/" + id
	pending := mustJSON(t, admin, "GET", path, nil, 200)
	if pending["status"] != "pending" {
		t.Fatal(pending)
	}
	observation.Sessions = []contracts.BrowserTestUsage{{ID: id, Bytes: 64}}
	if err = s.Store.Transient.Set(ctx, "browser-test:"+itoa(gid), jsonText(observation), time.Minute); err != nil {
		t.Fatal(err)
	}
	ready := mustJSON(t, admin, "GET", path, nil, 200)
	if ready["status"] != "ready" {
		t.Fatal(ready)
	}
	owner := s.Store.Ent.User.Query().Where(user.RoleEQ(user.RoleAdmin)).FirstX(ctx)
	if err = browserSessionAccess(ctx, s.Store.Ent, owner.ID+100, browserSession{Claim: contracts.BrowserTestClaim{UserID: owner.ID}}); err == nil {
		t.Fatal("cross-user access")
	}
	if w := admin.do("POST", path, map[string]any{"status": "passed", "bytes": contracts.BrowserTestMaxBytes + 1}, nil); w.Code != 400 || !strings.Contains(w.Body.String(), "browser_test_result_invalid") {
		t.Fatal("over-limit measurement accepted", w.Code, w.Body)
	}
	node := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "ss2022", "skip_probe": true}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "browser-user@example.test", "password": "Passw0rd-browser", "all_nodes": false}, 200)
	member := login(t, s, "browser-user@example.test", "Passw0rd-browser")
	if w := member.do("POST", "/api/v1/me/nodes/"+itoa(int(node["id"].(float64)))+"/browser-tests", map[string]any{"purpose": "https"}, nil); w.Code != 403 {
		t.Fatal("unassigned node issued a browser session", w.Code, w.Body)
	}
	if w := member.do("DELETE", path, nil, nil); w.Code != 403 {
		t.Fatal("foreign session revoke accepted", w.Code, w.Body)
	}
	mustJSON(t, admin, "DELETE", path, nil, 200)
	mustJSON(t, admin, "POST", path, map[string]any{"status": "passed", "reason": "late", "bytes": 64}, 409)
	mustJSON(t, admin, "DELETE", path, nil, 200)
	// Only the configured certificate may authenticate the QUIC endpoint.
	certPEM, keyPEM, err := selfSigned("gate.example.test")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	leaf.NotAfter = time.Now().Add(24 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, leaf.PublicKey, pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	uploaded := mustJSON(t, admin, "POST", "/api/v1/admin/certs", map[string]any{"name": "short test certificate", "domain": "gate.example.test", "cert_pem": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), "key_pem": keyPEM}, 200)
	config.Plan.CertID = int64(uploaded["id"].(float64))
	s.Store.Ent.Resource.Update().Where(resource.KindEQ(browserConfigKind), resource.KeyEQ(itoa(gid))).SetData(toMap(config)).ExecX(ctx)
	quic := mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+itoa(gid)+"/browser-tests", map[string]any{"purpose": "webtransport"}, 201)
	if quic["certificate_hash"] != certFingerprint(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))) {
		t.Fatal("session hash does not match configured leaf")
	}
	if _, exists := created["certificate_hash"]; exists {
		t.Fatal("HTTPS must use normal certificate validation")
	}
	mustJSON(t, admin, "DELETE", "/api/v1/browser-tests/"+quic["id"].(string), nil, 200)
	created = mustJSON(t, admin, "POST", "/api/v1/admin/gates/"+itoa(gid)+"/browser-tests", map[string]any{"purpose": "webrtc"}, 201)
	observation.Boot = strings.Repeat("b", 32)
	if err = s.Store.Transient.Set(ctx, "browser-test:"+itoa(gid), jsonText(observation), time.Minute); err != nil {
		t.Fatal(err)
	}
	expired := mustJSON(t, admin, "GET", "/api/v1/browser-tests/"+created["id"].(string), nil, 200)
	if expired["status"] != "expired" {
		t.Fatal("restart did not revoke session", expired)
	}
}
