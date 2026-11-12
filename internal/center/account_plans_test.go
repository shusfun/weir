package center

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/accountplan"
	"github.com/shusfun/weir/internal/policydata"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/accesscode"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/subgrant"
	"github.com/shusfun/weir/internal/store/ent/user"
)

func setAccessPolicy(t *testing.T, s *Server, invite, redeem bool) {
	t.Helper()
	v, err := s.accountPolicy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	doc := policydata.Empty()
	doc.Site["invite_required"], _ = json.Marshal(invite)
	doc.Site["redeem_enabled"], _ = json.Marshal(redeem)
	if err = s.saveAccountPolicy(t.Context(), doc, v.Revision, 1); err != nil {
		t.Fatal(err)
	}
}
func issueCode(t *testing.T, admin *testClient, kind string, planID int) string {
	t.Helper()
	body := map[string]any{"kind": kind, "count": 1}
	if planID > 0 {
		body["plan_id"] = planID
	}
	v := mustJSON(t, admin, "POST", "/api/v1/admin/access-codes", body, 201)
	return v["items"].([]any)[0].(map[string]any)["code"].(string)
}

func TestAccessCodeSnapshotAndRegisterTransaction(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "ss2022", "skip_probe": true}, 200)
	profile := s.Store.Ent.SubProfile.Create().SetName("套餐订阅").SaveX(ctx)
	grant := accountplan.Grant{Quota: 4096, Days: 30, NodeIDs: []int{int(n["id"].(float64))}, ProfileIDs: []int{profile.ID}}
	p := mustJSON(t, admin, "POST", "/api/v1/admin/account-plans", map[string]any{"name": "初始套餐", "enabled": true, "grant": grant}, 200)
	pid := int(p["id"].(float64))
	raw := issueCode(t, admin, "invite", pid)
	grant.Quota = 8192
	mustJSON(t, admin, "PUT", "/api/v1/admin/account-plans/"+itoa(pid), map[string]any{"name": "新套餐", "enabled": true, "revision": 1, "grant": grant}, 200)
	mustJSON(t, admin, "PUT", "/api/v1/admin/account-plans/"+itoa(pid), map[string]any{"name": "过时修改", "enabled": true, "revision": 1, "grant": grant}, 409)
	stored := s.Store.Ent.AccessCode.Query().OnlyX(ctx)
	if stored.Digest != digest(raw) || stored.Grant.Quota != 4096 || stored.PlanRevision != 1 {
		t.Fatal("issuance snapshot changed")
	}
	listed := mustJSON(t, admin, "GET", "/api/v1/admin/access-codes", nil, 200)
	data, _ := json.Marshal(listed)
	if strings.Contains(string(data), raw) || strings.Contains(string(data), stored.Digest) {
		t.Fatal("code material leaked in list")
	}
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/security", map[string]any{"registration_enabled": true}, 200)
	setAccessPolicy(t, s, true, true)
	anon := &testClient{t: t, router: s.Router()}
	mustJSON(t, anon, "POST", "/api/v1/auth/register", map[string]any{"email": "missing@invite.test", "password": "Passw0rd"}, 400)
	broken := true
	s.Store.Ent.SubGrant.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if broken {
				return nil, errors.New("injected grant failure")
			}
			return next.Mutate(ctx, m)
		})
	})
	body := map[string]any{"email": "invite@invite.test", "password": "Passw0rd", "invite_code": raw}
	if w := anon.do("POST", "/api/v1/auth/register", body, nil); w.Code != 503 {
		t.Fatalf("grant failure: %d %s", w.Code, w.Body)
	}
	if s.Store.Ent.User.Query().Where(user.EmailEQ("invite@invite.test")).ExistX(ctx) || s.Store.Ent.AccessCode.GetX(ctx, stored.ID).UsedAt != nil {
		t.Fatal("failed grant consumed invitation or created user")
	}
	broken = false
	trace := "8eac1b2c3d4e5f60718293a4b5c6d7e8"
	w := anon.do("POST", "/api/v1/auth/register", body, map[string]string{"traceparent": "00-" + trace + "-0102030405060708-01"})
	if w.Code != 201 {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	u := s.Store.Ent.User.Query().Where(user.EmailEQ("invite@invite.test")).OnlyX(ctx)
	if u.Quota != 4096 || u.Unlimited || u.AllNodes || len(u.NodeIds) != 1 || u.ExpiresAt == nil || time.Until(*u.ExpiresAt) < 29*24*time.Hour {
		t.Fatalf("initial grant: %+v", u)
	}
	if s.Store.Ent.SubGrant.Query().Where(subgrant.UserIDEQ(u.ID)).CountX(ctx) != 1 || s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).CountX(ctx) != 1 {
		t.Fatal("initial authorizations missing")
	}
	if s.Store.Ent.AccessCode.GetX(ctx, stored.ID).UsedBy == nil || *s.Store.Ent.AccessCode.GetX(ctx, stored.ID).UsedBy != u.ID {
		t.Fatal("invitation not consumed")
	}
	if a := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("auth.register"), audit.ActorEQ(u.ID)).OnlyX(ctx); a.Detail["trace_id"] != trace {
		t.Fatal("registration audit trace missing")
	}
	if b := g.config(0).Budgets[userKey(u.ID)]; b.Unlimited || b.Allocated != 4096 {
		t.Fatalf("initial Gate allowance: %+v", b)
	}
	userClient := &testClient{t: t, router: s.Router()}
	mustJSON(t, userClient, "POST", "/api/v1/auth/login", map[string]any{"email": u.Email, "password": "Passw0rd"}, 200)
	mustJSON(t, userClient, "POST", "/api/v1/me/redeem", map[string]any{"code": raw}, 400)
	for _, path := range []string{"/api/v1/admin/account-plans", "/api/v1/admin/access-codes"} {
		mustJSON(t, userClient, "GET", path, nil, 403)
		mustJSON(t, anon, "GET", path, nil, 401)
	}
}

