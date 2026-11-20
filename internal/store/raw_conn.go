package store

import (
	"context"
	"database/sql"
	"path/filepath"
)

// 连接与 schema 的建立发生在 Ent 客户端存在之前，只能直接使用 database/sql。

// openSQLiteDB 打开单机数据库：外键、WAL 与忙等待都在连接参数里设定，每个新连接都生效。
func openSQLiteDB(path string) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)"
	return sql.Open("sqlite", dsn)
}

// openPostgresProbe 打开只用于连通性检查与只读探测的连接，不设置 search_path。
func openPostgresProbe(p PostgresConfig) (*sql.DB, error) {
	return sql.Open("pgx", postgresDSN(p))
}

// createWeirSchema 只建立独立的 weir schema，不触碰共享实例中的其他数据。
func createWeirSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS weir")
	return err
}
