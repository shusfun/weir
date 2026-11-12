package center

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/accountsecurity"
)

type turnstileTransport func(*http.Request) (*http.Response, error)

func (f turnstileTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTurnstileHostActionExpiryAndFailure(t *testing.T) {
	s, _ := newInstalledServer(t)
	secret, err := s.Crypto.Encrypt("test-only-secret")
	if err != nil {
		t.Fatal(err)
	}
	cfg := turnstileSettings{SiteKey: "site", Secret: secret, Hostnames: []string{"example.test"}}
	for _, tc := range []struct {
		name             string
		status           int
		response         string
		offline, invalid bool
	}{
		{"success", 200, `{"success":true,"hostname":"example.test","action":"register","challenge_ts":"` + time.Now().UTC().Format(time.RFC3339) + `"}`, false, false},
		{"hostname", 200, `{"success":true,"hostname":"evil.test","action":"register","challenge_ts":"` + time.Now().UTC().Format(time.RFC3339) + `"}`, false, true},
		{"action", 200, `{"success":true,"hostname":"example.test","action":"reset","challenge_ts":"` + time.Now().UTC().Format(time.RFC3339) + `"}`, false, true},
		{"old", 200, `{"success":true,"hostname":"example.test","action":"register","challenge_ts":"2000-01-01T00:00:00Z"}`, false, true},
		{"failed", 200, `{"success":false}`, false, true},
		{"malformed", 200, `{`, false, false},
		{"oversized", 200, strings.Repeat("x", (32<<10)+1), false, false},
		{"unavailable", 503, `{}`, false, false},
		{"offline", 0, "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.TurnstileHTTP = &http.Client{Transport: turnstileTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://challenges.cloudflare.com/turnstile/v0/siteverify" || r.Method != http.MethodPost || r.Header.Get("traceparent") != "" {
					t.Fatal("invalid provider boundary")
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Form.Get("secret") != "test-only-secret" || r.Form.Get("response") != "token" || r.Form.Get("remoteip") != "203.0.113.1" {
					t.Fatal("verification parameters lost")
				}
				if tc.offline {
					return nil, errors.New("offline includes secret must not escape")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
			})}
			err := s.verifyTurnstile(t.Context(), cfg, "token", "register", "203.0.113.1")
			if tc.name == "success" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("invalid response accepted or secret leaked", err)
			}
			if tc.invalid && !errors.Is(err, accountsecurity.ErrHumanInvalid) {
				t.Fatal("wrong rejection category", err)
			}
		})
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	s.TurnstileHTTP = &http.Client{Transport: turnstileTransport(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}
	if s.verifyTurnstile(cancelled, cfg, "token", "register", "ip") == nil {
		t.Fatal("cancelled verification succeeded")
	}
}

func TestTurnstileSettingsApprovalAndLocalLogin(t *testing.T) {
	s, admin := newInstalledServer(t)
	request := map[string]any{"site_key": "test-site", "secret": "test-only-secret", "hostnames": []string{"example.test"}}
	w := admin.do("PUT", "/api/v1/admin/settings/turnstile", request, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "test-only-secret") || decode(t, w)["secret_set"] != true {
		t.Fatal("settings secret boundary", w.Code, w.Body.String())
	}
	cfg, err := s.turnstileSettings(t.Context())
	if err != nil || cfg.Secret == "test-only-secret" {
		t.Fatal("secret not encrypted", err)
	}
	before, err := s.accountPolicy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	doc := before.Document
	doc.Site["human_provider"] = json.RawMessage(`"turnstile"`)
	if err := s.saveAccountPolicy(t.Context(), doc, before.Revision, 1); err != nil {
		t.Fatal(err)
	}
	loginChallenge := admin.do("GET", "/api/v1/auth/human/challenge?purpose=login", nil, nil)
	if loginChallenge.Code != 200 || decode(t, loginChallenge)["provider"] != "altcha" || decode(t, loginChallenge)["sitekey"] != "" {
		t.Fatal("login leaked external provider", loginChallenge.Body.String())
	}
	start := time.Now()
	w = admin.do("GET", "/api/v1/auth/human/challenge?purpose=register", nil, nil)
	if w.Code != 200 || decode(t, w)["provider"] != "turnstile" || decode(t, w)["sitekey"] != "test-site" || strings.Contains(w.Body.String(), "test-only-secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	challenge := decode(t, w)
	encoded, _ := json.Marshal(map[string]any{"challenge": map[string]any{"signature": challenge["signature"]}})
	ticket := base64.StdEncoding.EncodeToString(encoded)
	if wait := 2*time.Second - time.Since(start) + 20*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	if w := admin.do("PUT", "/api/v1/admin/settings/security", map[string]bool{"registration_enabled": true}, nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	count := 0
	s.TurnstileHTTP = &http.Client{Transport: turnstileTransport(func(r *http.Request) (*http.Response, error) {
		count++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"success":true,"hostname":"example.test","action":"register","challenge_ts":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))}, nil
	})}
	req := map[string]string{"email": "turnstile@example.test", "password": "Passw0rd", "altcha": ticket, "turnstile": "token"}
	if w := admin.do("POST", "/api/v1/auth/register", req, nil); w.Code != 201 {
		t.Fatal("external registration", w.Code, w.Body.String())
	}
	if w := admin.do("POST", "/api/v1/auth/register", req, nil); w.Code != 400 {
		t.Fatal("external ticket replay", w.Code, w.Body.String())
	}
	if count != 1 {
		t.Fatal("replayed proof reached third party", count)
	}
	anon := &testClient{t: t, router: admin.router}
	if w := anon.do("GET", "/api/v1/admin/settings/turnstile", nil, nil); w.Code != 401 {
		t.Fatal("anonymous settings exposed")
	}
	user := login(t, s, "turnstile@example.test", "Passw0rd")
	if w := user.do("PUT", "/api/v1/admin/settings/turnstile", request, nil); w.Code != 403 {
		t.Fatal("non-admin settings accepted")
	}
}
