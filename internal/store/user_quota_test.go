package store

import (
	"testing"
)

func TestUserQuotaMigration(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *Store{"sqlite": openSQLiteForTest, "postgres": openPostgresForTest} {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			ctx := t.Context()
			zero := s.Ent.User.Create().SetEmail("zero@quota.test").SetPasswordHash("unused").SaveX(ctx)
			limited := s.Ent.User.Create().SetEmail("limited@quota.test").SetPasswordHash("unused").SetQuota(1024).SaveX(ctx)
			if zero.Unlimited || limited.Unlimited {
				t.Fatal("new accounts must have a bounded allowance")
			}
			if _, err := s.DB.ExecContext(ctx, "ALTER TABLE users DROP COLUMN unlimited"); err != nil {
				t.Fatal(err)
			}
			if err := migrateUserQuota(ctx, s.DB, s.Driver); err != nil {
				t.Fatal(err)
			}
			if !s.Ent.User.GetX(ctx, zero.ID).Unlimited || s.Ent.User.GetX(ctx, limited.ID).Unlimited {
				t.Fatal("migration changed legacy allowance")
			}
			zero.Update().SetUnlimited(false).ExecX(ctx)
			fresh := s.Ent.User.Create().SetEmail("fresh@quota.test").SetPasswordHash("unused").SaveX(ctx)
			if err := migrateUserQuota(ctx, s.DB, s.Driver); err != nil {
				t.Fatal(err)
			}
			if s.Ent.User.GetX(ctx, zero.ID).Unlimited || s.Ent.User.GetX(ctx, fresh.ID).Unlimited {
				t.Fatal("repeated migration granted new zero allowances unlimited access")
			}
		})
	}
}
