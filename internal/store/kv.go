package store

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// KV 是会话、限速、一次性凭证等短期数据的存储。ttl 为 0 表示不过期。
// 键由调用方给出（不含前缀）；Redis 实现统一加 migate: 前缀，只触及本项目的键。
type KV interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	// SetNX 仅在键不存在（或已过期）时写入，返回是否写入成功。
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	// GetDel 原子地读取并删除，用于一次性凭证。
	GetDel(ctx context.Context, key string) (string, bool, error)
	Del(ctx context.Context, keys ...string) error
	// Incr 原子加一；键首次出现时设置 ttl。用于限速计数。
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
	// Keys 列出指定前缀的键（不含 migate: 前缀），最多 limit 个；超出返回 ErrTooMany。
	Keys(ctx context.Context, prefix string, limit int) ([]string, error)
	Ping(ctx context.Context) error
}

var ErrTooMany = errors.New("kv: too many keys")

const redisPrefix = "migate:"

func redisTLS() *tls.Config { return &tls.Config{MinVersion: tls.VersionTLS12} }

type redisKV struct{ c *redis.Client }

func (r redisKV) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := r.c.Get(ctx, redisPrefix+key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (r redisKV) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.c.Set(ctx, redisPrefix+key, value, ttl).Err()
}

func (r redisKV) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return r.c.SetNX(ctx, redisPrefix+key, value, ttl).Result()
}

func (r redisKV) GetDel(ctx context.Context, key string) (string, bool, error) {
	v, err := r.c.GetDel(ctx, redisPrefix+key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (r redisKV) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = redisPrefix + k
	}
	return r.c.Del(ctx, full...).Err()
}

// 只用专用 ACL 允许的命令（INCR / EXPIRE），过期时间按秒向上取整。
var incrWithTTL = redis.NewScript(`local n=redis.call('INCR',KEYS[1]);if n==1 and tonumber(ARGV[1])>0 then redis.call('EXPIRE',KEYS[1],ARGV[1]) end;return n`)

func (r redisKV) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	seconds := int64(0)
	if ttl > 0 {
		seconds = int64((ttl + time.Second - 1) / time.Second)
	}
	return incrWithTTL.Run(ctx, r.c, []string{redisPrefix + key}, seconds).Int64()
}

func (r redisKV) Keys(ctx context.Context, prefix string, limit int) ([]string, error) {
	var out []string
	iter := r.c.Scan(ctx, 0, redisPrefix+escapeGlob(prefix)+"*", 200).Iterator()
	for iter.Next(ctx) {
		if len(out) >= limit {
			return nil, ErrTooMany
		}
		out = append(out, strings.TrimPrefix(iter.Val(), redisPrefix))
	}
	return out, iter.Err()
}

func (r redisKV) Ping(ctx context.Context) error { return r.c.Ping(ctx).Err() }

func escapeGlob(s string) string {
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`, "]", `\]`).Replace(s)
}

// sqliteKV 是单机模式的内置缓存：数据库里一张 kv 表，重启不丢会话。
// 过期在读取时判断，并在写入时顺带清理。
type sqliteKV struct{ db *sql.DB }

func newSQLiteKV(ctx context.Context, db *sql.DB) (*sqliteKV, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT NOT NULL, expires_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS kv_expires ON kv(expires_at) WHERE expires_at > 0`); err != nil {
		return nil, err
	}
	return &sqliteKV{db}, nil
}

func expiry(ttl time.Duration) int64 {
	if ttl <= 0 {
		return 0
	}
	return time.Now().Add(ttl).UnixMilli()
}

const liveRow = `(expires_at = 0 OR expires_at > ?)`

func (s *sqliteKV) Get(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ? AND `+liveRow, key, time.Now().UnixMilli()).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *sqliteKV) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO kv(key, value, expires_at) VALUES(?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, expires_at = excluded.expires_at`, key, value, expiry(ttl))
	s.purge(ctx)
	return err
}

func (s *sqliteKV) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	// 已过期的旧值视为不存在，可被覆盖。
	res, err := s.db.ExecContext(ctx, `INSERT INTO kv(key, value, expires_at) VALUES(?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, expires_at = excluded.expires_at WHERE kv.expires_at > 0 AND kv.expires_at <= ?`, key, value, expiry(ttl), time.Now().UnixMilli())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *sqliteKV) GetDel(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `DELETE FROM kv WHERE key = ? AND `+liveRow+` RETURNING value`, key, time.Now().UnixMilli()).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *sqliteKV) Del(ctx context.Context, keys ...string) error {
	for _, k := range keys {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM kv WHERE key = ?`, k); err != nil {
			return err
		}
	}
	return nil
}

func (s *sqliteKV) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	// 键不存在或已过期时从 1 开始并设置 ttl；否则加一并保持原到期时间。
	var n int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO kv(key, value, expires_at) VALUES(?, '1', ?)
ON CONFLICT(key) DO UPDATE SET
  value = CASE WHEN kv.expires_at > 0 AND kv.expires_at <= ? THEN '1' ELSE CAST(CAST(kv.value AS INTEGER) + 1 AS TEXT) END,
  expires_at = CASE WHEN kv.expires_at > 0 AND kv.expires_at <= ? THEN excluded.expires_at ELSE kv.expires_at END
RETURNING CAST(value AS INTEGER)`, key, expiry(ttl), time.Now().UnixMilli(), time.Now().UnixMilli()).Scan(&n)
	return n, err
}

func (s *sqliteKV) Keys(ctx context.Context, prefix string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key FROM kv WHERE substr(key, 1, ?) = ? AND `+liveRow+` ORDER BY key LIMIT ?`, len(prefix), prefix, time.Now().UnixMilli(), limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err = rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	if len(out) > limit {
		return nil, ErrTooMany
	}
	return out, rows.Err()
}

func (s *sqliteKV) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *sqliteKV) purge(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM kv WHERE expires_at > 0 AND expires_at <= ?`, time.Now().UnixMilli())
}
