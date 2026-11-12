package center

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/accountsecurity"
	"github.com/shusfun/weir/internal/diagnostic"
	"github.com/shusfun/weir/internal/policydata"
	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/setting"
)

func TestAccountPolicyApprovalHistoryRollbackAndPermissions(t *testing.T) {
	s, admin := newInstalledServer(t)
	before, err := s.accountPolicy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	doc := before.Document
	doc.External["code_attempts"] = json.RawMessage(`3`)
	doc.Site["mail_site_hour"] = json.RawMessage(`1200`)
	b, _ := json.Marshal(doc)
	w := admin.do("POST", "/api/v1/admin/settings/account-policy/draft", map[string]any{"text": string(b)}, map[string]string{"traceparent": "00-11111111111111111111111111111111-2222222222222222-01"})
	if w.Code != 201 {
		t.Fatalf("draft %d %s", w.Code, w.Body)
	}
	var d Draft
	if err = json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	current, _ := s.accountPolicy(t.Context())
	if current.Revision != before.Revision || len(d.Steps[0].Diff) == 0 || !strings.Contains(strings.Join(d.Steps[0].Preview, " "), "放宽警告") {
		t.Fatal("missing preview or immediate activation", d)
	}
	for _, source := range []string{"analysis"} {
		if _, ok := findCap(source, "account.policy.update"); ok {
			t.Fatal("automatic actor can change account policy", source)
		}
	}
	if capb, ok := findCap("observation", "account.policy.update"); !ok || capb.Kind != "draft" {
		t.Fatal("observation must use approval drafts")
	}
	apply := admin.do("POST", "/api/v1/admin/ai/drafts/"+d.ID+"/apply", map[string]any{}, map[string]string{"traceparent": "00-33333333333333333333333333333333-4444444444444444-01"})
	if apply.Code != 200 {
		t.Fatalf("apply %d %s", apply.Code, apply.Body)
	}
	after, err := s.accountPolicy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision == before.Revision || string(after.Effective["code_attempts"]) != "3" || after.Sources["code_attempts"] != "external" {
		t.Fatal("not applied", after)
	}
	entry, err := s.diagnosticEntry(t.Context(), diagnostic.Selection{Kind: "account-policy", ID: "current"})
	if err != nil || entry.Evidence["revision"] != after.Revision || len(entry.Reproduction) == 0 {
		t.Fatal("diagnostic policy projection missing", entry, err)
	}
	a := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("account.policy.apply")).OnlyX(t.Context())
	if a.Detail["trace_id"] != "33333333333333333333333333333333" {
		t.Fatal("policy audit trace missing", a.Detail)
	}
	history := mustJSON(t, admin, "GET", "/api/v1/admin/settings/account-policy/history", nil, 200)
	if len(history["items"].([]any)) != 2 {
		t.Fatal("history missing", history)
	}
	for _, item := range history["items"].([]any) {
		row := item.(map[string]any)
		data, ok := row["data"].(map[string]any)
		if !ok || data["document"] == nil {
			t.Fatal("history omitted restorable document", row)
		}
		b, _ := json.Marshal(data["document"])
		var historic policydata.Document
		if json.Unmarshal(b, &historic) != nil {
			t.Fatal("history document shape changed")
		}
		resolved, err := accountsecurity.ResolvePolicy(historic)
		if err != nil || resolved.Revision != row["key"] {
			t.Fatal("history key does not restore document", err)
		}
	}
	if err = s.saveAccountPolicy(t.Context(), policydata.Empty(), before.Revision, 1); err == nil {
		t.Fatal("stale update accepted")
	}
	if err = s.saveAccountPolicy(t.Context(), policydata.Empty(), after.Revision, 1); err != nil {
		t.Fatal(err)
	}
	restored, _ := s.accountPolicy(t.Context())
	if restored.Revision != before.Revision {
		t.Fatal("rollback changed defaults")
	}
	anon := &testClient{t: t, router: s.Router()}
	mustJSON(t, anon, "GET", "/api/v1/admin/settings/account-policy", nil, 401)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "policy-reader@example.test", "password": "Passw0rd"}, 200)
	member := login(t, s, "policy-reader@example.test", "Passw0rd")
	for _, path := range []string{"/settings/account-policy", "/settings/account-policy/history"} {
		mustJSON(t, member, "GET", "/api/v1/admin"+path, nil, 403)
	}
	mustJSON(t, member, "POST", "/api/v1/admin/settings/account-policy/draft", map[string]any{"text": string(b)}, 403)
}

