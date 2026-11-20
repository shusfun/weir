package store

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/redis/go-redis/v9"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/kv"
	"github.com/shusfun/weir/internal/store/ent/predicate"
)

// KV 是会话、限速、一次性凭证等短期数据的存储。ttl 为 0 表示不过期。
// 键由调用方给出（不含前缀）；Redis 实现统一加 weir: 前缀，只触及本项目的键。
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
	// IncrBy 原子加 delta；键首次出现时设置 ttl。用于按天累计的流量。
	IncrBy(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
	// Keys 列出指定前缀的键（不含 weir: 前缀），最多 limit 个；超出返回 ErrTooMany。
	Keys(ctx context.Context, prefix string, limit int) ([]string, error)
	Ping(ctx context.Context) error
}

var ErrTooMany = errors.New("kv: too many keys")

const redisPrefix = "weir:"

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

// incrByWithTTL 只用 get / set / pttl / expire（受限的 ACL 用户也有这些命令），在脚本里原子地完成：
// 键已存在时保留剩余时间，首次出现时设置 ttl。
var incrByWithTTL = redis.NewScript(`local k=KEYS[1];local n=tonumber(redis.call('GET',k) or '0')+tonumber(ARGV[1]);local t=redis.call('PTTL',k);if t>0 then redis.call('SET',k,n,'PX',t) else redis.call('SET',k,n);if t==-2 and tonumber(ARGV[2])>0 then redis.call('EXPIRE',k,ARGV[2]) end end;return n`)

func (r redisKV) IncrBy(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	seconds := int64(0)
	if ttl > 0 {
		seconds = int64((ttl + time.Second - 1) / time.Second)
	}
	return incrByWithTTL.Run(ctx, r.c, []string{redisPrefix + key}, delta, seconds).Int64()
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
// 过期在读取时判断，后台维护按批清理。条件写入与计数在 raw_kv.go。
type sqliteKV struct {
	ent       *ent.Client
	db        *sql.DB
	mu        sync.Mutex
	lastPurge time.Time
	interval  time.Duration
	batch     int
}

func expiry(ttl time.Duration) int64 {
	if ttl <= 0 {
		return 0
	}
	return time.Now().Add(ttl).UnixMilli()
}

// live 是未过期的键：不过期，或到期时间晚于现在。
func live() predicate.KV {
	return kv.Or(kv.ExpiresAt(0), kv.ExpiresAtGT(time.Now().UnixMilli()))
}

func (s *sqliteKV) Get(ctx context.Context, key string) (string, bool, error) {
	row, err := s.ent.KV.Query().Where(kv.ID(key), live()).Only(ctx)
	if ent.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return row.Value, true, nil
}

func (s *sqliteKV) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return s.ent.KV.Create().SetID(key).SetValue(value).SetExpiresAt(expiry(ttl)).
		OnConflictColumns(kv.FieldID).UpdateNewValues().Exec(ctx)
}

// GetDel 在一个事务里读出并删除；SQLite 只有一个写连接，读写之间不会插入其他写入。
func (s *sqliteKV) GetDel(ctx context.Context, key string) (value string, found bool, err error) {
	tx, err := s.ent.Tx(ctx)
	if err != nil {
		return "", false, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	row, err := tx.KV.Query().Where(kv.ID(key), live()).Only(ctx)
	if ent.IsNotFound(err) {
		return "", false, tx.Commit()
	}
	if err != nil {
		return "", false, err
	}
	if _, err = tx.KV.Delete().Where(kv.ID(key)).Exec(ctx); err != nil {
		return "", false, err
	}
	return row.Value, true, tx.Commit()
}

func (s *sqliteKV) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := s.ent.KV.Delete().Where(kv.IDIn(keys...)).Exec(ctx)
	return err
}

func (s *sqliteKV) Keys(ctx context.Context, prefix string, limit int) ([]string, error) {
	out, err := s.ent.KV.Query().Where(kv.IDGTE(prefix), kv.IDLT(prefix+"\U0010ffff"), live()).
		Order(kv.ByID()).Limit(limit + 1).IDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(out) > limit {
		return nil, ErrTooMany
	}
	return out, nil
}

func (s *sqliteKV) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *sqliteKV) purge(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	interval, batch := s.interval, s.batch
	if interval == 0 {
		interval = time.Minute
	}
	if batch == 0 {
		batch = 500
	}
	if time.Since(s.lastPurge) < interval {
		return nil
	}
	now := time.Now().UnixMilli()
	_, err := s.ent.KV.Delete().Where(func(sel *entsql.Selector) {
		d := entsql.Dialect(sel.Dialect())
		r := d.Table(kv.Table).As("r")
		expired := d.Select(r.C(kv.FieldID)).From(r).
			Where(entsql.And(entsql.GT(r.C(kv.FieldExpiresAt), 0), entsql.LTE(r.C(kv.FieldExpiresAt), now))).
			OrderBy(r.C(kv.FieldExpiresAt)).Limit(batch)
		sel.Where(entsql.In(sel.C(kv.FieldID), expired))
	}).Exec(ctx)
	if err == nil {
		s.lastPurge = time.Now()
	}
	return err
}
