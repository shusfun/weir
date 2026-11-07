package store

import "testing"

func TestProbeCredentialIncrementalMigration(t *testing.T) {
	for name, open := range backends(t) {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			ctx := t.Context()
			legacy := s.Ent.Credential.Create().SetNodeID(1).SetUserID(3).SetLinkID(7).SetStatsKey("legacy-probe-migration").SetPassword("isolated-existing-secret").SetMeterUp(42).SaveX(ctx)
			if _, err := s.DB.ExecContext(ctx, "ALTER TABLE credentials DROP COLUMN probe_parent_id"); err != nil {
				t.Fatal(err)
			}
			if err := s.Ent.Schema.Create(ctx); err != nil {
				t.Fatal(err)
			}
			got := s.Ent.Credential.GetX(ctx, legacy.ID)
			if got.ProbeParentID != nil || got.Password != legacy.Password || got.MeterUp != 42 || got.UserID == nil || *got.UserID != 3 {
				t.Fatal("migration changed existing access or counters")
			}
			probe := s.Ent.Credential.Create().SetNodeID(1).SetUserID(3).SetLinkID(7).SetStatsKey("child-probe-migration").SetProbeParentID(legacy.ID).SaveX(ctx)
			if err := s.Ent.Schema.Create(ctx); err != nil {
				t.Fatal(err)
			}
			got = s.Ent.Credential.GetX(ctx, probe.ID)
			if got.ProbeParentID == nil || *got.ProbeParentID != legacy.ID {
				t.Fatal("probe marker did not survive repeated migration")
			}
		})
	}
}
