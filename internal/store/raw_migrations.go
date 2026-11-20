package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// schema_migrations 记录迁移阶段，必须在 Atlas 运行之前存在并可读写（它记录的正是 Atlas 那一步），
// 不能交给 Ent schema 管理。

func (s *Store) appliedSchema(ctx context.Context) (version, checksum string, err error) {
	if _, err = s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, checksum TEXT NOT NULL, stage TEXT NOT NULL, status TEXT NOT NULL, updated_at BIGINT NOT NULL)`); err != nil {
		return "", "", err
	}
	err = s.DB.QueryRowContext(ctx, `SELECT version,checksum FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version, &checksum)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return version, checksum, err
}

func (s *Store) recordSchemaStage(ctx context.Context, stage, status string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO schema_migrations(version,checksum,stage,status,updated_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(version) DO UPDATE SET stage=excluded.stage,status=excluded.status,updated_at=excluded.updated_at`, schemaVersion, schemaChecksum(), stage, status, time.Now().UnixMilli())
	return err
}
