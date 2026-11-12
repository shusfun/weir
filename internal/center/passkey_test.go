package center

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiscoverablePasskeyOptionsAndDisabledPolicy(t *testing.T) {
	s, admin := newInstalledServer(t)
	anon := &testClient{t: t, router: s.Router()}
	for _, email := range []string{"", "unknown@example.test", "admin@example.test"} {
		v := mustJSON(t, anon, "POST", "/api/v1/auth/passkey/begin", map[string]any{"email": email}, 200)
		pk := v["options"].(map[string]any)["publicKey"].(map[string]any)
		if pk["userVerification"] != "required" || pk["allowCredentials"] != nil {
			t.Fatal("not a discoverable assertion", pk)
		}
		raw, ok, err := s.Store.KV.Get(t.Context(), "passkey:"+digest(v["token"].(string)))
		var ses passSession
		if err != nil || !ok || json.Unmarshal([]byte(raw), &ses) != nil || ses.UserID != 0 {
			t.Fatal("challenge bound to supplied email", err)
		}
		for attempt := 0; attempt < 2; attempt++ {
			w := anon.do("POST", "/api/v1/auth/passkey/finish", map[string]any{}, map[string]string{"X-Passkey-Token": v["token"].(string)})
			want := "passkey_verification_failed"
			if attempt == 1 {
				want = "passkey_challenge_invalid"
			}
			if w.Code != 401 || !strings.Contains(w.Body.String(), want) || anon.cookie != nil {
				t.Fatal(w.Code, w.Body)
			}
		}
	}
	v := mustJSON(t, admin, "POST", "/api/v1/security/passkey/begin", map[string]any{"password": "Passw0rd"}, 200)
	selection := v["options"].(map[string]any)["publicKey"].(map[string]any)["authenticatorSelection"].(map[string]any)
	if selection["residentKey"] != "required" || selection["requireResidentKey"] != true {
		t.Fatal("new passkey is not discoverable", selection)
	}
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/security", map[string]any{"passkey_enabled": false}, 200)
	for _, path := range []string{"/api/v1/auth/passkey/begin", "/api/v1/auth/passkey/finish"} {
		w := anon.do("POST", path, map[string]any{}, nil)
		if w.Code != 403 || !strings.Contains(w.Body.String(), "passkey_disabled") {
			t.Fatal(w.Code, w.Body)
		}
	}
}
