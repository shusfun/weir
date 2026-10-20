package lifecycle

import (
	"context"
	"encoding/json"
	"github.com/imzyb/MiGate/internal/control"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Only run in a disposable postgres:18-alpine container joined to the dedicated
// test.compose network. This performs real schema backup/purge/restore.
func TestIsolatedCenterPurgeAndPGRestore(t *testing.T) {
	if os.Getenv("MIGATE_CENTER_PURGE_TEST") != "1" {
		t.Skip("requires isolated PG18 tool container")
	}
	ctx := context.Background()
	cfg := control.Config{Installed: true, EncryptionKey: "1111111111111111111111111111111111111111111111111111111111111111", Listen: "127.0.0.1:19810", Database: control.DatabaseConfig{Host: "postgres", Port: 5432, User: "gate", Password: "local-test-only", Database: "gate", SSLMode: "disable"}, Redis: control.RedisConfig{Address: "redis:6379", Username: "migate", Password: "local-test-only"}}
	pc, e := pgx.ParseConfig(pgDSN(cfg.Database))
	if e != nil {
		t.Fatal(e)
	}
	pc.Password = cfg.Database.Password
	pg, e := pgx.ConnectConfig(ctx, pc)
	if e != nil {
		t.Fatal(e)
	}
	defer pg.Close(ctx)
	for _, q := range []string{"CREATE SCHEMA IF NOT EXISTS migate", "CREATE TABLE IF NOT EXISTS migate.lifecycle_probe(id integer PRIMARY KEY, value text)", "INSERT INTO migate.lifecycle_probe VALUES(1,'recoverable') ON CONFLICT(id) DO NOTHING", "CREATE TABLE IF NOT EXISTS public.other_app_sentinel(value text)", "INSERT INTO public.other_app_sentinel VALUES('preserved')"} {
		if _, e = pg.Exec(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	r := redis.NewClient(&redis.Options{Addr: cfg.Redis.Address})
	defer r.Close()
	if e = r.Set(ctx, "other-app:sentinel", "preserved", 0).Err(); e != nil {
		t.Fatal(e)
	}
	if e = r.Set(ctx, "migate:lifecycle-probe", "recoverable", 0).Err(); e != nil {
		t.Fatal(e)
	}
	tools := t.TempDir()
	if e = os.WriteFile(filepath.Join(tools, "systemctl"), []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", tools+":"+os.Getenv("PATH"))
	m := fixture("control")
	m.Releases = []string{"v1", "v2"}
	for _, p := range []string{"/etc/migate-control", "/var/lib/migate-control", "/opt/migate-control/releases/v2"} {
		if e = os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	b, _ := json.Marshal(cfg)
	if e = os.WriteFile("/etc/migate-control/config.json", b, 0600); e != nil {
		t.Fatal(e)
	}
	backupDir := t.TempDir()
	o := Options{Command: "uninstall", Purge: true, Confirm: "control:uninstall:purge", BackupDir: backupDir, PGDump: "/usr/local/bin/pg_dump"}
	if e = Execute(ctx, m, o, io.Discard); e != nil {
		t.Fatal(e)
	}
	var schemaExists bool
	if e = pg.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='migate')").Scan(&schemaExists); e != nil || schemaExists {
		t.Fatal("schema not purged", e)
	}
	var sentinel string
	if e = pg.QueryRow(ctx, "SELECT value FROM public.other_app_sentinel LIMIT 1").Scan(&sentinel); e != nil || sentinel != "preserved" {
		t.Fatal("other schema changed", e)
	}
	if value, e := r.Get(ctx, "other-app:sentinel").Result(); e != nil || value != "preserved" {
		t.Fatal("other Redis namespace changed")
	}
	if r.Get(ctx, "migate:lifecycle-probe").Err() != redis.Nil {
		t.Fatal("owned Redis key not purged")
	}
	dumps, _ := filepath.Glob(filepath.Join(backupDir, "*.pgdump"))
	if len(dumps) != 1 {
		t.Fatal("database backup missing")
	}
	snapshots, _ := filepath.Glob(filepath.Join(backupDir, "*.snapshot.json"))
	if len(snapshots) != 1 {
		t.Fatal("snapshot manifest missing")
	}
	recovery := Options{Command: "restore", Snapshot: snapshots[0], BackupDir: filepath.Join(backupDir, "before-restore"), PGRestore: "/usr/local/bin/pg_restore", PGDump: "/usr/local/bin/pg_dump", Confirm: "control:restore"}
	if e = Execute(ctx, m, recovery, io.Discard); e != nil {
		t.Fatal("restore command failed", e)
	}
	if e = pg.QueryRow(ctx, "SELECT value FROM migate.lifecycle_probe WHERE id=1").Scan(&sentinel); e != nil || sentinel != "recoverable" {
		t.Fatal("database probe not restored", e)
	}
	if _, e = os.Stat("/etc/migate-control/config.json"); e != nil {
		t.Fatal("configuration not restored", e)
	}
	if value, e := r.Get(ctx, "migate:lifecycle-probe").Result(); e != nil || value != "recoverable" {
		t.Fatal("Redis probe restore failed")
	}
}
