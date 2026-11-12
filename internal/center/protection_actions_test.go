package center

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/policydata"
	"github.com/shusfun/weir/internal/store/ent/setting"
)

func TestAutomaticProtectionRequiresBoundRunAndExpires(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	doc := policydata.Empty()
	doc.Site["enabled"] = json.RawMessage(`true`)
	s.Store.Ent.Setting.Create().SetKey(protectionPolicyKey).SetValue(toMap(doc)).SaveX(ctx)
	event := &SecurityEvent{ID: 77, SourceIP: "203.0.113.7", Status: "analyzing", CreatedAt: time.Now().UTC(), TraceID: randomHex(16)}
	if err := s.saveEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	steps := []DraftStep{{Action: "defense.block", Args: map[string]any{"event_id": float64(event.ID), "minutes": float64(60)}}}
	d, err := s.submitActionDraft(ctx, "analysis", "验证无任务授权只产生草稿", steps)
	if err != nil || d.Status != "passed" {
		t.Fatal(d, err)
	}
	event.Status = "analyzing"
	if err := s.saveEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	grant := securityRunGrant{RunID: randomHex(16), EventID: event.ID, TraceID: event.TraceID, Until: time.Now().Add(time.Minute)}
	bound := context.WithValue(withTrace(ctx, event.TraceID), securityRunKey{}, grant)
	d, err = s.submitActionDraft(bound, "analysis", "仅当前事件的安全收紧", steps)
	if err != nil || d.Status != "auto_applied" {
		t.Fatal(d, err)
	}
	actions, err := s.protectionActions(ctx)
	if err != nil || len(actions) != 1 || actions[0].Until.Sub(actions[0].CreatedAt) > 24*time.Hour || actions[0].TraceID != event.TraceID {
		t.Fatal(actions, err)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/defense/automatic/"+d.ID+"/revoke", nil, 204)
	actions, _ = s.protectionActions(ctx)
	if actions[0].RevokedAt == nil {
		t.Fatal("revocation not persisted")
	}
	// A repeat after revocation and disabling automation returns a receipt;
	// it must neither reactivate the rule nor produce a new approval draft.
	doc.Site["enabled"] = json.RawMessage(`false`)
	if err = s.Store.Ent.Setting.Update().Where(setting.KeyEQ(protectionPolicyKey)).SetValue(toMap(doc)).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	replayed, err := s.submitActionDraft(bound, "analysis", "重复提交不能恢复已撤销处置", steps)
	if err != nil || replayed.ID != d.ID || replayed.Status != "auto_receipt" {
		t.Fatalf("replay: %+v %v", replayed, err)
	}
	actions, err = s.protectionActions(ctx)
	if err != nil || len(actions) != 1 || actions[0].RevokedAt == nil {
		t.Fatalf("replay wrote policy: %+v %v", actions, err)
	}
	// Self configuration cannot enter the automatic whitelist even when it
	// arrives with the same task's authority.
	malicious := &Draft{ID: randomToken(12), Source: "analysis", Steps: []DraftStep{{Action: "ai.profile.update"}}}
	if applied, err := s.tryAutomaticProtection(bound, malicious); applied || err != nil {
		t.Fatal("self modification became automatic", err)
	}
}

func TestProtectionActionRevisionIgnoresDeliveryButDetectsRevocation(t *testing.T) {
	a := protectionAction{ID: "action", Kind: "block", IP: "203.0.113.7", Until: time.Now().Add(time.Hour), Notification: "pending"}
	revision := protectionActionRevision(a)
	a.Notification = "failed"
	a.NotificationAttempts = 3
	next := time.Now().Add(time.Minute)
	a.NotificationNext = &next
	if got := protectionActionRevision(a); got != revision {
		t.Fatal("delivery changed approval version")
	}
	a.RevokedAt = &next
	if got := protectionActionRevision(a); got == revision {
		t.Fatal("revocation did not change approval version")
	}
}
