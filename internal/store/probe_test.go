package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHasDataSQLite(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "migate.db")}
	if has, err := HasData(ctx, cfg); err != nil || has {
		t.Fatalf("missing file: has=%v err=%v", has, err)
	}
	s, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// 只有表、没有账户（上次安装中途失败）可以继续安装
	if has, err := HasData(ctx, cfg); err != nil || has {
		t.Fatalf("empty schema: has=%v err=%v", has, err)
	}
	if _, err = s.Ent.User.Create().SetEmail("a@b.c").SetPasswordHash("x").Save(ctx); err != nil {
		t.Fatal(err)
	}
	if has, err := HasData(ctx, cfg); err != nil || !has {
		t.Fatalf("with account: has=%v err=%v", has, err)
	}
}

// 复现生产环境：旧版本在共享库里留下列不全、已有账户的 migate.users，探测要标出来，HasData 要拒绝。
func TestProbePostgresMarksLegacyData(t *testing.T) {
	if os.Getenv("MIGATE_INTEGRATION") != "1" {
		t.Skip("set MIGATE_INTEGRATION=1 with deploy/test.compose.yml running")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", "postgres://gate:local-test-only@127.0.0.1:25432/gate?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	suffix := time.Now().UnixNano()
	legacy, empty := fmt.Sprintf("migate_probe_legacy_%d", suffix), fmt.Sprintf("migate_probe_empty_%d", suffix)
	for _, name := range []string{legacy, empty} {
		if _, err = admin.Exec("CREATE DATABASE " + name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)") })
	}
	p := PostgresConfig{Host: "127.0.0.1", Port: 25432, User: "gate", Password: "local-test-only", Database: legacy}
	db, err := sql.Open("pgx", postgresDSN(p))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"CREATE SCHEMA migate",
		"CREATE TABLE migate.users (id bigserial PRIMARY KEY, email text, role text, created_at timestamptz)",
		"INSERT INTO migate.users (email, role, created_at) VALUES ('admin', 'admin', now())",
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	cfg := Config{Driver: DriverPostgres, Postgres: p, Redis: RedisConfig{Address: "127.0.0.1:26379", Username: "migate", Password: "local-test-only"}}
	if has, err := HasData(ctx, cfg); err != nil || !has {
		t.Fatalf("legacy: has=%v err=%v", has, err)
	}
	cfg.Postgres.Database = empty
	if has, err := HasData(ctx, cfg); err != nil || has {
		t.Fatalf("empty: has=%v err=%v", has, err)
	}
	version, dbs, err := ProbePostgres(ctx, PostgresConfig{Host: "127.0.0.1", Port: 25432, User: "gate", Password: "local-test-only", Database: "postgres"})
	if err != nil || version == "" {
		t.Fatalf("probe: %q %v", version, err)
	}
	got := map[string]PostgresDatabase{}
	for _, d := range dbs {
		got[d.Name] = d
	}
	if d, ok := got[legacy]; !ok || !d.HasData || d.Unknown {
		t.Fatalf("legacy db = %+v (%v)", d, ok)
	}
	if d, ok := got[empty]; !ok || d.HasData || d.Unknown {
		t.Fatalf("empty db = %+v (%v)", d, ok)
	}
	if status, _ := ProbeRedis(ctx, cfg.Redis); status != RedisOK {
		t.Fatalf("redis = %s", status)
	}
	// 测试实例的 default 用户不要密码，无法在这里覆盖 NOAUTH；只设 requirepass 的实例（1Panel）在部署时实测。
	if status, _ := ProbeRedis(ctx, RedisConfig{Address: cfg.Redis.Address, Username: "migate", Password: "wrong"}); status != RedisWrongPassword {
		t.Fatalf("redis wrong password = %s", status)
	}
	if status, _ := ProbeRedis(ctx, RedisConfig{Address: "127.0.0.1:1"}); status != RedisUnreachable {
		t.Fatalf("redis closed port = %s", status)
	}
}
