package gate

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPanelDiscoveryReadsOnlyNamedLoopbackAuthorization(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "core.db")
	other := filepath.Join(dir, "other.db")
	secret := "isolated-test-api-key"
	id := "test-key-id"
	key := sha256.Sum256([]byte("1panel:api-key:v1:test-master"))
	block, _ := aes.NewCipher(key[:])
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	encrypted := base64.RawStdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(secret), []byte("api-key:v1:"+id)))
	writeDB := func(target, name, ip, version string) {
		t.Helper()
		db, err := sql.Open("sqlite", target)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		for _, q := range []string{
			`CREATE TABLE IF NOT EXISTS settings (key TEXT,value TEXT)`,
			`CREATE TABLE IF NOT EXISTS api_keys(id TEXT,name TEXT,status TEXT,secret_version INTEGER,secret_ciphertext TEXT,ip_white_list TEXT,expires_at TEXT,created_at TEXT)`,
			`DELETE FROM settings`,
			`DELETE FROM api_keys`,
		} {
			if _, err = db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		for k, v := range map[string]string{"SystemVersion": version, "ServerPort": "18276", "SSL": "Disable", "EncryptKey": "test-master"} {
			if _, err = db.Exec("INSERT INTO settings VALUES (?,?)", k, v); err != nil {
				t.Fatal(err)
			}
		}
		if name != "" {
			if _, err = db.Exec("INSERT INTO api_keys VALUES (?,?,?,1,?,?,NULL,?)", id, name, "Enable", encrypted, ip, "2026-11-04"); err != nil {
				t.Fatal(err)
			}
		}
	}
	reject := func(target, want string) {
		t.Helper()
		_, err := discoverPanelAuthorization(target)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("discover %s = %v, want %s", target, err, want)
		}
	}
	writeDB(other, "Weir certificate manager", "127.0.0.1", "v2.3.2")
	writeDB(path, "unrelated-client", "127.0.0.1", "v2.3.2")
	reject(path, "专用 API Key")
	writeDB(path, "Weir certificate manager", "203.0.113.1", "v2.3.2")
	reject(path, "127.0.0.1")
	writeDB(path, "Weir certificate manager", "127.0.0.1", "v2.3.2")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := discoverPanelAuthorization(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != secret || cfg.Port != 18276 || cfg.HTTPS {
		t.Fatal("unexpected authorization configuration")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("panel database was modified", err)
	}
	if _, err = decryptPanelAPIKey(encrypted, "other-id", "test-master"); err == nil {
		t.Fatal("ciphertext identity not authenticated")
	}
	writeDB(path, "Weir certificate manager", "127.0.0.1", "v9.0.0")
	reject(path, "版本")
}