func TestAccessCodeConcurrentRedeemAndRollback(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	setAccessPolicy(t, s, false, true)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "redeem@code.test", "password": "Passw0rd", "quota": 100, "unlimited": false, "monthly_reset": true}, 200)
	uid := int(u["id"].(float64))
	expiry := time.Now().AddDate(0, 0, 5).Truncate(time.Microsecond)
	s.Store.Ent.User.UpdateOneID(uid).SetUsed(20).SetExpiresAt(expiry).ExecX(ctx)
	p := mustJSON(t, admin, "POST", "/api/v1/admin/account-plans", map[string]any{"name": "叠加套餐", "enabled": true, "grant": accountplan.Grant{Quota: 50, Days: 10}}, 200)
	pid := int(p["id"].(float64))
	raw := issueCode(t, admin, "redeem", pid)
	client := &testClient{t: t, router: s.Router()}
	mustJSON(t, client, "POST", "/api/v1/auth/login", map[string]any{"email": "redeem@code.test", "password": "Passw0rd"}, 200)
	var wg sync.WaitGroup
	statuses := make(chan int, 8)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- client.do("POST", "/api/v1/me/redeem", map[string]any{"code": raw}, nil).Code
		}()
	}
	wg.Wait()
	close(statuses)
	success := 0
	for code := range statuses {
		if code == 200 {
			success++
		} else if code != 400 && code != 429 {
			t.Errorf("unexpected concurrent status %d", code)
		}
	}
	if success != 1 {
		t.Fatalf("code redeemed %d times", success)
	}
	got := s.Store.Ent.User.GetX(ctx, uid)
	if got.Quota != 150 || got.Used != 20 || !got.MonthlyReset || !got.ExpiresAt.Equal(expiry.AddDate(0, 0, 10)) {
		t.Fatalf("stacked account: %+v", got)
	}
	raw = issueCode(t, admin, "redeem", pid)
	broken := true
	s.Store.Ent.Audit.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if broken {
				return nil, errors.New("injected audit failure")
			}
			return next.Mutate(ctx, m)
		})
	})
	w := client.do("POST", "/api/v1/me/redeem", map[string]any{"code": raw}, nil)
	if w.Code != 503 {
		t.Fatalf("audit fault: %d %s", w.Code, w.Body)
	}
	if s.Store.Ent.User.GetX(ctx, uid).Quota != 150 || s.Store.Ent.AccessCode.Query().Where(accesscode.DigestEQ(digest(raw))).OnlyX(ctx).UsedAt != nil {
		t.Fatal("audit failure partially committed redemption")
	}
	broken = false
	if w = client.do("POST", "/api/v1/me/redeem", map[string]any{"code": raw}, nil); w.Code != 200 {
		t.Fatalf("retry after rollback: %d %s", w.Code, w.Body)
	}
	if s.Store.Ent.User.GetX(ctx, uid).Quota != 200 {
		t.Fatal("rollback retry granted wrong allowance")
	}
}

func TestInvitationConcurrentClaimAndRevocation(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	setAccessPolicy(t, s, true, false)
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/security", map[string]any{"registration_enabled": true}, 200)
	raw := issueCode(t, admin, "invite", 0)
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, email := range []string{"one@concurrent.test", "two@concurrent.test"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := &testClient{t: t, router: s.Router()}
			statuses <- c.do("POST", "/api/v1/auth/register", map[string]any{"email": email, "password": "Passw0rd", "invite_code": raw}, nil).Code
		}()
	}
	wg.Wait()
	close(statuses)
	success := 0
	for code := range statuses {
		if code == 201 {
			success++
		} else if code != 400 {
			t.Errorf("unexpected invite status %d", code)
		}
	}
	if success != 1 {
		t.Fatalf("invite used %d times", success)
	}
	raw = issueCode(t, admin, "invite", 0)
	row := s.Store.Ent.AccessCode.Query().Where(accesscode.DigestEQ(digest(raw))).OnlyX(ctx)
	mustJSON(t, admin, "DELETE", "/api/v1/admin/access-codes/"+itoa(row.ID), nil, 200)
	anon := &testClient{t: t, router: s.Router()}
	mustJSON(t, anon, "POST", "/api/v1/auth/register", map[string]any{"email": "revoked@concurrent.test", "password": "Passw0rd", "invite_code": raw}, 400)
	if s.Store.Ent.User.Query().Where(user.EmailEQ("revoked@concurrent.test")).ExistX(ctx) {
		t.Fatal("revoked invitation created account")
	}
}
