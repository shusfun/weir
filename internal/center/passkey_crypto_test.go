package center

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/shusfun/weir/internal/store/ent/user"
)

// This authenticator signs real WebAuthn data. Transport is the HTTP test
// server; browser mediation and hardware prompts are tested separately.
type testAuthenticator struct {
	private *ecdsa.PrivateKey
	id      []byte
}

func newTestAuthenticator(t *testing.T) testAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	if _, err = rand.Read(id); err != nil {
		t.Fatal(err)
	}
	return testAuthenticator{key, id}
}

func b64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }
func challengeOf(v map[string]any) string {
	return v["options"].(map[string]any)["publicKey"].(map[string]any)["challenge"].(string)
}
func authClientData(kind, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": "https://example.com", "crossOrigin": false})
	return b
}
func authData(flags byte, counter uint32) []byte {
	rp := sha256.Sum256([]byte("example.com"))
	b := append([]byte{}, rp[:]...)
	b = append(b, flags)
	b = binary.BigEndian.AppendUint32(b, counter)
	return b
}

func (a testAuthenticator) enrollment(t *testing.T, challenge string) map[string]any {
	t.Helper()
	data := authData(0x45, 0)
	data = append(data, make([]byte, 16)...)
	data = binary.BigEndian.AppendUint16(data, uint16(len(a.id)))
	data = append(data, a.id...)
	key, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: a.private.X.FillBytes(make([]byte, 32)), -3: a.private.Y.FillBytes(make([]byte, 32))})
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, key...)
	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": data})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": b64(a.id), "rawId": b64(a.id), "type": "public-key", "response": map[string]any{"clientDataJSON": b64(authClientData("webauthn.create", challenge)), "attestationObject": b64(attestation), "transports": []string{"internal"}}}
}

func (a testAuthenticator) assertion(t *testing.T, challenge string, uid int, counter uint32) map[string]any {
	t.Helper()
	data := authData(0x05, counter)
	client := authClientData("webauthn.get", challenge)
	h := sha256.Sum256(client)
	message := append(append([]byte{}, data...), h[:]...)
	signed := sha256.Sum256(message)
	signature, err := ecdsa.SignASN1(rand.Reader, a.private, signed[:])
	if err != nil {
		t.Fatal(err)
	}
	handle := make([]byte, 8)
	binary.BigEndian.PutUint64(handle, uint64(uid))
	return map[string]any{"id": b64(a.id), "rawId": b64(a.id), "type": "public-key", "response": map[string]any{"authenticatorData": b64(data), "clientDataJSON": b64(client), "signature": b64(signature), "userHandle": b64(handle)}}
}

func TestDiscoverablePasskeySignedLoginAndWrongUserHandle(t *testing.T) {
	s, admin := newInstalledServer(t)
	u := s.Store.Ent.User.Query().Where(user.RoleEQ(user.RoleAdmin)).OnlyX(t.Context())
	a := newTestAuthenticator(t)
	begin := mustJSON(t, admin, "POST", "/api/v1/security/passkey/begin", map[string]any{"password": "Passw0rd"}, 200)
	w := admin.do("POST", "/api/v1/security/passkey/finish", a.enrollment(t, challengeOf(begin)), map[string]string{"X-Passkey-Token": begin["token"].(string)})
	if w.Code != 201 {
		t.Fatal("signed enrollment", w.Code, w.Body)
	}
	anon := &testClient{t: t, router: s.Router()}
	for attempt, uid := range []int{u.ID + 1, u.ID} {
		v := mustJSON(t, anon, "POST", "/api/v1/auth/passkey/begin", map[string]any{}, 200)
		assertion := a.assertion(t, challengeOf(v), uid, uint32(attempt+1))
		w := anon.do("POST", "/api/v1/auth/passkey/finish", assertion, map[string]string{"X-Passkey-Token": v["token"].(string), "traceparent": "00-11111111111111111111111111111111-2222222222222222-01"})
		if attempt == 0 {
			if w.Code != 401 || anon.cookie != nil {
				t.Fatal("wrong handle accepted", w.Code, w.Body)
			}
			continue
		}
		if w.Code != 200 || anon.cookie == nil || w.Header().Get("X-Trace-Id") != "11111111111111111111111111111111" {
			t.Fatal("valid assertion rejected", w.Code, w.Body)
		}
	}
	s.Store.Ent.User.UpdateOneID(u.ID).SetEnabled(false).ExecX(t.Context())
	disabled := &testClient{t: t, router: s.Router()}
	v := mustJSON(t, disabled, "POST", "/api/v1/auth/passkey/begin", map[string]any{}, 200)
	w = disabled.do("POST", "/api/v1/auth/passkey/finish", a.assertion(t, challengeOf(v), u.ID, 3), map[string]string{"X-Passkey-Token": v["token"].(string)})
	if w.Code != 401 || disabled.cookie != nil {
		t.Fatal("disabled account used passkey", w.Code, w.Body)
	}
}
