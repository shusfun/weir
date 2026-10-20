package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imzyb/MiGate/internal/mailer"
	"strconv"
)

func testSettingsTransactions(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	before, err := readSystemSettings(ctx, s.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for key, value := range map[string]any{"security": before.Security, "smtp": before.SMTP, "laya": before.Laya} {
			if err := s.saveSetting(ctx, key, value); err != nil {
				t.Error(err)
			}
		}
	}()
	base := systemSettingsUpdate{Security: before.Security, SMTP: mailer.SMTPConfig{Host: "smtp.example.test", Port: 465, Username: "operator", Password: "private-smtp-password", From: "sender@example.test", UseTLS: true}, Laya: ModelConfig{URL: "https://model.example.test/v1/systemone", Model: "observer", Key: "private-model-key"}, Revision: before.Revision, Password: "local-testing-password"}
	call := func(body systemSettingsUpdate, want int) {
		t.Helper()
		w := request("PUT", "/api/v1/admin/settings", body, "", admin)
		if w.Code != want {
			t.Fatalf("settings write HTTP %d want %d", w.Code, want)
		}
	}
	call(base, 200)
	stored, err := readSystemSettings(ctx, s.DB)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SMTP.Password == base.SMTP.Password || stored.Laya.Key == base.Laya.Key || stored.SMTP.Password == "" || stored.Laya.Key == "" {
		t.Fatal("settings secrets not encrypted")
	}
	response := request("GET", "/api/v1/admin/settings", nil, "", admin)
	if response.Code != 200 || strings.Contains(response.Body.String(), "private-") || strings.Contains(response.Body.String(), stored.SMTP.Password) {
		t.Fatal("settings exposed secret")
	}
	var view map[string]any
	json.Unmarshal(response.Body.Bytes(), &view)
	if view["smtp_password_set"] != true || view["laya_key_set"] != true || view["settings_revision"] != stored.Revision {
		t.Fatal("settings presence/revision missing")
	}
	if request("PUT", "/api/v1/admin/settings", base, "", alice).Code != 403 {
		t.Fatal("ordinary user updated settings")
	}
	base.SMTP.Password = ""
	base.Laya.Key = ""
	base.Revision = stored.Revision
	call(base, 200)
	preserved, _ := readSystemSettings(ctx, s.DB)
	if preserved.SMTP.Password != stored.SMTP.Password || preserved.Laya.Key != stored.Laya.Key {
		t.Fatal("blank fields erased secrets")
	}
	// Force the final write to fail after security and SMTP writes. The entire
	// transaction must roll back; no partial security policy can be activated.
	_, err = s.DB.ExecContext(ctx, `ALTER TABLE migate.resources ADD CONSTRAINT acceptance_settings_failure CHECK (NOT (kind='settings' AND key='laya' AND data->>'model'='transaction-failure'))`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.ExecContext(ctx, `ALTER TABLE migate.resources DROP CONSTRAINT IF EXISTS acceptance_settings_failure`)
	failure := base
	failure.Security.Registration = !failure.Security.Registration
	failure.SMTP.FromName = "must-not-save"
	failure.Laya.Model = "transaction-failure"
	call(failure, 503)
	afterFailure, _ := readSystemSettings(ctx, s.DB)
	if afterFailure.Revision != preserved.Revision {
		t.Fatal("failed settings transaction partially changed state")
	}
	base.SMTP.FromName = "updated"
	call(base, 200)
	call(base, 409) // old revision cannot overwrite another administrator.
	current, _ := readSystemSettings(ctx, s.DB)
	base.Revision = current.Revision
	base.Laya.URL = "https://model.example.test/?key=secret"
	call(base, 400)
	base.Laya.URL = ""
	base.Laya.Enabled = false
	base.ClearLayaKey = true
	base.SMTP = mailer.SMTPConfig{}
	base.ClearSMTPPassword = true
	call(base, 200)
	cleared, _ := readSystemSettings(ctx, s.DB)
	if cleared.SMTP.Password != "" || cleared.Laya.Key != "" {
		t.Fatal("explicit secret clear failed")
	}
	base.Revision = cleared.Revision
	base.Security.RequireAdminMFA = true
	base.Security.TOTP = false
	base.Security.Passkey = false
	call(base, 400)
	// Bounded mail retry changes only a failed encrypted queue item.
	sealed, _ := s.Crypto.Encrypt("recovery-message-fixture")
	mail, err := s.Store.Resource.Create().SetKind("mail").SetKey("settings-retry-fixture").SetData(map[string]any{"status": "failed", "attempts": 3, "body": sealed, "to": "recipient@example.test"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(mail).Exec(ctx)
	path := "/api/v1/admin/mail/" + strconv.Itoa(mail.ID) + "/retry"
	if request("POST", path, nil, "", alice).Code != 403 {
		t.Fatal("ordinary user retried mail")
	}
	if request("POST", path, nil, "", admin).Code != 202 {
		t.Fatal("mail retry failed")
	}
	if request("POST", path, nil, "", admin).Code != 409 {
		t.Fatal("pending mail duplicated")
	}
	mail, _ = s.Store.Resource.Get(ctx, mail.ID)
	if mail.Data["body"] != sealed || number(mail.Data["attempts"]) != 3 {
		t.Fatal("retry changed mail material or reset attempts")
	}
	mail.Data["status"] = "failed"
	mail.Data["attempts"] = 5
	s.Store.Resource.UpdateOne(mail).SetData(mail.Data).Save(ctx)
	if request("POST", path, nil, "", admin).Code != 409 {
		t.Fatal("mail retry cap bypassed")
	}
	w := request("GET", "/api/v1/admin/resources/mail", nil, "", admin)
	if strings.Contains(w.Body.String(), sealed) || strings.Contains(w.Body.String(), "recovery-message-fixture") {
		t.Fatal("queue list exposed body")
	}
	// Corrupt JSON field types must never silently turn off MFA or verification.
	if err := s.saveSetting(ctx, "security", map[string]any{"require_admin_mfa": "invalid-type"}); err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		method, path string
		body         any
		cookie       string
	}{
		{"GET", "/api/v1/public", nil, ""},
		{"GET", "/api/v1/admin/settings", nil, admin},
		{"POST", "/api/v1/auth/login", map[string]any{"email": "admin@example.test", "password": "local-testing-password"}, ""},
		{"POST", "/api/v1/auth/passkey/begin", map[string]any{"email": "admin@example.test"}, ""},
		{"POST", "/api/v1/auth/register", map[string]any{"email": "new@example.test", "password": "local-testing-password"}, ""},
	} {
		if response := request(probe.method, probe.path, probe.body, "", probe.cookie); response.Code != 503 {
			t.Fatalf("unavailable policy %s returned %d", probe.path, response.Code)
		}
	}
	if err := s.saveSetting(ctx, "security", before.Security); err != nil {
		t.Fatal(err)
	}
}
