package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestBoundedMaintenancePreservesLiveState(t *testing.T) {
	s, err := Open(t.Context(), Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "db.sqlite"), Maintenance: MaintenanceConfig{KVBatch: 2}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, key := range []string{"expired-a", "expired-b", "expired-c"} {
		if err = s.KV.Set(t.Context(), key, "x", time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.KV.Set(t.Context(), "session", "live", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err = s.KV.Set(t.Context(), "durable", "pending", 0); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.ExecContext(t.Context(), "UPDATE kv SET expires_at=1 WHERE key LIKE 'expired-%'")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Maintain(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM kv WHERE expires_at=1").Scan(&count); err != nil || count != 1 {
		t.Fatalf("batch count=%d err=%v", count, err)
	}
	if err = s.Maintain(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session", "durable"} {
		if _, ok, err := s.KV.Get(context.Background(), key); err != nil || !ok {
			t.Fatalf("lost %s", key)
		}
	}
}

func TestInvalidPoolConfiguration(t *testing.T) {
	for _, cfg := range []PoolConfig{{MaxOpen: -1}, {MaxOpen: 2, MaxIdle: 3}, {LifetimeSeconds: 90000}} {
		if cfg.validate() == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
}
