package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
)

// kv 表只在 SQLite 模式使用。过期索引带谓词，Atlas 不管理这张表（Ent schema 标记跳过迁移）。
func newSQLiteKV(ctx context.Context, client *ent.Client, db *sql.DB) (*sqliteKV, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT NOT NULL, expires_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS kv_expires ON kv(expires_at) WHERE expires_at > 0`); err != nil {
		return nil, err
	}
	return &sqliteKV{ent: client, db: db}, nil
}

// SetNX 是条件 CAS：只有键不存在或已过期时才写入，并且要知道是否写成。Ent 的 upsert 不返回受影响行数，
// 无法区分「写入」与「条件不满足」，所以直接执行带 WHERE 的 ON CONFLICT。
func (s *sqliteKV) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO kv(key, value, expires_at) VALUES(?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, expires_at = excluded.expires_at WHERE kv.expires_at > 0 AND kv.expires_at <= ?`, key, value, expiry(ttl), time.Now().UnixMilli())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Incr 与 IncrBy 在一条语句里按「是否已过期」分支改写值和到期时间并返回新值（CASE + RETURNING），
// Ent 的 upsert 不能表达条件赋值，也不能返回更新后的值。
// 键不存在或已过期时从初值开始并设置 ttl；否则累加并保持原到期时间。
func (s *sqliteKV) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO kv(key, value, expires_at) VALUES(?, '1', ?)
ON CONFLICT(key) DO UPDATE SET
  value = CASE WHEN kv.expires_at > 0 AND kv.expires_at <= ? THEN '1' ELSE CAST(CAST(kv.value AS INTEGER) + 1 AS TEXT) END,
  expires_at = CASE WHEN kv.expires_at > 0 AND kv.expires_at <= ? THEN excluded.expires_at ELSE kv.expires_at END
RETURNING CAST(value AS INTEGER)`, key, expiry(ttl), time.Now().UnixMilli(), time.Now().UnixMilli()).Scan(&n)
	return n, err
}

func (s *sqliteKV) IncrBy(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	var n int64
	now := time.Now().UnixMilli()
	err := s.db.QueryRowContext(ctx, `INSERT INTO kv(key, value, expires_at) VALUES(?, CAST(? AS TEXT), ?)
ON CONFLICT(key) DO UPDATE SET
  value = CASE WHEN kv.expires_at > 0 AND kv.expires_at <= ? THEN excluded.value ELSE CAST(CAST(kv.value AS INTEGER) + ? AS TEXT) END,
  expires_at = CASE WHEN kv.expires_at > 0 AND kv.expires_at <= ? THEN excluded.expires_at ELSE kv.expires_at END
RETURNING CAST(value AS INTEGER)`, key, delta, expiry(ttl), now, delta, now).Scan(&n)
	return n, err
}
