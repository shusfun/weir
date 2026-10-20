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
	Listen        string         `json:"listen"`
	PublicURL     string         `json:"public_url"`
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
	if c.PublicURL == "" {
		c.PublicURL = "http://localhost:19810"
	}
	if c.EncryptionKey == "" {
		c.EncryptionKey = randomToken(32)
	}
	if _, _, err = net.SplitHostPort(c.Listen); err != nil {
		return c, fmt.Errorf("invalid listen address: %w", err)
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		return c, errors.New("public_url must be HTTPS, except localhost")
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
func Initialize(path string, c Config) (string, error) {
	if c.Installed {
		return "", errors.New("already installed")
	}
	token := randomToken(32)
	c.SetupHash = digest(token)
	c.SetupExpires = time.Now().Add(30 * time.Minute)
	if err := saveConfig(path, c); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "setup-token"), []byte(token), 0600); err != nil {
		return "", err
	}
	return token, nil
}
