package center

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/shusfun/weir/internal/policydata"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/user"
)

type emailDNSFunc func(context.Context, string) ([]*net.MX, error)

func (f emailDNSFunc) LookupMX(ctx context.Context, d string) ([]*net.MX, error) { return f(ctx, d) }

func TestRegistrationEmailPolicyPrecedesAccountLookupAndCodeIssue(t *testing.T) {
	s, admin := newInstalledServer(t)
	s.Mail = &fakeMail{}
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/smtp", map[string]any{"host": "smtp.example.test", "port": 465, "from": "noreply@example.test"}, 200)
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/security", map[string]any{"registration_enabled": true, "email_verification": true}, 200)
	before, _ := s.accountPolicy(t.Context())
	doc := policydata.Empty()
	doc.Site["email_allow_suffixes"] = json.RawMessage(`["@example.test","*.allowed.test"]`)
	doc.Site["email_deny_suffixes"] = json.RawMessage(`["@blocked.allowed.test"]`)
	if err := s.saveAccountPolicy(t.Context(), doc, before.Revision, 1); err != nil {
		t.Fatal(err)
	}
	// Both an existing and an unknown account get the same source-policy refusal.
	s.Store.Ent.User.Create().SetEmail("existing@blocked.allowed.test").SetPasswordHash("unused").SaveX(t.Context())
	var lookups atomic.Int32
	s.EmailDNS = emailDNSFunc(func(context.Context, string) ([]*net.MX, error) {
		lookups.Add(1)
		return nil, errors.New("DNS must not run for denied suffixes")
	})
	anon := &testClient{t: t, router: s.Router()}
	for _, email := range []string{"existing@blocked.allowed.test", "missing@blocked.allowed.test", "missing@allowed.test", "missing@elsewhere.test"} {
		for _, endpoint := range []string{"/auth/email", "/auth/register"} {
			w := anon.do("POST", "/api/v1"+endpoint, map[string]any{"email": email, "purpose": "register", "password": "Passw0rd"}, nil)
			if w.Code != 400 || !strings.Contains(w.Body.String(), "email_suffix_denied") {
				t.Fatal(w.Code, w.Body)
			}
		}
		if _, ok, _ := s.Store.KV.Get(t.Context(), "email-code:register:"+digest(email)); ok {
			t.Fatal("denied request issued a code")
		}
	}
	if lookups.Load() != 0 || len(s.Mail.(*fakeMail).sent) != 0 {
		t.Fatal("denied source caused DNS or mail")
	}
	pub := mustJSON(t, anon, "GET", "/api/v1/public", nil, 200)
	if len(pub["registration_email_suffixes"].([]any)) != 2 {
		t.Fatal("public suffix policy absent", pub)
	}
	// Reset is deliberately exempt so existing users can recover their accounts.
	mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": "existing@blocked.allowed.test", "purpose": "reset"}, 200)
}

func TestRegistrationMXFailureAndDisposableRefusal(t *testing.T) {
	s, admin := newInstalledServer(t)
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/security", map[string]any{"registration_enabled": true}, 200)
	anon := &testClient{t: t, router: s.Router()}
	w := anon.do("POST", "/api/v1/auth/register", map[string]any{"email": "x@mailinator.com", "password": "Passw0rd"}, nil)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "email_disposable") {
		t.Fatal(w.Code, w.Body)
	}
	if !s.Store.Ent.Audit.Query().Where(audit.ActionEQ("security.disposable-email")).ExistX(t.Context()) {
		t.Fatal("missing safe security event")
	}
	for _, tc := range []struct {
		records []*net.MX
		err     error
		code    string
		status  int
	}{
		{nil, nil, "email_mx_missing", 400}, {[]*net.MX{{Host: "."}}, nil, "email_mx_missing", 400},
		{nil, &net.DNSError{IsTimeout: true}, "email_mx_unavailable", 503},
	} {
		s.EmailDNS = emailDNSFunc(func(context.Context, string) ([]*net.MX, error) { return tc.records, tc.err })
		w := anon.do("POST", "/api/v1/auth/register", map[string]any{"email": "mx@example.test", "password": "Passw0rd"}, map[string]string{"traceparent": "00-11111111111111111111111111111111-2222222222222222-01"})
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || w.Header().Get("X-Trace-Id") != "11111111111111111111111111111111" {
			t.Fatal(w.Code, w.Body)
		}
	}
	if s.Store.Ent.User.Query().Where(user.EmailEQ("mx@example.test")).ExistX(t.Context()) {
		t.Fatal("MX failure created user")
	}
}

func TestRegistrationAliasConcurrencyAndLegacyAccounts(t *testing.T) {
	s, admin := newInstalledServer(t)
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/security", map[string]any{"registration_enabled": true}, 200)
	var wg sync.WaitGroup
	var accepted, duplicate, failed atomic.Int32
	router := s.Router()
	for _, email := range []string{"a.b+1@gmail.com", "ab+2@gmail.com", "a.b+3@googlemail.com", "ab+4@googlemail.com"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := (&testClient{t: t, router: router}).do("POST", "/api/v1/auth/register", map[string]any{"email": email, "password": "Passw0rd"}, nil)
			switch w.Code {
			case 201:
				accepted.Add(1)
			case 409:
				duplicate.Add(1)
			default:
				failed.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 || duplicate.Load() != 3 || failed.Load() != 0 {
		t.Fatal("alias registration race", accepted.Load(), duplicate.Load(), failed.Load())
	}
	u := s.Store.Ent.User.Query().Where(user.NormalizedEmailEQ("ab@gmail.com")).OnlyX(t.Context())
	login(t, s, u.Email, "Passw0rd")
	// Old backups lack the new column. Current rules must still protect them.
	s.Store.Ent.User.Create().SetEmail("old.name+tag@gmail.com").SetPasswordHash("unused").SaveX(t.Context())
	w := (&testClient{t: t, router: router}).do("POST", "/api/v1/auth/register", map[string]any{"email": "oldname@googlemail.com", "password": "Passw0rd"}, nil)
	if w.Code != 409 {
		t.Fatal("legacy identity bypass", w.Code, w.Body)
	}
	before, _ := s.accountPolicy(t.Context())
	doc := policydata.Empty()
	doc.Site["email_alias_rules"] = json.RawMessage(`[{"domain":"example.test","canonical_domain":"example.test","strip_plus":true,"strip_dots":true}]`)
	if err := s.saveAccountPolicy(t.Context(), doc, before.Revision, 1); err != nil {
		t.Fatal(err)
	}
	s.Store.Ent.User.Create().SetEmail("legacy.alias@example.test").SetPasswordHash("unused").SaveX(t.Context())
	w = (&testClient{t: t, router: router}).do("POST", "/api/v1/auth/register", map[string]any{"email": "legacyalias+new@example.test", "password": "Passw0rd"}, nil)
	if w.Code != 409 {
		t.Fatal("changed rules ignored old account", w.Code, w.Body)
	}
}
