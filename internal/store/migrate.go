package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	atlas "ariga.io/atlas/sql/schema"
	entschema "entgo.io/ent/dialect/sql/schema"

	"github.com/shusfun/weir/internal/lockfile"
)

// Atlas remains responsible for schema planning and application. This metadata
// joins its startup entry with the existing data backfill and raw schemas;
// it is not a downgrade authorization or a replacement SQL migration engine.
const schemaVersion = "2026112001"

//go:embed ent/migrate/schema.go raw_user_quota.go raw_subscription_cache.go raw_worker_schema.go exit_binding_migration.go
var schemaInputs embed.FS

func schemaChecksum() string {
	h := sha256.New()
	for _, path := range []string{"ent/migrate/schema.go", "raw_user_quota.go", "raw_subscription_cache.go", "raw_worker_schema.go", "exit_binding_migration.go"} {
		b, _ := schemaInputs.ReadFile(path)
		h.Write([]byte(path))
		h.Write([]byte(strings.ReplaceAll(string(b), "\r\n", "\n")))
	}
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
		version, checksum, err := s.appliedSchema(ctx)
		if err != nil {
			return err
		}
		if version > schemaVersion || version == schemaVersion && checksum != schemaChecksum() {
			return errors.New("database migration version or checksum is incompatible with this binary")
		}
		options := []entschema.MigrateOption{entschema.WithDiffHook(keepWorkerIdemConstraint)}
		if s.Driver == DriverSQLite {
			options = append(options, entschema.WithDiffHook(ignoreSQLitePrimaryKeyNull))
		}
		stages := []struct {
			name string
			run  func(context.Context) error
		}{
			{"user-allowance", func(ctx context.Context) error { return migrateUserQuota(ctx, s.DB, s.Driver) }},
			{"ent-atlas", func(ctx context.Context) error { return s.Ent.Schema.Create(ctx, options...) }},
			{"kv", func(ctx context.Context) error {
				if s.Driver != DriverSQLite {
					return nil
				}
				kv, err := newSQLiteKV(ctx, s.Ent, s.DB)
				if err == nil {
					s.KV = kv
				}
				return err
			}},
			{"workers", func(ctx context.Context) error { return migrateWorkerTables(ctx, s.DB) }},
			{"subscription-cache", s.migrateSubscriptionCache},
			{"exit-bindings", s.migrateExitBindings},
		}
		for _, stage := range stages {
			if err = s.recordSchemaStage(ctx, stage.name, "running"); err != nil {
				return err
			}
			if err = stage.run(ctx); err != nil {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				recordErr := s.recordSchemaStage(cleanup, stage.name, "failed")
				cancel()
				return errors.Join(fmt.Errorf("migrate %s: %w", stage.name, err), recordErr)
			}
		}
		return s.recordSchemaStage(ctx, "complete", "applied")
	})
}

func keepWorkerIdemConstraint(next entschema.Differ) entschema.Differ {
	return entschema.DiffFunc(func(current, desired *atlas.Schema) ([]atlas.Change, error) {
		changes, err := next.Diff(current, desired)
		table, ok := current.Table("worker_tasks")
		if err != nil || !ok {
			return changes, err
		}
		equivalent := func(index *atlas.Index) bool {
			return index.Unique && len(index.Parts) == 1 && index.Parts[0].C != nil && index.Parts[0].C.Name == "idem"
		}
		var existing map[string]bool
		for _, index := range table.Indexes {
			if equivalent(index) && (index.Name == "worker_tasks_idem_key" || index.Name == "sqlite_autoindex_worker_tasks_2") {
				if existing == nil {
					existing = make(map[string]bool)
				}
				existing[index.Name] = true
			}
		}
		if len(existing) == 0 {
			return changes, nil
		}
		kept := changes[:0]
		for _, change := range changes {
			if modify, ok := change.(*atlas.ModifyTable); ok && modify.T.Name == table.Name {
				inner := modify.Changes[:0]
				for _, item := range modify.Changes {
					switch index := item.(type) {
					case *atlas.AddIndex:
						if equivalent(index.I) {
							continue
						}
					case *atlas.DropIndex:
						if existing[index.I.Name] {
							continue
						}
					}
					inner = append(inner, item)
				}
				modify.Changes = inner
				if len(inner) == 0 {
					continue
				}
			}
			kept = append(kept, change)
		}
		return kept, nil
	})
}

// ignoreSQLitePrimaryKeyNull 丢弃 SQLite 主键列「可空 → 非空」的差异。SQLite 允许非整数主键列为 NULL，
// 早期手写 DDL 建的 TEXT / BIGINT 主键因此被 Atlas 读成可空；Atlas 要修正它只能重建整张表。
// 主键列实际上从未写入 NULL，这个差异不需要迁移。Ent 自己建的主键都带 NOT NULL，不受影响。
func ignoreSQLitePrimaryKeyNull(next entschema.Differ) entschema.Differ {
	return entschema.DiffFunc(func(current, desired *atlas.Schema) ([]atlas.Change, error) {
		changes, err := next.Diff(current, desired)
		if err != nil {
			return nil, err
		}
		kept := changes[:0]
		for _, change := range changes {
			if modify, ok := change.(*atlas.ModifyTable); ok {
				inner := modify.Changes[:0]
				for _, c := range modify.Changes {
					if column, ok := c.(*atlas.ModifyColumn); ok && column.Change == atlas.ChangeNull && primaryKeyColumn(current, modify.T.Name, column.From.Name) {
						continue
					}
					inner = append(inner, c)
				}
				modify.Changes = inner
				if len(inner) == 0 {
					continue
				}
			}
			kept = append(kept, change)
		}
		return kept, nil
	})
}

func primaryKeyColumn(s *atlas.Schema, table, column string) bool {
	t, ok := s.Table(table)
	if !ok || t.PrimaryKey == nil {
		return false
	}
	for _, part := range t.PrimaryKey.Parts {
		if part.C != nil && part.C.Name == column {
			return true
		}
	}
	return false
}
