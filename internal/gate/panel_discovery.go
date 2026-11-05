package gate

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// 只读适配已核对的 1Panel v2.3.2 格式：不写面板数据库、不重置其他消费者的密钥。
// API key 格式依据 1Panel v2.3.2 core/utils/encrypt/api_key.go。
func discoverPanelAuthorization(dbPath string) (PanelConfig, error) {
	u := url.URL{Scheme: "file", Path: dbPath}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return PanelConfig{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	settings := map[string]string{}
	for _, key := range []string{"SystemVersion", "ServerPort", "SSL", "EncryptKey"} {
		var v string
		if err = db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&v); err != nil {
			return PanelConfig{}, errors.New("无法读取 1Panel 本机授权配置")
		}
		settings[key] = v
	}
	if strings.TrimPrefix(settings["SystemVersion"], "v") != "2.3.2" {
		return PanelConfig{}, errors.New("尚未验证此 1Panel 版本的本机授权读取，请使用面板 API 授权入口")
	}
	rows, err := db.Query("SELECT id, secret_ciphertext, ip_white_list, expires_at FROM api_keys WHERE name = ? AND status = ? AND secret_version = 1 ORDER BY created_at DESC", "Weir certificate manager", "Enable")
	if err != nil {
		return PanelConfig{}, errors.New("此 1Panel 的本机 API 授权格式不受支持")
	}
	defer rows.Close()
	for rows.Next() {
		var id, encrypted, ips string
		var expires sql.NullString
		if err = rows.Scan(&id, &encrypted, &ips, &expires); err != nil {
			return PanelConfig{}, errors.New("读取 1Panel 授权失败")
		}
		if expires.Valid && expires.String != "" {
			t, err := time.Parse(time.RFC3339Nano, expires.String)
			if err != nil || !time.Now().Before(t) {
				continue
			}
		}
		allowed := false
		for _, s := range strings.FieldsFunc(ips, func(r rune) bool { return r == '\n' || r == ',' || r == ';' }) {
			s = strings.TrimSpace(s)
			if s == "127.0.0.1" {
				allowed = true
			}
			if _, network, err := net.ParseCIDR(s); err == nil && network.Contains(net.ParseIP("127.0.0.1")) {
				allowed = true
			}
		}
		if !allowed {
			continue
		}
		secret, err := decryptPanelAPIKey(encrypted, id, settings["EncryptKey"])
		if err != nil {
			return PanelConfig{}, err
		}
		port, err := strconv.Atoi(settings["ServerPort"])
		if err != nil || port < 1 || port > 65535 {
			return PanelConfig{}, errors.New("1Panel 端口无效")
		}
		return PanelConfig{Port: port, HTTPS: settings["SSL"] == "Enable", APIKey: secret}, nil
	}
	if err = rows.Err(); err != nil {
		return PanelConfig{}, errors.New("读取 1Panel 授权失败")
	}
	return PanelConfig{}, errors.New("请先登录 1Panel 为 Weir 创建专用 API Key，仅允许 127.0.0.1；随后可自动接入，无需复制密钥")
}

func decryptPanelAPIKey(value, id, key string) (string, error) {
	derived := sha256.Sum256([]byte("1panel:api-key:v1:" + key))
	block, err := aes.NewCipher(derived[:])
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	data, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil || len(data) < aead.NonceSize()+aead.Overhead() {
		return "", errors.New("1Panel API Key 格式无效")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte("api-key:v1:"+id))
	if err != nil {
		return "", errors.New("无法解密 Weir 的 1Panel 本机授权")
	}
	return string(plain), nil
}

func (h *Helper) discoverPanel(ctx context.Context) error {
	if _, ok := h.loadPanel(); ok {
		return nil
	}
	if _, err := os.Stat("/www/server/panel"); err == nil {
		return baotaHTTPRoot(baotaDefaultConfig, baotaDefaultRoot)
	}
	path := "/opt/1panel/db/core.db"
	if _, err := os.Stat(path); err != nil {
		return errors.New("未找到受支持的 1Panel 本机授权配置")
	}
	cfg, err := discoverPanelAuthorization(path)
	if err != nil {
		return err
	}
	if _, err = newPanelClient(cfg).version(ctx); err != nil {
		return err
	}
	b, _ := json.Marshal(cfg)
	if err = os.MkdirAll(filepath.Dir(h.panelPath()), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.panelPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("保存 Weir 面板授权失败：%w", err)
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(h.panelPath())
		return err
	}
	return closeErr
}
