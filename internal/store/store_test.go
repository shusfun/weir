package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/user"
)

func openSQLiteForTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "migate.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// openPostgresForTest 只在 MIGATE_INTEGRATION=1 时连接 deploy/test.compose.yml 的一次性容器。
// 每次新建独立数据库并在结束时删除，不触碰容器里本地开发环境使用的 gate 库。
func openPostgresForTest(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("MIGATE_INTEGRATION") != "1" {
		t.Skip("set MIGATE_INTEGRATION=1 with deploy/test.compose.yml running")
	}
	admin, err := sql.Open("pgx", "postgres://gate:local-test-only@127.0.0.1:25432/gate?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := fmt.Sprintf("migate_store_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)") })
	s, err := Open(context.Background(), Config{
		Driver:   DriverPostgres,
		Postgres: PostgresConfig{Host: "127.0.0.1", Port: 25432, User: "gate", Password: "local-test-only", Database: name},
		Redis:    RedisConfig{Address: "127.0.0.1:26379", Username: "migate", Password: "local-test-only"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func backends(t *testing.T) map[string]func(*testing.T) *Store {
	return map[string]func(*testing.T) *Store{"sqlite": openSQLiteForTest, "postgres": openPostgresForTest}
}

func TestValidateRejectsIncompleteConfig(t *testing.T) {
	for _, c := range []Config{{}, {Driver: DriverSQLite}, {Driver: DriverSQLite, SQLitePath: "relative.db"}, {Driver: DriverPostgres, Postgres: PostgresConfig{Host: "h", Database: "d", User: "u"}}, {Driver: "mysql"}} {
		if c.Validate() == nil {
			t.Fatalf("%+v should be rejected", c)
		}
	}
}

func TestStoreBackends(t *testing.T) {
	for name, open := range backends(t) {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			ctx := context.Background()
			if err := s.Ping(ctx); err != nil {
				t.Fatal(err)
			}
			email := "admin-" + name + "-" + time.Now().Format("150405.000000") + "@example.test"
			u, err := s.Ent.User.Create().SetEmail(email).SetPasswordHash("x").SetRole(user.RoleAdmin).Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.Ent.User.Query().Where(user.EmailEQ(email)).Only(ctx)
			if err != nil || got.ID != u.ID || got.Role != user.RoleAdmin || got.ExpiresAt != nil {
				t.Fatalf("got %+v, %v", got, err)
			}
			testKV(t, s.KV, "test:"+name+":"+time.Now().Format("150405.000000")+":")
			testLocker(t, s.Lock, "test-"+name)
		})
	}
}

func testKV(t *testing.T, kv KV, p string) {
	ctx := context.Background()
	if _, ok, err := kv.Get(ctx, p+"missing"); ok || err != nil {
		t.Fatalf("missing key: ok=%v err=%v", ok, err)
	}
	if err := kv.Set(ctx, p+"a", "1", time.Minute); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := kv.Get(ctx, p+"a"); !ok || v != "1" {
		t.Fatalf("get a = %q %v", v, ok)
	}
	if ok, _ := kv.SetNX(ctx, p+"a", "2", time.Minute); ok {
		t.Fatal("SetNX must not overwrite a live key")
	}
	if ok, err := kv.SetNX(ctx, p+"b", "2", time.Minute); !ok || err != nil {
		t.Fatalf("SetNX new key: %v %v", ok, err)
	}
	if v, ok, _ := kv.GetDel(ctx, p+"b"); !ok || v != "2" {
		t.Fatalf("GetDel = %q %v", v, ok)
	}
	if _, ok, _ := kv.GetDel(ctx, p+"b"); ok {
		t.Fatal("GetDel must consume the key")
	}
	for want := int64(1); want <= 3; want++ {
		if n, err := kv.Incr(ctx, p+"count", time.Minute); err != nil || n != want {
			t.Fatalf("Incr = %d %v, want %d", n, err, want)
		}
	}
	keys, err := kv.Keys(ctx, p, 10)
	if err != nil || len(keys) != 2 {
		t.Fatalf("Keys = %v %v", keys, err)
	}
	if _, err = kv.Keys(ctx, p, 1); !errors.Is(err, ErrTooMany) {
		t.Fatalf("Keys over limit err = %v", err)
	}
	if err = kv.Set(ctx, p+"short", "x", time.Second); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, ok, _ := kv.Get(ctx, p+"short"); ok {
		t.Fatal("expired key still readable")
	}
	if ok, _ := kv.SetNX(ctx, p+"short", "y", time.Minute); !ok {
		t.Fatal("SetNX must replace an expired key")
	}
	if err = kv.Del(ctx, p+"a", p+"count", p+"short"); err != nil {
		t.Fatal(err)
	}
	if keys, _ = kv.Keys(ctx, p, 10); len(keys) != 0 {
		t.Fatalf("keys after Del = %v", keys)
	}
}

func testLocker(t *testing.T, l Locker, name string) {
	var active, maxActive int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := l.Do(context.Background(), name, func(context.Context) error {
				n := atomic.AddInt32(&active, 1)
				for {
					m := atomic.LoadInt32(&maxActive)
					if n <= m || atomic.CompareAndSwapInt32(&maxActive, m, n) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				atomic.AddInt32(&active, -1)
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if maxActive != 1 {
		t.Fatalf("lock allowed %d concurrent holders", maxActive)
	}
}
