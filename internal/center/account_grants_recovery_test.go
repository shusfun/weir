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

	"github.com/shusfun/weir/internal/accountplan"
	"github.com/shusfun/weir/internal/diagnostic"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/accesscode"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/user"
)

func TestAccountGrantPendingRecoversWithoutReissuing(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	setAccessPolicy(t, s, false, true)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "ss2022", "skip_probe": true}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "pending@grant.test", "password": "Passw0rd", "quota": 0, "unlimited": false, "all_nodes": false}, 200)
	uid := int(u["id"].(float64))
	p := mustJSON(t, admin, "POST", "/api/v1/admin/account-plans", map[string]any{"name": "恢复套餐", "enabled": true, "grant": accountplan.Grant{Quota: 4096, NodeIDs: []int{int(n["id"].(float64))}}}, 200)
	raw := issueCode(t, admin, "redeem", int(p["id"].(float64)))
	member := login(t, s, "pending@grant.test", "Passw0rd")
	var broken atomic.Bool
	broken.Store(true)
	s.Store.Ent.Gate.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if broken.Load() {
				return nil, errors.New("injected configuration sync failure")
			}
			return next.Mutate(ctx, m)
		})
	})
	trace := "87654321876543218765432187654321"
	w := member.do("POST", "/api/v1/me/redeem", map[string]any{"code": raw}, map[string]string{"traceparent": "00-" + trace + "-1234567812345678-01"})
	if w.Code != 202 || decode(t, w)["synchronization"] != "pending" {
		t.Fatal(w.Code, w.Body)
	}
	marker := s.Store.Ent.Resource.Query().Where(resource.KindEQ(accountGrantSyncKind)).OnlyX(ctx)
	if marker.Data["trace_id"] != trace || s.Store.Ent.User.GetX(ctx, uid).Quota != 4096 {
		t.Fatal("commit or persisted correlation missing")
	}
	if err := s.ReconcileAccountGrants(ctx); err == nil {
		t.Fatal("failed synchronization reported complete")
	}
	broken.Store(false)
	// A fresh service has no in-memory pending list; only the persisted marker
	// and committed user state are used to recover the interrupted operation.
	restarted := &Server{Store: s.Store, Config: s.Config, Crypto: s.Crypto}
	for i := 0; i < 2; i++ {
		if err := restarted.ReconcileAccountGrants(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if s.Store.Ent.Resource.Query().Where(resource.KindEQ(accountGrantSyncKind)).ExistX(ctx) {
		t.Fatal("completed marker retained")
	}
	if s.Store.Ent.User.GetX(ctx, uid).Quota != 4096 || s.Store.Ent.Audit.Query().Where(audit.ActionEQ("account.code.redeem"), audit.ActorEQ(uid)).CountX(ctx) != 1 {
		t.Fatal("recovery repeated grant")
	}
	stages := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("account.grant.sync"), audit.TargetEQ("user:"+itoa(uid))).Order(ent.Asc(audit.FieldID)).AllX(ctx)
	if len(stages) != 2 || stages[0].Result != "pending" || stages[1].Result != "ok" || stages[0].Detail["trace_id"] != trace || stages[1].Detail["trace_id"] != trace {
		t.Fatal("recovery timeline duplicated or lost trace", stages)
	}
	evidence, err := s.diagnosticEntry(ctx, diagnostic.Selection{Kind: "audit", ID: itoa(stages[0].ID)})
	if err != nil || evidence.Evidence["source"] != diagnosticSources["center.reconcileAccountGrant"] || evidence.Evidence["trace_id"] != trace || evidence.Evidence["error_code"] != "grant_sync_failed" {
		t.Fatal("pending grant diagnostic lost error origin or correlation", evidence, err)
	}
	if b := g.config(0).Budgets[userKey(uid)]; b.Unlimited || b.Allocated != 4096 {
		t.Fatal("recovered budget", b)
	}
	mustJSON(t, member, "POST", "/api/v1/me/redeem", map[string]any{"code": raw}, 400)
}

