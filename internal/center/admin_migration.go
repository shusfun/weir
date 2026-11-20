package center

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/proxygroupversion"
	"github.com/shusfun/weir/internal/store/ent/user"
)

func migrateAdministrator(ctx context.Context, st *store.Store) (err error) {
	ctx = withTrace(ctx, randomHex(16))
	finish := operationStage(ctx, "user.admin.migrate")
	defer func() { finish(err) }()
	return st.Lock.Do(ctx, "schema-migration", func(ctx context.Context) error {
		tx, err := st.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		admins, err := tx.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Order(ent.Asc(user.FieldCreatedAt, user.FieldID)).All(ctx)
		if err != nil {
			return fmt.Errorf("read administrators: %w", err)
		}
		if len(admins) == 0 {
			return errors.New("installed store has no administrator; restore the configuration before starting")
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		primary := admins[0]
		if !primary.Enabled {
			if err := tx.User.UpdateOneID(primary.ID).SetEnabled(true).SetSessionsValidAfter(now).Exec(ctx); err != nil {
				return err
			}
			if err := auditAdministratorMigration(ctx, tx, primary.ID, "enabled"); err != nil {
				return err
			}
		}
		for _, account := range admins[1:] {
			if err := tx.User.UpdateOneID(account.ID).SetRole(user.RoleUser).SetSessionsValidAfter(now).Exec(ctx); err != nil {
				return err
			}
			if err := auditAdministratorMigration(ctx, tx, account.ID, "demoted"); err != nil {
				return err
			}
		}
		seeded, err := tx.ProxyGroupVersion.Query().Where(proxygroupversion.OwnerIDEQ(primary.ID)).Exist(ctx)
		if err != nil {
			return err
		}
		if !seeded {
			for _, group := range loadPolicy(primary).Groups {
				if err := writeGroupVersion(ctx, tx, primary.ID, group, false); err != nil {
					return err
				}
			}
		}
		return tx.Commit()
	})
}

func auditAdministratorMigration(ctx context.Context, tx *ent.Tx, uid int, result string) error {
	return tx.Audit.Create().SetAction("user.admin.migrate").SetTarget("user:" + itoa(uid)).SetResult(result).
		SetDetail(map[string]any{"trace_id": taskTrace(ctx)}).Exec(ctx)
}
