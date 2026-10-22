// Package store 打开中心的数据存储：PostgreSQL + Redis，或只用 SQLite（内置缓存）。
// 业务代码只依赖 Ent 客户端、KV 与 Locker 三个接口，不感知具体后端。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"

	"github.com/imzyb/MiGate/internal/store/ent"
)

const (
	DriverPostgres = "postgres"
	DriverSQLite   = "sqlite"
)

// Config 由初始化向导写入中心配置文件。
type Config struct {
	Driver     string         `json:"driver"`
	SQLitePath string         `json:"sqlite_path,omitempty"`
	Postgres   PostgresConfig `json:"postgres,omitempty"`
	Redis      RedisConfig    `json:"redis,omitempty"`
}

type PostgresConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"ssl_mode"`
}

type RedisConfig struct {
	Address  string `json:"address"`
	Username string `json:"username"`
	Password string `json:"password"`
	TLS      bool   `json:"tls"`
}

// Store 是中心持有的全部存储句柄。
type Store struct {
	Driver string
	DB     *sql.DB
	Ent    *ent.Client
	KV     KV
	Lock   Locker
	closer func() error
}

func (c Config) Validate() error {
	switch c.Driver {
	case DriverSQLite:
		if c.SQLitePath == "" || !filepath.IsAbs(c.SQLitePath) {
			return errors.New("sqlite_path must be an absolute path")
		}
	case DriverPostgres:
		if c.Postgres.Host == "" || c.Postgres.Database == "" || c.Postgres.User == "" {
			return errors.New("postgres host, database and user are required")
		}
		if c.Redis.Address == "" {
			return errors.New("redis address is required with postgres")
		}
	default:
		return errors.New("driver must be postgres or sqlite")
	}
	return nil
}

// Open 连接存储、建表并返回可用句柄；失败时释放已打开的资源。
func Open(ctx context.Context, c Config) (*Store, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Driver == DriverSQLite {
		return openSQLite(ctx, c.SQLitePath)
	}
	return openPostgres(ctx, c)
}

func (s *Store) Close() error {
	var errs []error
	if s.Ent != nil {
		errs = append(errs, s.Ent.Close())
	}
	if s.closer != nil {
		errs = append(errs, s.closer())
	}
	return errors.Join(errs...)
}

// Ping 用于健康检查：数据库与缓存都必须可用。
func (s *Store) Ping(ctx context.Context) error {
	if err := s.DB.PingContext(ctx); err != nil {
		return err
	}
	return s.KV.Ping(ctx)
}

// Check 只测试连通性，不建表、不写数据，供初始化向导的「测试连接」使用。
func Check(ctx context.Context, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Driver == DriverSQLite {
		dir := filepath.Dir(c.SQLitePath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		probe, err := os.CreateTemp(dir, ".migate-probe-*")
		if err != nil {
			return err
		}
		probe.Close()
		return os.Remove(probe.Name())
	}
	p := c.Postgres
	if p.Port == 0 {
		p.Port = 5432
	}
	if p.SSLMode == "" {
		p.SSLMode = "disable"
	}
	u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(p.Host, fmt.Sprint(p.Port)), Path: "/" + p.Database, User: url.UserPassword(p.User, p.Password)}
	q := u.Query()
	q.Set("sslmode", p.SSLMode)
	q.Set("connect_timeout", "5")
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	if err = db.PingContext(ctx); err != nil {
		return err
	}
	rdb := newRedisClient(c.Redis)
	defer rdb.Close()
	return rdb.Ping(ctx).Err()
}

func openSQLite(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite 单写者：一个连接即可避免 SQLITE_BUSY，读写都经由同一连接串行。
	db.SetMaxOpenConns(1)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	s := &Store{Driver: DriverSQLite, DB: db, Ent: client, Lock: newLocalLocker(), closer: db.Close}
	if err = client.Schema.Create(ctx); err != nil {
		s.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	kv, err := newSQLiteKV(ctx, db)
	if err != nil {
		s.Close()
		return nil, err
	}
	s.KV = kv
	return s, nil
}

func openPostgres(ctx context.Context, c Config) (*Store, error) {
	p := c.Postgres
	if p.Port == 0 {
		p.Port = 5432
	}
	if p.SSLMode == "" {
		p.SSLMode = "disable"
	}
	u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(p.Host, fmt.Sprint(p.Port)), Path: "/" + p.Database, User: url.UserPassword(p.User, p.Password)}
	q := u.Query()
	q.Set("sslmode", p.SSLMode)
	q.Set("connect_timeout", "5")
	u.RawQuery = q.Encode()
	pc, err := pgx.ParseConfig(u.String())
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	// 只使用独立的 migate schema，不触碰共享实例中的其他数据。
	pc.RuntimeParams["search_path"] = "migate"
	pc.RuntimeParams["application_name"] = "migate"
	db := stdlib.OpenDB(*pc)
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	ping, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err = db.PingContext(ping); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres unavailable: %w", err)
	}
	if _, err = db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS migate"); err != nil {
		db.Close()
		return nil, fmt.Errorf("create migate schema: %w", err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	rdb := newRedisClient(c.Redis)
	s := &Store{Driver: DriverPostgres, DB: db, Ent: client, KV: redisKV{rdb}, Lock: pgLocker{db}, closer: func() error { return errors.Join(rdb.Close(), db.Close()) }}
	if err = client.Schema.Create(ctx); err != nil {
		s.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err = s.KV.Ping(ping); err != nil {
		s.Close()
		return nil, fmt.Errorf("redis unavailable: %w", err)
	}
	return s, nil
}

func newRedisClient(c RedisConfig) *redis.Client {
	opts := &redis.Options{Addr: c.Address, Username: c.Username, Password: c.Password, DialTimeout: 5 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second}
	if c.TLS {
		opts.TLSConfig = redisTLS()
	}
	return redis.NewClient(opts)
}
