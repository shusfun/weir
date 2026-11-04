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
	path := filepath.Join(t.TempDir(), "core.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`CREATE TABLE settings (key TEXT,value TEXT)`, `CREATE TABLE api_keys(id TEXT,name TEXT,status TEXT,secret_version INTEGER,secret_ciphertext TEXT,ip_white_list TEXT,expires_at TEXT,created_at TEXT)`} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range map[string]string{"SystemVersion": "v2.3.2", "ServerPort": "18276", "SSL": "Disable", "EncryptKey": "test-master"} {
		if _, err = db.Exec("INSERT INTO settings VALUES (?,?)", k, v); err != nil {
			t.Fatal(err)
		}
	}
	secret := "isolated-test-api-key"
	id := "test-key-id"
	key := sha256.Sum256([]byte("1panel:api-key:v1:test-master"))
	block, _ := aes.NewCipher(key[:])
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	encrypted := base64.RawStdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(secret), []byte("api-key:v1:"+id)))
	insert := func(name, ip string) {
		t.Helper()
		if _, err = db.Exec("INSERT INTO api_keys VALUES (?,?,?,1,?,?,NULL,?)", id, name, "Enable", encrypted, ip, "2026-11-04"); err != nil {
			t.Fatal(err)
		}
	}
	insert("unrelated-client", "127.0.0.1")
	if _, err = discoverPanelAuthorization(path); err == nil {
		t.Fatal("unrelated consumer authorization reused")
	}
	db.Exec("DELETE FROM api_keys")
	insert("Weir certificate manager", "203.0.113.1")
	if _, err = discoverPanelAuthorization(path); err == nil {
		t.Fatal("non-loopback authorization accepted")
	}
	db.Exec("DELETE FROM api_keys")
	insert("Weir certificate manager", "127.0.0.1")
	db.Close()
	before, _ := os.ReadFile(path)
	cfg, err := discoverPanelAuthorization(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != secret || cfg.Port != 18276 || cfg.HTTPS {
		t.Fatal("unexpected authorization configuration")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("panel database was modified")
	}
	if _, err = decryptPanelAPIKey(encrypted, "other-id", "test-master"); err == nil {
		t.Fatal("ciphertext identity not authenticated")
	}
	db, _ = sql.Open("sqlite", path)
	db.Exec("UPDATE settings SET value='v9.0.0' WHERE key='SystemVersion'")
	db.Close()
	if _, err = discoverPanelAuthorization(path); err == nil || !strings.Contains(err.Error(), "版本") {
		t.Fatal("unknown panel version accepted")
	}
}
