package center

import (
	"encoding/json"
	"testing"

	"github.com/shusfun/weir/internal/accountplan"
)

func TestAccountPlanDraftNeedsApprovalAndRejectsStale(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	p := mustJSON(t, admin, "POST", "/api/v1/admin/account-plans", map[string]any{"name": "初始", "enabled": true, "grant": accountplan.Grant{Quota: 1024}}, 200)
	id := int(p["id"].(float64))
	raw := issueCode(t, admin, "invite", id)
	text, _ := json.Marshal(accountPlanDocument{Name: "修改", Enabled: true, Grant: accountplan.Grant{Quota: 2048}})
	steps := []DraftStep{{Action: "account.plan.update", Args: map[string]any{"plan_id": float64(id), "text": string(text)}}}
	d, err := s.submitActionDraft(ctx, "dsh", "调整后续发放套餐", steps)
	if err != nil {
		t.Fatal(err)
	}
	if s.Store.Ent.AccountPlan.GetX(ctx, id).Grant.Quota != 1024 || len(d.Steps[0].Diff) == 0 {
		t.Fatal("draft applied early or omitted diff")
	}
	if _, ok := findCap("analysis", "account.plan.update"); ok {
		t.Fatal("automatic safety actor may modify plans")
	}
	if c, ok := findCap("observation", "account.plan.update"); !ok || c.Kind != "draft" {
		t.Fatal("observation must submit approval draft")
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d.ID+"/apply", nil, 200)
	if p := s.Store.Ent.AccountPlan.GetX(ctx, id); p.Grant.Quota != 2048 || p.Revision != 2 {
		t.Fatal("approved plan not applied", p)
	}
	if code := s.Store.Ent.AccessCode.Query().OnlyX(ctx); code.Digest != digest(raw) || code.Grant.Quota != 1024 {
		t.Fatal("draft changed issued code")
	}
	text, _ = json.Marshal(accountPlanDocument{Name: "第三版", Enabled: true, Grant: accountplan.Grant{Quota: 3072}})
	d, err = s.submitActionDraft(ctx, "dsh", "下一版套餐", []DraftStep{{Action: "account.plan.update", Args: map[string]any{"plan_id": float64(id), "text": string(text)}}})
	if err != nil {
		t.Fatal(err)
	}
	mustJSON(t, admin, "PUT", "/api/v1/admin/account-plans/"+itoa(id), map[string]any{"name": "管理员修改", "enabled": false, "revision": 2, "grant": accountplan.Grant{Quota: 4096}}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d.ID+"/apply", nil, 409)
	if s.Store.Ent.AccountPlan.GetX(ctx, id).Grant.Quota != 4096 {
		t.Fatal("stale draft overwrote plan")
	}
}
