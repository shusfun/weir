package control

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/imzyb/MiGate/ent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
)

type DatabaseConfig struct {
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
type Config struct {
	// 只配置监听地址；公共地址按请求推导，旧配置中的 public_url 被忽略
	Listen        string         `json:"listen"`
	Database      DatabaseConfig `json:"database"`
	Redis         RedisConfig    `json:"redis"`
	EncryptionKey string         `json:"encryption_key"`
	SetupHash     string         `json:"setup_hash"`
	SetupExpires  time.Time      `json:"setup_expires"`
	Installed     bool           `json:"installed"`
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, err
	}
	if err == nil {
		if err = json.Unmarshal(b, &c); err != nil {
			return c, err
		}
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:19810"
	}
	if c.EncryptionKey == "" {
		c.EncryptionKey = randomToken(32)
	}
	if _, _, err = net.SplitHostPort(c.Listen); err != nil {
		return c, fmt.Errorf("invalid listen address: %w", err)
	}
	return c, nil
}
func saveConfig(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	if err = os.WriteFile(temp, b, 0600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
func openDatabase(c DatabaseConfig) (*sql.DB, *ent.Client, error) {
	if c.Port == 0 {
		c.Port = 5432
	}
	if c.SSLMode == "" {
		c.SSLMode = "disable"
	}
	u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.Host, fmt.Sprint(c.Port)), Path: "/" + c.Database, User: url.UserPassword(c.User, c.Password)}
	q := u.Query()
	q.Set("sslmode", c.SSLMode)
	q.Set("connect_timeout", "5")
	u.RawQuery = q.Encode()
	pc, err := pgx.ParseConfig(u.String())
	if err != nil {
		return nil, nil, errors.New("invalid PostgreSQL configuration")
	}
	pc.RuntimeParams["search_path"] = "migate"
	pc.RuntimeParams["application_name"] = "migate"
	db := stdlib.OpenDB(*pc)
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db, ent.NewClient(ent.Driver(entsql.OpenDB("postgres", db))), nil
}
func redisClient(c RedisConfig) *redis.Client {
	opts := &redis.Options{Addr: c.Address, Username: c.Username, Password: c.Password, DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	if c.TLS {
		opts.TLSConfig = redisTLS()
	}
	return redis.NewClient(opts)
}

type setupCredential struct {
	Installed bool
	Hash      string
	Expires   time.Time
}

// 安装凭证以磁盘为准。只读取校验所需字段，避免 LoadConfig 在密钥为空时生成新密钥。
func readSetupCredential(path string) (setupCredential, error) {
	var cred setupCredential
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cred, nil
	}
	if err != nil {
		return cred, err
	}
	var raw struct {
		Installed    bool      `json:"installed"`
		SetupHash    string    `json:"setup_hash"`
		SetupExpires time.Time `json:"setup_expires"`
	}
	if err = json.Unmarshal(b, &raw); err != nil {
		return cred, err
	}
	cred.Installed = raw.Installed
	cred.Hash = raw.SetupHash
	cred.Expires = raw.SetupExpires
	return cred, nil
}

func readSetupToken(path string) string {
	b, err := os.ReadFile(filepath.Join(filepath.Dir(path), "setup-token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func replaceFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(src, dst)
}

func Initialize(path string, c Config) (string, error) {
	if c.Installed {
		return "", errors.New("already installed")
	}
	disk, err := readSetupCredential(path)
	if err != nil {
		return "", err
	}
	if disk.Installed {
		return "", errors.New("already installed")
	}
	token := randomToken(32)
	c.SetupHash = digest(token)
	c.SetupExpires = time.Now().Add(30 * time.Minute)
	dir := filepath.Dir(path)
	pending := filepath.Join(dir, "setup-token.new")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err = os.WriteFile(pending, []byte(token), 0600); err != nil {
		return "", err
	}
	if err = saveConfig(path, c); err != nil {
		_ = os.Remove(pending)
		return "", err
	}
	if err = replaceFile(pending, filepath.Join(dir, "setup-token")); err != nil {
		return "", err
	}
	_ = os.Remove(filepath.Join(dir, "initialization.txt"))
	return token, nil
}
