package center

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
)

// testStore 默认使用 SQLite；MIGATE_CENTER_STORE=postgres 时改用 deploy/test.compose.yml 的 PostgreSQL + Redis：
// 每个测试新建独立数据库并在结束时删除，Redis 测试前后清空 migate: 前缀（一次性测试实例）。
func testStore(t *testing.T, dir string) map[string]any {
	t.Helper()
	if os.Getenv("MIGATE_CENTER_STORE") != "postgres" {
		return map[string]any{"driver": "sqlite", "sqlite_path": filepath.Join(dir, "migate.db")}
	}
	admin, err := sql.Open("pgx", "postgres://gate:local-test-only@127.0.0.1:25432/gate?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("migate_center_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Username: "migate", Password: "local-test-only"})
	flush := func() {
		ctx := context.Background()
		iter := rdb.Scan(ctx, 0, "migate:*", 500).Iterator()
		for iter.Next(ctx) {
			rdb.Del(ctx, iter.Val())
		}
	}
	flush()
	// 注册在服务器关闭之前执行（t.Cleanup 后进先出）：先关服务，再删库。
	t.Cleanup(func() {
		flush()
		rdb.Close()
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		admin.Close()
	})
	return map[string]any{"driver": "postgres",
		"postgres": map[string]any{"host": "127.0.0.1", "port": 25432, "user": "gate", "password": "local-test-only", "database": name, "ssl_mode": "disable"},
		"redis":    map[string]any{"address": "127.0.0.1:26379", "username": "migate", "password": "local-test-only"}}
}
