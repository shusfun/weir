package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/lockfile"
	"github.com/shusfun/weir/internal/taskqueue"
)

// Atlas remains responsible for schema planning and application. This metadata
// joins its startup entry with the existing data backfill and worker schemas;
// it is not a downgrade authorization or a replacement SQL migration engine.
const schemaVersion = "2026111301"

//go:embed ent/migrate/schema.go user_quota.go subscription_cache.go
var schemaInputs embed.FS

func schemaChecksum() string {
	h := sha256.New()
	for _, path := range []string{"ent/migrate/schema.go", "user_quota.go", "subscription_cache.go"} {
		b, _ := schemaInputs.ReadFile(path)
		h.Write([]byte(path))
		h.Write([]byte(strings.ReplaceAll(string(b), "\r\n", "\n")))
	}
	h.Write([]byte(taskqueue.MigrationChecksum()))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Store) migrate(ctx context.Context, sqlitePath string) error {
	if sqlitePath != "" {
		release, err := lockfile.TryAcquire(sqlitePath + ".migration.lock")
		if err != nil {
			return fmt.Errorf("schema migration lock: %w", err)
		}
		defer release()
	}
	return s.Lock.Do(ctx, "schema-migration", func(ctx context.Context) error {
		if _, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, checksum TEXT NOT NULL, stage TEXT NOT NULL, status TEXT NOT NULL, updated_at BIGINT NOT NULL)`); err != nil {
			return err
		}
		var version, checksum string
		err := s.DB.QueryRowContext(ctx, `SELECT version,checksum FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version, &checksum)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if version > schemaVersion || version == schemaVersion && checksum != schemaChecksum() {
			return errors.New("database migration version or checksum is incompatible with this binary")
		}
		stages := []struct {
			name string
			run  func(context.Context) error
		}{
			{"user-allowance", func(ctx context.Context) error { return migrateUserQuota(ctx, s.DB, s.Driver) }},
			{"ent-atlas", func(ctx context.Context) error { return s.Ent.Schema.Create(ctx) }},
			{"kv", func(ctx context.Context) error {
				if s.Driver != DriverSQLite {
					return nil
				}
				kv, err := newSQLiteKV(ctx, s.DB)
				if err == nil {
					s.KV = kv
				}
				return err
			}},
			{"workers", s.Tasks.MigrateWithHostLock},
			{"subscription-cache", s.migrateSubscriptionCache},
		}
		record := func(ctx context.Context, stage, status string) error {
			_, err := s.DB.ExecContext(ctx, `INSERT INTO schema_migrations(version,checksum,stage,status,updated_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(version) DO UPDATE SET stage=excluded.stage,status=excluded.status,updated_at=excluded.updated_at`, schemaVersion, schemaChecksum(), stage, status, time.Now().UnixMilli())
			return err
		}
		for _, stage := range stages {
			if err = record(ctx, stage.name, "running"); err != nil {
				return err
			}
			if err = stage.run(ctx); err != nil {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				recordErr := record(cleanup, stage.name, "failed")
				cancel()
				return errors.Join(fmt.Errorf("migrate %s: %w", stage.name, err), recordErr)
			}
		}
		return record(ctx, "complete", "applied")
	})
}
