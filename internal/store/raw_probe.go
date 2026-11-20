package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// 初始化向导在建表之前探测目标存储：读取版本、数据库列表和系统目录，
// 这些都是数据库自身的目录查询，不属于 Weir 的表，没有 Ent 模型。

// PostgresDatabase 是探测到的一个数据库：HasData 表示其中已有 Weir 账户，不能在上面重新初始化。
type PostgresDatabase struct {
	Name    string `json:"name"`
	HasData bool   `json:"has_data"`
	Unknown bool   `json:"unknown,omitempty"`
}

// ProbePostgres 用给定账号连上实例，读取版本并列出可连接的数据库及其中是否已有 Weir 账户。只读。
func ProbePostgres(ctx context.Context, p PostgresConfig) (string, []PostgresDatabase, error) {
	db, err := openPostgresProbe(p)
	if err != nil {
		return "", nil, err
	}
	defer db.Close()
	var version string
	if err = db.QueryRowContext(ctx, "SHOW server_version").Scan(&version); err != nil {
		return "", nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT datname FROM pg_database
		WHERE NOT datistemplate AND datallowconn AND has_database_privilege(datname, 'CONNECT')
		ORDER BY datname LIMIT 50`)
	if err != nil {
		return version, nil, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return version, nil, err
		}
		names = append(names, name)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return version, nil, err
	}
	out := make([]PostgresDatabase, 0, len(names))
	for _, name := range names {
		entry := PostgresDatabase{Name: name}
		q := p
		q.Database = name
		one, cancel := context.WithTimeout(ctx, 3*time.Second)
		if dbn, err := openPostgresProbe(q); err != nil {
			entry.Unknown = true
		} else {
			entry.HasData, err = postgresHasAccounts(one, dbn)
			entry.Unknown = err != nil
			dbn.Close()
		}
		cancel()
		out = append(out, entry)
	}
	return version, out, nil
}

// HasData 报告目标存储里是否已有 Weir 账户；只读，不建表。
// 只有表结构、没有账户（上次安装中途失败）不算已有数据，可以继续安装。
func HasData(ctx context.Context, c Config) (bool, error) {
	if err := c.Validate(); err != nil {
		return false, err
	}
	if c.Driver == DriverSQLite {
		if _, err := os.Stat(c.SQLitePath); errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(c.SQLitePath)+"?mode=ro")
		if err != nil {
			return false, err
		}
		defer db.Close()
		var tables int
		if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'users'").Scan(&tables); err != nil || tables == 0 {
			return false, err
		}
		var exists bool
		err = db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM users)").Scan(&exists)
		return exists, err
	}
	db, err := openPostgresProbe(c.Postgres)
	if err != nil {
		return false, err
	}
	defer db.Close()
	return postgresHasAccounts(ctx, db)
}

func postgresHasAccounts(ctx context.Context, db *sql.DB) (bool, error) {
	var table bool
	if err := db.QueryRowContext(ctx, "SELECT to_regclass('weir.users') IS NOT NULL").Scan(&table); err != nil {
		return false, fmt.Errorf("%w: %w", ErrPostgresUnavailable, err)
	}
	if !table {
		return false, nil
	}
	var exists bool
	err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM weir.users)").Scan(&exists)
	return exists, err
}