func TestAccountPolicyStaleDraftAndAuditFailureRollback(t *testing.T) {
	s, admin := newInstalledServer(t)
	v, _ := s.accountPolicy(t.Context())
	doc := policydata.Empty()
	doc.Site["code_attempts"] = json.RawMessage(`3`)
	b, _ := json.Marshal(doc)
	d, err := s.submitActionDraft(t.Context(), "dsh", "tighter limits", []DraftStep{{Action: "account.policy.update", Args: map[string]any{"text": string(b)}}})
	if err != nil {
		t.Fatal(err)
	}
	other := policydata.Empty()
	other.Site["code_attempts"] = json.RawMessage(`4`)
	if err = s.saveAccountPolicy(t.Context(), other, v.Revision, 1); err != nil {
		t.Fatal(err)
	}
	w := admin.do("POST", "/api/v1/admin/ai/drafts/"+d.ID+"/apply", nil, nil)
	if w.Code != 409 {
		t.Fatalf("stale draft accepted %d %s", w.Code, w.Body)
	}
	before, _ := s.accountPolicy(t.Context())
	s.Store.Ent.Audit.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			return nil, errors.New("audit unavailable")
		})
	})
	if err = s.saveAccountPolicy(t.Context(), policydata.Empty(), before.Revision, 1); err == nil {
		t.Fatal("saved without audit")
	}
	after, _ := s.accountPolicy(t.Context())
	if before.Revision != after.Revision {
		t.Fatal("audit failure partially saved policy")
	}
}

func TestAccountMailBudgetAtomicAndFailClosed(t *testing.T) {
	s, admin := newInstalledServer(t)
	s.Mail = &fakeMail{}
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/smtp", map[string]any{"host": "smtp.example.test", "port": 465, "from": "noreply@example.test", "use_tls": true}, 200)
	v, _ := s.accountPolicy(t.Context())
	doc := policydata.Empty()
	doc.Site["mail_site_hour"] = json.RawMessage(`2`)
	if err := s.saveAccountPolicy(t.Context(), doc, v.Revision, 1); err != nil {
		t.Fatal(err)
	}
	anon := &testClient{t: t, router: s.Router()}
	for _, email := range []string{"one@example.test", "two@example.test"} {
		mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": email, "purpose": "reset"}, 200)
	}
	w := anon.do("POST", "/api/v1/auth/email", map[string]any{"email": "three@example.test", "purpose": "reset"}, nil)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "email_rate_limited") {
		t.Fatal(w.Code, w.Body)
	}
	if n := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("security.email-send-limit")).CountX(t.Context()); n != 1 {
		t.Fatal("limit observation missing", n)
	}
	mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": "four@example.test", "purpose": "reset"}, 429)
	if n := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("security.email-send-limit")).CountX(t.Context()); n != 1 {
		t.Fatal("repeated refusal flooded audit", n)
	}

	limits := accountsecurity.DefaultPolicy().MailLimits
	limits.Site = 3
	now := time.Now().Add(2 * time.Hour)
	var accepted, failed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := accountsecurity.ReserveMail(t.Context(), s.Store.KV, limits, "atomic@example.test", "198.51.100.2", now)
			if err == nil {
				accepted.Add(1)
			} else {
				var limit *accountsecurity.LimitError
				if !errors.As(err, &limit) {
					failed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 3 || failed.Load() != 0 {
		t.Fatal("shared store limit bypass", accepted.Load(), failed.Load())
	}
	original := s.Store.KV
	s.Store.KV = mailCounterFailure{KV: original}
	mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": "storage@example.test", "purpose": "reset"}, 503)
	s.Store.KV = original
}

type mailCounterFailure struct{ store.KV }

func (mailCounterFailure) Incr(context.Context, string, time.Duration) (int64, error) {
	return 0, errors.New("counter unavailable")
}

func TestInvalidStoredAccountPolicyRejectsMail(t *testing.T) {
	s, admin := newInstalledServer(t)
	s.Mail = &fakeMail{}
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/smtp", map[string]any{"host": "smtp.example.test", "port": 465, "from": "noreply@example.test", "use_tls": true}, 200)
	s.Store.Ent.Setting.Create().SetKey(accountPolicyKey).SetValue(map[string]any{"schema": 1, "external": map[string]any{}, "site": map[string]any{"code_attempts": 1000}}).OnConflictColumns(setting.FieldKey).UpdateNewValues().ExecX(t.Context())
	anon := &testClient{t: t, router: s.Router()}
	w := anon.do("POST", "/api/v1/auth/email", map[string]any{"email": "unavailable@example.test", "purpose": "reset"}, nil)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "account_policy_unavailable") {
		t.Fatal(w.Code, w.Body)
	}
}
