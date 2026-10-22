// Package center 是重写后的中心服务：初始化、认证与业务 API。
package center

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imzyb/MiGate/internal/store"
)

// DefaultConfigPath 是中心配置文件的固定位置。
const DefaultConfigPath = "/etc/migate/center/config.json"

// Config 只配置监听地址与存储；公共地址按每次请求推导。
type Config struct {
	Listen        string       `json:"listen"`
	Store         store.Config `json:"store"`
	EncryptionKey string       `json:"encryption_key"`
	SetupHash     string       `json:"setup_hash,omitempty"`
	SetupExpires  time.Time    `json:"setup_expires,omitempty"`
	Installed     bool         `json:"installed"`
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// LoadConfig 读取配置；文件不存在时返回默认值（尚未初始化）。
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
	// root 重写时保留服务账号原有的所有者与权限，否则服务读不到配置。
	return writeReplacing(path, b)
}

// DefaultSQLitePath 是单机模式的数据库位置（与配置同属 /var/lib/migate 数据目录）。
func DefaultSQLitePath() string { return "/var/lib/migate/center/migate.db" }

type setupCredential struct {
	Installed bool
	Hash      string
	Expires   time.Time
}

// readSetupCredential 每次从磁盘读取，只取校验所需字段，避免误生成新密钥。
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
	return setupCredential{Installed: raw.Installed, Hash: raw.SetupHash, Expires: raw.SetupExpires}, nil
}

func setupTokenPath(path string) string { return filepath.Join(filepath.Dir(path), "setup-token") }

func readSetupToken(path string) string {
	b, err := os.ReadFile(setupTokenPath(path))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Initialize 生成新的 30 分钟初始化凭证：配置只存摘要，原文写入仅 root 可读的 setup-token。
func Initialize(path string, c Config) (string, error) {
	disk, err := readSetupCredential(path)
	if err != nil {
		return "", err
	}
	if c.Installed || disk.Installed {
		return "", errors.New("already installed")
	}
	token := randomToken(32)
	c.SetupHash = digest(token)
	c.SetupExpires = time.Now().Add(30 * time.Minute)
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	pending := setupTokenPath(path) + ".new"
	if err = os.WriteFile(pending, []byte(token), 0600); err != nil {
		return "", err
	}
	if err = saveConfig(path, c); err != nil {
		_ = os.Remove(pending)
		return "", err
	}
	if err = replaceFile(pending, setupTokenPath(path)); err != nil {
		return "", err
	}
	return token, nil
}

func replaceFile(src, dst string) error {
	if info, err := os.Lstat(dst); err == nil && !info.Mode().IsRegular() {
		return errors.New("refusing to replace a non-regular file")
	}
	if err := applyIdentity(src, targetFileIdentity(dst)); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(src, dst)
}
