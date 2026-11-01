package center

import "testing"

// 普通设置不带密码直接保存；改密码与二次验证、通行密钥、API 令牌、导入或恢复备份仍要求重新验证身份。
func TestReauthOnlyForAccountSecurityAndRestore(t *testing.T) {
	_, admin := newInstalledServer(t)

	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/security", map[string]any{"totp_enabled": true, "passkey_enabled": true}, 200)
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/notify", map[string]any{"enabled": false}, 200)
	mustJSON(t, admin, "PUT", "/api/v1/admin/center-origin", map[string]any{"origin": "https://example.com"}, 200)
	// 更改已记录的中心地址也不再要求密码。
	mustJSON(t, admin, "PUT", "https://new.example.com/api/v1/admin/center-origin", map[string]any{"origin": "https://new.example.com"}, 200)

	for _, r := range []struct{ method, path string }{
		{"POST", "/api/v1/security/totp/setup"},
		{"POST", "/api/v1/security/totp/disable"},
		{"POST", "/api/v1/security/password"},
		{"POST", "/api/v1/security/passkey/begin"},
		{"DELETE", "/api/v1/security/passkeys/1"},
		{"POST", "/api/v1/admin/api-tokens"},
		{"POST", "/api/v1/admin/backup/import"},
		{"POST", "/api/v1/admin/backup/webdav/restore"},
	} {
		w := admin.do(r.method, r.path, map[string]any{"name": "x", "content": "{}", "file": "weir-subscriptions-1.json", "new_password": "NewPassw0rd"}, nil)
		if w.Code != 401 || decode(t, w)["error"].(map[string]any)["code"] != "password_required" {
			t.Fatalf("%s %s without password = %d %s", r.method, r.path, w.Code, w.Body.String())
		}
	}
}
