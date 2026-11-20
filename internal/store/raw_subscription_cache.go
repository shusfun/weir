package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
)

// subscription_cache_state 由数据库触发器维护：依赖表的任何写入都在同一事务里递增 revision，
// 包括导入与后台任务，业务代码不必逐处记得失效缓存。触发器、单行 CHECK 约束和
// 「递增并返回新值」（UPDATE ... RETURNING）都无法用 Ent 表达，所以这张表的读写集中在这里。

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

// markSubscriptionCacheDirty 在写 KV 之前留下持久标记；写入中断时，下一次读版本会先把它隔离掉。
func (s *Store) markSubscriptionCacheDirty(ctx context.Context) error {
	return s.execCacheState(ctx, `UPDATE subscription_cache_state SET dirty = 1 WHERE id = 1`)
}

// publishSubscriptionCache 在 KV 写入完成后递增版本并清除标记。
func (s *Store) publishSubscriptionCache(ctx context.Context) error {
	return s.execCacheState(ctx, `UPDATE subscription_cache_state SET revision = revision + 1, dirty = 0 WHERE id = 1`)
}

// LockSubscriptionRevision fences imports against every dependency writer,
// including writers which do not hold the subscription profile application lock.
// The dependency triggers update this same row in their transaction, so the
// no-op UPDATE takes the row lock inside tx and returns the current revision.
func LockSubscriptionRevision(ctx context.Context, tx *ent.Tx) (string, error) {
	rows, err := tx.QueryContext(ctx, "UPDATE subscription_cache_state SET revision = revision WHERE id = 1 AND dirty = 0 RETURNING revision")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return "", err
		}
		return "", errors.New("subscription_data_busy")
	}
	var revision int64
	if err = rows.Scan(&revision); err != nil {
		return "", err
	}
	return strconv.FormatInt(revision, 10), rows.Err()
}
