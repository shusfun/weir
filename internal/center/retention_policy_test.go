package center

import (
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/audit"
)

func TestRetentionPolicyValidationConflictAndAudit(t *testing.T) {
	s, admin := newInstalledServer(t)
	path := "/api/v1/admin/settings/retention"
	doc := mustJSON(t, admin, "GET", path, nil, 200)
	p := doc["policy"].(map[string]any)
	if p["audit_days"] != float64(0) {
		t.Fatal("audit deletion must be opt-in")
	}
	p["audit_days"] = 1
	mustJSON(t, admin, "PUT", path, doc, 422)
	p["audit_days"], p["observation_days"] = 30, 21
	saved := mustJSON(t, admin, "PUT", path, doc, 200)
	if saved["revision"] == doc["revision"] {
		t.Fatal("policy version did not advance")
	}
	mustJSON(t, admin, "PUT", path, doc, 409)
	current := mustJSON(t, admin, "GET", path, nil, 200)
	if current["revision"] != saved["revision"] {
		t.Fatal("conflicting save replaced policy")
	}
	if s.Store.Ent.Audit.Query().Where(audit.ActionEQ("settings.retention")).CountX(t.Context()) != 1 {
		t.Fatal("policy save must have one atomic audit record")
	}
}

func TestAuditRetentionDefaultPreservesAndPrunesBoundedBatch(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx, now := t.Context(), time.Now()
	s.Config.Store.Maintenance.RecordBatch = 2
	for i := 0; i < 5; i++ {
		s.Store.Ent.Audit.Create().SetAction("old-fixture").SetCreatedAt(now.Add(-40 * 24 * time.Hour)).SaveX(ctx)
	}
	p := defaultRetentionPolicy()
	if err := s.pruneAudit(ctx, now, p); err != nil {
		t.Fatal(err)
	}
	if n := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("old-fixture")).CountX(ctx); n != 5 {
		t.Fatal("default policy deleted audit history")
	}
	p.AuditDays = 30
	if err := s.pruneAudit(ctx, now, p); err != nil {
		t.Fatal(err)
	}
	if n := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("old-fixture")).CountX(ctx); n != 3 {
		t.Fatalf("cleanup did not honor batch: %d", n)
	}
}
