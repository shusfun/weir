package center

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/accountplan"
	"github.com/shusfun/weir/internal/store/ent/accesscode"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

func TestAccountCodesTraceSmoke(t *testing.T) {
	s, admin := newInstalledServer(t)
	setAccessPolicy(t, s, false, true)
	trace := "4eac1b2c3d4e5f60718293a4b5c6d7e8"
	headers := map[string]string{"traceparent": "00-" + trace + "-0102030405060708-01"}
	p := admin.do("POST", "/api/v1/admin/account-plans", map[string]any{"name": "流量", "enabled": true, "grant": accountplan.Grant{Quota: 123}}, headers)
	if p.Code != 200 {
		t.Fatalf("plan: %d %s", p.Code, p.Body)
	}
	var plan map[string]any
	json.Unmarshal(p.Body.Bytes(), &plan)
	w := admin.do("POST", "/api/v1/admin/access-codes", map[string]any{"kind": "redeem", "count": 2, "plan_id": plan["id"]}, headers)
	if w.Code != 201 {
		t.Fatalf("issue: %d %s", w.Code, w.Body)
	}
	var issued struct {
		Items []struct {
			ID   int    `json:"id"`
			Code string `json:"code"`
		} `json:"items"`
	}
	json.Unmarshal(w.Body.Bytes(), &issued)
	w = admin.do("POST", "/api/v1/me/redeem", map[string]any{"code": issued.Items[0].Code}, headers)
	if w.Code != 200 {
		t.Fatalf("redeem: %d %s", w.Code, w.Body)
	}
	w = admin.do("DELETE", "/api/v1/admin/access-codes/"+itoa(issued.Items[1].ID), nil, headers)
	if w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	for _, action := range []string{"account.plan", "account.code.issue", "account.code.redeem", "account.grant.sync", "account.code.revoke"} {
		a := s.Store.Ent.Audit.Query().Where(audit.ActionEQ(action)).OnlyX(t.Context())
		if a.Detail["trace_id"] != trace {
			t.Fatalf("%s trace missing: %+v", action, a.Detail)
		}
	}
	for _, row := range s.Store.Ent.AccessCode.Query().AllX(t.Context()) {
		if row.TraceID != trace {
			t.Fatal("issuance trace missing")
		}
	}
	for _, path := range []string{"/api/v1/me/redemptions", "/api/v1/admin/access-codes?status=used"} {
		w = admin.do("GET", path, nil, headers)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"synchronization":"ok"`) {
			t.Fatalf("history: %d %s", w.Code, w.Body)
		}
		if w.Header().Get("X-Trace-ID") != trace {
			t.Fatalf("response trace: %+v", w.Header())
		}
	}
	w = admin.do("GET", "/api/v1/admin/access-codes?status=bad", nil, headers)
	if w.Code != 400 || w.Header().Get("X-Trace-ID") != trace {
		t.Fatalf("failure trace: %d %+v", w.Code, w.Header())
	}
}

func TestAccountCodeHistoryOwnershipSnapshotAndSynchronization(t *testing.T) {
	s, admin := newInstalledServer(t)
	setAccessPolicy(t, s, false, true)
	ctx := t.Context()
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "history@code.test", "password": "Passw0rd", "quota": 0}, 200)
	uid := int(u["id"].(float64))
	client := &testClient{t: t, router: s.Router()}
	mustJSON(t, client, "POST", "/api/v1/auth/login", map[string]any{"email": "history@code.test", "password": "Passw0rd"}, 200)
	p := mustJSON(t, admin, "POST", "/api/v1/admin/account-plans", map[string]any{"name": "原套餐", "enabled": true, "grant": accountplan.Grant{Quota: 123}}, 200)
	pid := int(p["id"].(float64))
	raw := issueCode(t, admin, "redeem", pid)
	mustJSON(t, client, "POST", "/api/v1/me/redeem", map[string]any{"code": raw}, 200)
	row := s.Store.Ent.AccessCode.Query().Where(accesscode.DigestEQ(digest(raw))).OnlyX(ctx)
	mustJSON(t, admin, "PUT", "/api/v1/admin/account-plans/"+itoa(pid), map[string]any{"name": "改名", "enabled": false, "revision": 1, "grant": accountplan.Grant{Quota: 999}}, 200)
	setAccessPolicy(t, s, false, false)
	read := func() map[string]any {
		v := mustJSON(t, client, "GET", "/api/v1/me/redemptions?user_id=1", nil, 200)
		if v["total"].(float64) != 1 {
			t.Fatalf("history count: %+v", v)
		}
		item := v["items"].([]any)[0].(map[string]any)
		if item["grant"].(map[string]any)["quota"].(float64) != 123 || item["plan_revision"].(float64) != 1 {
			t.Fatal("historical snapshot changed")
		}
		data, _ := json.Marshal(v)
		for _, secret := range []string{raw, digest(raw), "trace_id", "used_by"} {
			if strings.Contains(string(data), secret) {
				t.Fatalf("history leaks %q", secret)
			}
		}
		return item
	}
	if read()["synchronization"] != "ok" {
		t.Fatal("completed sync missing")
	}
	s.Store.Ent.Audit.Delete().Where(audit.ActionEQ("account.grant.sync")).ExecX(ctx)
	if read()["synchronization"] != "unknown" {
		t.Fatal("missing proof was reported as completed")
	}
	s.Store.Ent.Resource.Create().SetKind(accountGrantSyncKind).SetKey(itoa(row.ID)).SetOwnerID(uid).SetData(map[string]any{}).SaveX(ctx)
	if read()["synchronization"] != "pending" {
		t.Fatal("pending marker ignored")
	}
	s.Store.Ent.Resource.Delete().Where(resource.KindEQ(accountGrantSyncKind)).ExecX(ctx)
	if v := mustJSON(t, admin, "GET", "/api/v1/me/redemptions?user_id="+itoa(uid), nil, 200); v["total"].(float64) != 0 {
		t.Fatal("cross-user history leak")
	}
	mustJSON(t, client, "GET", "/api/v1/admin/access-codes", nil, 403)
	anon := &testClient{t: t, router: s.Router()}
	mustJSON(t, anon, "GET", "/api/v1/me/redemptions", nil, 401)
}

func TestAccountCodeFiltersAndPagination(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	plan := s.Store.Ent.AccountPlan.Create().SetName("筛选").SetGrant(accountplan.Grant{}).SaveX(ctx)
	now := time.Now()
	for i := 0; i < 54; i++ {
		b := s.Store.Ent.AccessCode.Create().SetKind(accesscode.KindRedeem).SetDigest(digest(itoa(i))).SetPlanID(plan.ID).SetGrant(accountplan.Grant{}).SetCreatedBy(1)
		if i == 51 {
			b.SetExpiresAt(now.Add(-time.Hour))
		}
		if i == 52 {
			b.SetRevoked(true).SetExpiresAt(now.Add(-time.Hour))
		}
		if i == 53 {
			b.SetUsedAt(now).SetUsedBy(1).SetRevoked(true)
		}
		b.SaveX(ctx)
	}
	for status, want := range map[string]int{"unused": 51, "expired": 1, "revoked": 1, "used": 1} {
		v := mustJSON(t, admin, "GET", "/api/v1/admin/access-codes?kind=redeem&plan_id="+itoa(plan.ID)+"&status="+status, nil, 200)
		if int(v["total"].(float64)) != want {
			t.Fatalf("%s total: %+v", status, v)
		}
		for _, item := range v["items"].([]any) {
			if item.(map[string]any)["status"] != status {
				t.Fatal("status display disagrees with filter")
			}
		}
	}
	v := mustJSON(t, admin, "GET", "/api/v1/admin/access-codes?status=unused&page=2", nil, 200)
	if len(v["items"].([]any)) != 1 {
		t.Fatal("pagination is inconsistent")
	}
	for _, q := range []string{"kind=bad", "status=bad", "plan_id=-1", "page=oops", "page=1000001"} {
		mustJSON(t, admin, "GET", "/api/v1/admin/access-codes?"+q, nil, 400)
	}
}
