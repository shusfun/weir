package center

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/shusfun/weir/internal/store"
)

// fakeOnePanel 按 1Panel 的布局写一份 1pctl 与应用 .env（值带引号，与真实文件一致）。
func fakeOnePanel(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctl := filepath.Join(base, "1pctl")
	write(ctl, "#!/bin/bash\nBASE_DIR="+base+"\nORIGINAL_PORT=27230\n")
	write(filepath.Join(base, "1panel/apps/postgresql/postgresql/.env"),
		"CONTAINER_NAME='1Panel-postgresql-x'\nPANEL_APP_PORT_HTTP=5432\nPANEL_DB_ROOT_USER='user_abc'\nPANEL_DB_ROOT_PASSWORD='pg pass=1'\n")
	write(filepath.Join(base, "1panel/apps/redis/redis/.env"),
		"HOST_IP='127.0.0.1'\nPANEL_APP_PORT_HTTP=6379\nPANEL_REDIS_ROOT_PASSWORD=\"redis'pw\"\n")
	oldCtl, oldBase := onePanelCtl, onePanelDefault
	onePanelCtl, onePanelDefault = ctl, "/nonexistent"
	t.Cleanup(func() { onePanelCtl, onePanelDefault = oldCtl, oldBase })
}

func TestOnePanelApps(t *testing.T) {
	fakeOnePanel(t)
	pg := onePanelApps("postgresql")
	if len(pg) != 1 || pg[0] != (onePanelApp{Port: 5432, User: "user_abc", Password: "pg pass=1"}) {
		t.Fatalf("postgresql = %+v", pg)
	}
	rd := onePanelApps("redis")
	if len(rd) != 1 || rd[0] != (onePanelApp{Port: 6379, Password: "redis'pw"}) {
		t.Fatalf("redis = %+v", rd)
	}
}

func TestFillDetectedSecretsOnlyForMatchingLocalInstance(t *testing.T) {
	fakeOnePanel(t)
	base := store.Config{Driver: store.DriverPostgres,
		Postgres: store.PostgresConfig{Host: "127.0.0.1", Port: 5432, User: "user_abc", Database: "gate"},
		Redis:    store.RedisConfig{Address: "127.0.0.1:6379"}}
	got := fillDetectedSecrets(base)
	if got.Postgres.Password != "pg pass=1" || got.Redis.Password != "redis'pw" {
		t.Fatalf("local instance not filled: %+v", got)
	}
	for name, mut := range map[string]func(*store.Config){
		"remote host":   func(c *store.Config) { c.Postgres.Host = "10.0.0.5"; c.Redis.Address = "10.0.0.5:6379" },
		"other port":    func(c *store.Config) { c.Postgres.Port = 5433; c.Redis.Address = "127.0.0.1:6380" },
		"other account": func(c *store.Config) { c.Postgres.User = "someone"; c.Redis.Username = "acl" },
	} {
		c := base
		mut(&c)
		if got := fillDetectedSecrets(c); got.Postgres.Password != "" || got.Redis.Password != "" {
			t.Fatalf("%s must not be filled: %+v", name, got)
		}
	}
	typed := base
	typed.Postgres.Password, typed.Redis.Password = "typed", "typed"
	if got := fillDetectedSecrets(typed); got.Postgres.Password != "typed" || got.Redis.Password != "typed" {
		t.Fatalf("typed password must win: %+v", got)
	}
}

func TestRecommendDatabaseSkipsExistingData(t *testing.T) {
	dbs := []store.PostgresDatabase{{Name: "gate", HasData: true}, {Name: "misub"}, {Name: "postgres"}, {Name: "user_abc"}}
	if got := recommendDatabase(dbs, "user_abc"); got != "user_abc" {
		t.Fatalf("recommend = %q", got)
	}
	if got := recommendDatabase([]store.PostgresDatabase{{Name: "postgres", Unknown: true}}, "u"); got != "" {
		t.Fatalf("unknown must not be recommended: %q", got)
	}
}

// 目标存储里已有账户（例如重装时没清掉旧库）：测试连接与安装都拒绝，提示恢复原配置或换空库，而不是笼统的连接失败。
func TestSetupRejectsStoreWithExistingAccounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "center.json")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	token, err := Initialize(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, err = LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "old.db")
	old, err := store.Open(context.Background(), store.Config{Driver: store.DriverSQLite, SQLitePath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Ent.User.Create().SetEmail("old@example.test").SetPasswordHash("x").Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	old.Close()
	s, err := New(cfg, path, fstest.MapFS{"index.html": {Data: []byte("<!doctype html>console")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	tc := &testClient{t: t, router: s.Router()}
	auth := map[string]string{"Authorization": "Bearer " + token}
	st := map[string]any{"driver": "sqlite", "sqlite_path": dbPath}
	w := tc.do("POST", "/api/v1/setup/test-store", st, auth)
	if w.Code != 409 || decode(t, w)["error"].(map[string]any)["code"] != "existing_installation_restore_config" {
		t.Fatalf("test-store = %d %s", w.Code, w.Body.String())
	}
	w = tc.do("POST", "/api/v1/setup/install", map[string]any{"store": st, "email": "admin@example.test", "password": "Passw0rd"}, auth)
	if w.Code != 409 || decode(t, w)["error"].(map[string]any)["code"] != "existing_installation_restore_config" {
		t.Fatalf("install = %d %s", w.Code, w.Body.String())
	}
	if reloaded, _ := LoadConfig(path); reloaded.Installed {
		t.Fatal("rejected install must not be saved")
	}
	if w := tc.do("POST", "/api/v1/setup/detect-store", nil, nil); w.Code != 401 {
		t.Fatalf("detect-store without token = %d", w.Code)
	}
	if w := tc.do("POST", "/api/v1/setup/detect-store", nil, auth); w.Code != 200 {
		t.Fatalf("detect-store = %d %s", w.Code, w.Body.String())
	}
}
