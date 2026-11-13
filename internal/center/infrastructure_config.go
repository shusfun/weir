package center

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// SubscriptionInfrastructure bounds disposable work independently of account state.
// Zero values select the documented defaults; changes require a restart.
type SubscriptionInfrastructure struct {
	FetchConcurrency int `json:"fetch_concurrency,omitempty"`
	RawCacheHours    int `json:"raw_cache_hours,omitempty"`
	CacheItemBytes   int `json:"cache_item_bytes,omitempty"`
	CacheTotalBytes  int `json:"cache_total_bytes,omitempty"`
	CacheEntries     int `json:"cache_entries,omitempty"`
}

func (c SubscriptionInfrastructure) effective() SubscriptionInfrastructure {
	if c.FetchConcurrency == 0 {
		c.FetchConcurrency = 4
	}
	if c.RawCacheHours == 0 {
		c.RawCacheHours = 336
	}
	if c.CacheItemBytes == 0 {
		c.CacheItemBytes = 10 << 20
	}
	if c.CacheTotalBytes == 0 {
		c.CacheTotalBytes = 256 << 20
	}
	if c.CacheEntries == 0 {
		c.CacheEntries = 2048
	}
	return c
}
func (c SubscriptionInfrastructure) validate() error {
	if c.FetchConcurrency < 0 || c.FetchConcurrency > 30 || c.RawCacheHours < 0 || c.RawCacheHours > 720 || c.CacheItemBytes < 0 || c.CacheItemBytes > 32<<20 || c.CacheItemBytes > 0 && c.CacheItemBytes < 1024 {
		return errors.New("invalid subscription infrastructure limits")
	}
	if c.CacheTotalBytes < 0 || c.CacheTotalBytes > 1<<30 || c.CacheTotalBytes > 0 && c.CacheTotalBytes < 1<<20 || c.CacheEntries < 0 || c.CacheEntries > 100000 || c.effective().CacheItemBytes > c.effective().CacheTotalBytes {
		return errors.New("invalid subscription cache budget")
	}
	return nil
}
func (c *Config) infrastructureEnvironment() map[string]*int {
	return map[string]*int{
		"WEIR_PG_MAX_OPEN":              &c.Store.Postgres.Pool.MaxOpen,
		"WEIR_PG_MAX_IDLE":              &c.Store.Postgres.Pool.MaxIdle,
		"WEIR_PG_LOCK_MAX_OPEN":         &c.Store.Postgres.LockPool.MaxOpen,
		"WEIR_PG_LOCK_MAX_IDLE":         &c.Store.Postgres.LockPool.MaxIdle,
		"WEIR_PG_LOCK_LIFETIME_SECONDS": &c.Store.Postgres.LockPool.LifetimeSeconds,
		"WEIR_PG_LOCK_IDLE_SECONDS":     &c.Store.Postgres.LockPool.IdleSeconds,
		"WEIR_PG_LIFETIME_SECONDS":      &c.Store.Postgres.Pool.LifetimeSeconds,
		"WEIR_PG_IDLE_SECONDS":          &c.Store.Postgres.Pool.IdleSeconds,
		"WEIR_PG_STATEMENT_TIMEOUT_MS":  &c.Store.Postgres.StatementTimeoutMS,
		"WEIR_PG_LOCK_TIMEOUT_MS":       &c.Store.Postgres.LockTimeoutMS,
		"WEIR_REDIS_POOL_SIZE":          &c.Store.Redis.PoolSize,
		"WEIR_REDIS_MIN_IDLE":           &c.Store.Redis.MinIdleConns,
		"WEIR_REDIS_DIAL_TIMEOUT_MS":    &c.Store.Redis.DialTimeoutMS,
		"WEIR_REDIS_READ_TIMEOUT_MS":    &c.Store.Redis.ReadTimeoutMS,
		"WEIR_REDIS_WRITE_TIMEOUT_MS":   &c.Store.Redis.WriteTimeoutMS,
		"WEIR_REDIS_POOL_TIMEOUT_MS":    &c.Store.Redis.PoolTimeoutMS,
		"WEIR_KV_MAINTENANCE_SECONDS":   &c.Store.Maintenance.KVIntervalSeconds,
		"WEIR_KV_MAINTENANCE_BATCH":     &c.Store.Maintenance.KVBatch,
		"WEIR_RECORD_MAINTENANCE_BATCH": &c.Store.Maintenance.RecordBatch,
		"WEIR_SUB_FETCH_CONCURRENCY":    &c.Subscriptions.FetchConcurrency,
		"WEIR_SUB_RAW_CACHE_HOURS":      &c.Subscriptions.RawCacheHours,
		"WEIR_SUB_CACHE_ITEM_BYTES":     &c.Subscriptions.CacheItemBytes,
		"WEIR_SUB_CACHE_TOTAL_BYTES":    &c.Subscriptions.CacheTotalBytes,
		"WEIR_SUB_CACHE_ENTRIES":        &c.Subscriptions.CacheEntries,
	}
}

