package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Preserve legacy zero quotas before the schema generator adds the new default.
// Column creation and backfill commit together, so a restart cannot reclassify
// accounts created with an explicit zero allowance.
func migrateUserQuota(ctx context.Context, db *sql.DB, driver string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists, migrated bool
	if driver == DriverPostgres {
		if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(7195202401)"); err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='weir' AND table_name='users'), EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='weir' AND table_name='users' AND column_name='unlimited')").Scan(&exists, &migrated)
	} else {
		err = tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type='table' AND name='users'), EXISTS (SELECT 1 FROM pragma_table_info('users') WHERE name='unlimited')").Scan(&exists, &migrated)
	}
	if err != nil {
		return err
	}
	if exists && !migrated {
		if _, err = tx.ExecContext(ctx, "ALTER TABLE users ADD COLUMN unlimited boolean NOT NULL DEFAULT false"); err != nil {
			return fmt.Errorf("add user allowance flag: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE users SET unlimited=true WHERE quota=0"); err != nil {
			return fmt.Errorf("preserve user allowance: %w", err)
		}
	}
	return tx.Commit()
}
