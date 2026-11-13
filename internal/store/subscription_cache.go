package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const subscriptionCacheLock = "subscription-cache-state"

// Dependency changes and their cache version commit in the same database
// transaction, including mutations made by imports and background workers.
func (s *Store) migrateSubscriptionCache(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS subscription_cache_state (id INTEGER PRIMARY KEY CHECK (id = 1), revision BIGINT NOT NULL, dirty INTEGER NOT NULL DEFAULT 0)`,
		fmt.Sprintf(`INSERT INTO subscription_cache_state(id, revision) VALUES(1, %d) ON CONFLICT(id) DO NOTHING`, time.Now().UnixNano()),
		`CREATE TABLE IF NOT EXISTS subscription_cache_entries (key TEXT PRIMARY KEY, bytes BIGINT NOT NULL, expires_at BIGINT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS subscription_cache_entries_expiry ON subscription_cache_entries(expires_at)`,
	}
	if s.Driver == DriverPostgres {
		statements = append(statements, `CREATE OR REPLACE FUNCTION weir.subscription_cache_changed() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN UPDATE weir.subscription_cache_state SET revision = revision + 1 WHERE id = 1; RETURN NULL; END $$`)
	}
	dependencies := []struct{ table, columns string }{
		{"sub_sources", "name,url,enabled,user_agent,filter_include,filter_exclude,operators,plus_as_space,node_cache,exclude_traffic,position"},
		{"sub_nodes", ""},
		{"sub_profiles", "name,enabled,all_sources,source_ids,node_ids,members,operators,settings,expires_at,position"},
		{"rule_templates", ""},
		{"settings", ""},
	}
	for _, dependency := range dependencies {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			name := "subscription_cache_" + dependency.table + "_" + strings.ToLower(event)
			operation := event
			if event == "UPDATE" && dependency.columns != "" {
				operation += " OF " + dependency.columns
			}
			if s.Driver == DriverPostgres {
				statements = append(statements, "DROP TRIGGER IF EXISTS "+name+" ON "+dependency.table,
					"CREATE TRIGGER "+name+" AFTER "+operation+" ON "+dependency.table+" FOR EACH STATEMENT EXECUTE FUNCTION weir.subscription_cache_changed()")
			} else {
				statements = append(statements, "CREATE TRIGGER IF NOT EXISTS "+name+" AFTER "+operation+" ON "+dependency.table+" BEGIN UPDATE subscription_cache_state SET revision = revision + 1 WHERE id = 1; END")
			}
		}
	}
	for _, statement := range statements {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SubscriptionCacheVersion(ctx context.Context) (string, error) {
	var revision int64
	var dirty int
	read := func() error {
		started := time.Now()
		defer recordQuery(ctx, started)
		return s.DB.QueryRowContext(ctx, `SELECT revision, dirty FROM subscription_cache_state WHERE id = 1`).Scan(&revision, &dirty)
	}
	err := read()
	if err == nil && dirty != 0 {
		err = s.Lock.Do(ctx, subscriptionCacheLock, func(ctx context.Context) error {
			if err := read(); err != nil {
				return err
			}
			if dirty == 0 {
				return nil
			}
			// Holding the same lock as writers proves that an interrupted writer
			// can no longer publish. Fence any Redis write whose completion was lost.
			started := time.Now()
			defer recordQuery(ctx, started)
			return s.DB.QueryRowContext(ctx, `UPDATE subscription_cache_state SET revision = revision + 1, dirty = 0 WHERE id = 1 RETURNING revision`).Scan(&revision)
		})
	}
	return strconv.FormatInt(revision, 10), err
}

func (s *Store) InvalidateSubscriptionCache(ctx context.Context) error {
	return s.execCacheState(ctx, `UPDATE subscription_cache_state SET revision = revision + 1 WHERE id = 1`)
}

func (s *Store) execCacheState(ctx context.Context, query string) error {
	started := time.Now()
	defer recordQuery(ctx, started)
	_, err := s.DB.ExecContext(ctx, query)
	return err
}

// WriteSubscriptionCache bridges the database and disposable KV without a
// distributed transaction. An interrupted write leaves a durable dirty marker;
// the next version read fences it before allowing a rendered-cache lookup.
func (s *Store) WriteSubscriptionCache(ctx context.Context, key, value string, ttl time.Duration) error {
	return s.WriteSubscriptionCacheBounded(ctx, key, value, ttl, SubscriptionCacheBudget{Bytes: 256 << 20, Entries: 2048})
}

type SubscriptionCacheBudget struct{ Bytes, Entries int }

var ErrSubscriptionCacheCapacity = errors.New("subscription cache budget exhausted")

// Admission metadata bounds this product's disposable cache payloads without
// changing Redis's global policy or evicting sessions and one-time credentials.
// It reserves before the KV boundary and retains uncertain reservations on error.
func (s *Store) reserveSubscriptionCache(ctx context.Context, key, value string, ttl time.Duration, budget SubscriptionCacheBudget) error {
	if budget.Bytes < 1 || budget.Entries < 1 || ttl <= 0 || len(value) > budget.Bytes {
		return ErrSubscriptionCacheCapacity
	}
	var count, bytes int64
	query := `SELECT COUNT(*),COALESCE(SUM(bytes),0) FROM subscription_cache_entries WHERE expires_at>$1 AND key<>$2`
	if s.Driver == DriverSQLite {
		query = `SELECT COUNT(*),COALESCE(SUM(bytes),0) FROM subscription_cache_entries WHERE expires_at>? AND key<>?`
	}
	if err := s.DB.QueryRowContext(ctx, query, time.Now().UnixMilli(), key).Scan(&count, &bytes); err != nil {
		return err
	}
	if count >= int64(budget.Entries) || bytes+int64(len(value)) > int64(budget.Bytes) {
		return ErrSubscriptionCacheCapacity
	}
	query = `INSERT INTO subscription_cache_entries(key,bytes,expires_at) VALUES($1,$2,$3) ON CONFLICT(key) DO UPDATE SET bytes=excluded.bytes,expires_at=excluded.expires_at`
	if s.Driver == DriverSQLite {
		query = `INSERT INTO subscription_cache_entries(key,bytes,expires_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET bytes=excluded.bytes,expires_at=excluded.expires_at`
	}
	_, err := s.DB.ExecContext(ctx, query, key, len(value), time.Now().Add(ttl).UnixMilli())
	return err
}

func (s *Store) WriteSubscriptionCacheBounded(ctx context.Context, key, value string, ttl time.Duration, budget SubscriptionCacheBudget) error {
	return s.Lock.Do(ctx, subscriptionCacheLock, func(ctx context.Context) error {
		if err := s.reserveSubscriptionCache(ctx, key, value, ttl, budget); err != nil {
			return err
		}
		old, exists, err := s.KV.Get(ctx, key)
		if err != nil {
			return err
		}
		if exists && old == value {
			return s.KV.Set(ctx, key, value, ttl)
		}
		if err = s.execCacheState(ctx, `UPDATE subscription_cache_state SET dirty = 1 WHERE id = 1`); err != nil {
			return err
		}
		if err = s.KV.Set(ctx, key, value, ttl); err != nil {
			return err
		}
		return s.execCacheState(ctx, `UPDATE subscription_cache_state SET revision = revision + 1, dirty = 0 WHERE id = 1`)
	})
}

func (s *Store) WriteSubscriptionOutputCache(ctx context.Context, key, value string, ttl time.Duration, budget SubscriptionCacheBudget) error {
	return s.Lock.Do(ctx, subscriptionCacheLock, func(ctx context.Context) error {
		if err := s.reserveSubscriptionCache(ctx, key, value, ttl, budget); err != nil {
			return err
		}
		return s.KV.Set(ctx, key, value, ttl)
	})
}

func (s *Store) PruneSubscriptionCacheReservations(ctx context.Context, batch int) error {
	if batch < 1 || batch > 10000 {
		return errors.New("invalid cache maintenance batch")
	}
	query := `DELETE FROM subscription_cache_entries WHERE key IN (SELECT key FROM subscription_cache_entries WHERE expires_at<=$1 ORDER BY expires_at LIMIT $2)`
	if s.Driver == DriverSQLite {
		query = `DELETE FROM subscription_cache_entries WHERE key IN (SELECT key FROM subscription_cache_entries WHERE expires_at<=? ORDER BY expires_at LIMIT ?)`
	}
	_, err := s.DB.ExecContext(ctx, query, time.Now().UnixMilli(), batch)
	return err
}
