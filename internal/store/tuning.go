package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PoolConfig bounds connections owned by one pool, independently of server limits.
// Zero values retain the installed defaults; changes take effect on restart.
type PoolConfig struct {
	MaxOpen         int `json:"max_open,omitempty"`
	MaxIdle         int `json:"max_idle,omitempty"`
	LifetimeSeconds int `json:"lifetime_seconds,omitempty"`
	IdleSeconds     int `json:"idle_seconds,omitempty"`
}

type MaintenanceConfig struct {
	KVIntervalSeconds int `json:"kv_interval_seconds,omitempty"`
	KVBatch           int `json:"kv_batch,omitempty"`
	RecordBatch       int `json:"record_batch,omitempty"`
}

func (c PoolConfig) validate() error {
	if c.MaxOpen < 0 || c.MaxOpen > 256 || c.MaxIdle < 0 || c.MaxIdle > 256 || c.LifetimeSeconds < 0 || c.LifetimeSeconds > 86400 || c.IdleSeconds < 0 || c.IdleSeconds > 86400 {
		return errors.New("invalid database pool limits")
	}
	if c.MaxOpen > 0 && c.MaxIdle > c.MaxOpen {
		return errors.New("idle connections exceed open connections")
	}
	return nil
}

func (c PoolConfig) Effective() PoolConfig {
	if c.MaxOpen == 0 {
		c.MaxOpen = 8
	}
	if c.MaxIdle == 0 {
		c.MaxIdle = 2
	}
	if c.MaxIdle > c.MaxOpen {
		c.MaxIdle = c.MaxOpen
	}
	if c.LifetimeSeconds == 0 {
		c.LifetimeSeconds = 1800
	}
	return c
}
func (c MaintenanceConfig) Effective() MaintenanceConfig {
	if c.KVIntervalSeconds == 0 {
		c.KVIntervalSeconds = 60
	}
	if c.KVBatch == 0 {
		c.KVBatch = 500
	}
	if c.RecordBatch == 0 {
		c.RecordBatch = 500
	}
	return c
}

func (c PoolConfig) apply(db *sql.DB) {
	c = c.Effective()
	db.SetMaxOpenConns(c.MaxOpen)
	db.SetMaxIdleConns(c.MaxIdle)
	db.SetConnMaxLifetime(time.Duration(c.LifetimeSeconds) * time.Second)
	db.SetConnMaxIdleTime(time.Duration(c.IdleSeconds) * time.Second)
}

// RedisTuning projects initialized client options without endpoint or credentials.
func (s *Store) RedisTuning() *RedisConfig {
	if s.redis == nil {
		return nil
	}
	o := s.redis.Options()
	return &RedisConfig{PoolSize: o.PoolSize, MinIdleConns: o.MinIdleConns, DialTimeoutMS: int(o.DialTimeout / time.Millisecond), ReadTimeoutMS: int(o.ReadTimeout / time.Millisecond), WriteTimeoutMS: int(o.WriteTimeout / time.Millisecond), PoolTimeoutMS: int(o.PoolTimeout / time.Millisecond)}
}

// Maintain removes only expired ephemeral records. It never evicts live sessions.
func (s *Store) Maintain(ctx context.Context) error {
	if kv, ok := s.KV.(*sqliteKV); ok {
		return kv.purge(ctx)
	}
	return nil
}