type infrastructureOverride struct{ file, effective int }

func (c *Config) applyInfrastructureEnvironment() error {
	c.environment = make(map[string]infrastructureOverride)
	for name, target := range c.infrastructureEnvironment() {
		if value, ok := os.LookupEnv(name); ok {
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid integer for %s", name)
			}
			c.environment[name] = infrastructureOverride{file: *target, effective: n}
			*target = n
		}
	}
	return nil
}

type poolStatus struct {
	Open           int     `json:"open"`
	InUse          int     `json:"in_use"`
	Idle           int     `json:"idle"`
	WaitCount      int64   `json:"wait_count"`
	WaitDurationMS float64 `json:"wait_duration_ms"`
	MaxOpen        int     `json:"max_open"`
}

func (s *Server) infrastructureStatus(c *gin.Context) {
	// Only non-secret tuning is projected. Connection addresses and credentials
	// are deliberately absent, even for an administrator.
	cfg := s.Config
	sources := map[string]string{}
	for name, value := range cfg.infrastructureEnvironment() {
		if _, ok := cfg.environment[name]; ok {
			sources[name] = "environment"
		} else if *value != 0 {
			sources[name] = "configuration file"
		} else {
			sources[name] = "default"
		}
	}
	c.Header("Cache-Control", "no-store")
	stats := s.Store.DB.Stats()
	dbStatus := poolStatus{Open: stats.OpenConnections, InUse: stats.InUse, Idle: stats.Idle, WaitCount: stats.WaitCount, WaitDurationMS: float64(stats.WaitDuration) / float64(time.Millisecond), MaxOpen: stats.MaxOpenConnections}
	var pending any = "unknown"
	if next, err := LoadConfig(s.Path); err == nil {
		pending = !reflect.DeepEqual(cfg.Store, next.Store) || cfg.Subscriptions != next.Subscriptions
	}
	budget := 0
	if s.Store.Driver == "postgres" {
		budget = cfg.Store.Postgres.Pool.Effective().MaxOpen + cfg.Store.Postgres.LockPool.Effective().MaxOpen
	}
	c.JSON(200, gin.H{"effective": gin.H{"postgres_pool": cfg.Store.Postgres.Pool.Effective(), "postgres_lock_pool": cfg.Store.Postgres.LockPool.Effective(), "postgres_connection_budget": budget, "postgres_statement_timeout_ms": cfg.Store.Postgres.StatementTimeoutMS, "postgres_lock_timeout_ms": cfg.Store.Postgres.LockTimeoutMS, "redis": s.Store.RedisTuning(), "maintenance": cfg.Store.Maintenance.Effective(), "subscriptions": cfg.Subscriptions.effective()}, "sources": sources, "activation": "restart", "pending_restart": pending, "database": dbStatus, "redis_deployment": s.Store.RedisRequirements(c.Request.Context()), "sampled_at": time.Now().UTC()})
}
