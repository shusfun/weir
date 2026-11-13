package store

import (
	"context"
	"testing"
)

func TestMigrationRecoveryAndVersionGuard(t *testing.T) {
	for name, open := range backends(t) {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			if s.Driver == DriverPostgres {
				lock := s.Lock.(pgLocker)
				lock.db.SetMaxOpenConns(1)
			}
			ctx := t.Context()
			u := s.Ent.User.Create().SetEmail("migration@example.test").SetPasswordHash("unchanged").SetQuota(1234).SaveX(ctx)
			// An interrupted idempotent stage resumes through Atlas on startup.
			if _, err := s.DB.ExecContext(ctx, `UPDATE schema_migrations SET status='running',stage='workers'`); err != nil {
				t.Fatal(err)
			}
			if err := s.migrate(ctx, ""); err != nil {
				t.Fatal(err)
			}
			if err := s.migrate(ctx, ""); err != nil {
				t.Fatal(err)
			}
			got := s.Ent.User.GetX(ctx, u.ID)
			if got.PasswordHash != u.PasswordHash || got.Quota != u.Quota {
				t.Fatal("migration changed account data")
			}
			var status string
			if err := s.DB.QueryRowContext(ctx, `SELECT status FROM schema_migrations WHERE version=$1`, schemaVersion).Scan(&status); err != nil || status != "applied" {
				t.Fatalf("status %s %v", status, err)
			}
			if _, err := s.DB.ExecContext(ctx, `UPDATE schema_migrations SET version='9999999999'`); err != nil {
				t.Fatal(err)
			}
			if err := s.migrate(context.Background(), ""); err == nil {
				t.Fatal("older binary accepted newer database")
			}
		})
	}
}