func TestAccessCodeIssuanceRollbackExpiryAndMailPrecheck(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	s.Mail = &fakeMail{}
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/smtp", map[string]any{"host": "smtp.example.test", "port": 465, "from": "noreply@example.test"}, 200)
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/security", map[string]any{"registration_enabled": true, "email_verification": true}, 200)
	setAccessPolicy(t, s, true, false)
	anon := &testClient{t: t, router: s.Router()}
	for _, code := range []string{"", "invalid"} {
		mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": "precheck@example.test", "purpose": "register", "invite_code": code}, 400)
	}
	if len(s.Mail.(*fakeMail).sent) != 0 {
		t.Fatal("invalid invitation sent mail")
	}
	if _, ok, err := s.Store.KV.Get(ctx, "email-code:register:"+digest("precheck@example.test")); err != nil || ok {
		t.Fatal("invalid invitation issued mail code", err)
	}
	raw := issueCode(t, admin, "invite", 0)
	mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": "precheck@example.test", "purpose": "register", "invite_code": raw}, 200)
	row := s.Store.Ent.AccessCode.Query().Where(accesscode.DigestEQ(digest(raw))).OnlyX(ctx)
	if len(s.Mail.(*fakeMail).sent) != 1 || row.UsedAt != nil {
		t.Fatal("mail precheck claimed invitation or did not send")
	}
	row.Update().SetExpiresAt(time.Now().Add(-time.Minute)).ExecX(ctx)
	mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": "expired@example.test", "purpose": "register", "invite_code": raw}, 400)
	if len(s.Mail.(*fakeMail).sent) != 1 {
		t.Fatal("expired invitation sent mail")
	}
	p := mustJSON(t, admin, "POST", "/api/v1/admin/account-plans", map[string]any{"name": "停用套餐", "enabled": false, "grant": accountplan.Grant{}}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/access-codes", map[string]any{"kind": "redeem", "count": 2, "plan_id": p["id"]}, 400)
	before := s.Store.Ent.AccessCode.Query().CountX(ctx)
	s.Store.Ent.Audit.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) {
			return nil, errors.New("injected issuance audit failure")
		})
	})
	w := admin.do("POST", "/api/v1/admin/access-codes", map[string]any{"kind": "invite", "count": 3}, nil)
	if w.Code != 503 || strings.Contains(w.Body.String(), "wi_") || s.Store.Ent.AccessCode.Query().CountX(ctx) != before {
		t.Fatal("issuance audit failure did not roll back", w.Code, w.Body)
	}
}

func TestAccessCodesDifferentCodesStackConcurrently(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	setAccessPolicy(t, s, false, true)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "stack@grant.test", "password": "Passw0rd", "quota": 100, "unlimited": false}, 200)
	p := mustJSON(t, admin, "POST", "/api/v1/admin/account-plans", map[string]any{"name": "叠加", "enabled": true, "grant": accountplan.Grant{Quota: 50}}, 200)
	member := login(t, s, "stack@grant.test", "Passw0rd")
	codes := []string{issueCode(t, admin, "redeem", int(p["id"].(float64))), issueCode(t, admin, "redeem", int(p["id"].(float64)))}
	var wg sync.WaitGroup
	for _, code := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := member.do("POST", "/api/v1/me/redeem", map[string]any{"code": code}, nil)
			if w.Code != 200 {
				t.Error(w.Code, w.Body)
			}
		}()
	}
	wg.Wait()
	u := s.Store.Ent.User.Query().Where(user.EmailEQ("stack@grant.test")).OnlyX(ctx)
	if u.Quota != 200 {
		t.Fatal("lost concurrent grant", u.Quota)
	}
	listed := mustJSON(t, admin, "GET", "/api/v1/admin/audit?scope=accounts&page_size=100", nil, 200)
	data, _ := json.Marshal(listed)
	if !strings.Contains(string(data), "account.code.redeem") || strings.Contains(string(data), "user.create") {
		t.Fatal("account observation filter omitted grants or included unrelated audit")
	}
}
