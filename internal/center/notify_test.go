package center

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNotifyAlertsOnceAndExternalAPI(t *testing.T) {
	s, admin := newInstalledServer(t)
	var mu sync.Mutex
	var msgs []string
	var paths []string
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(b, &body)
		mu.Lock()
		msgs = append(msgs, body["text"].(string))
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Write([]byte(`{"ok":true}`))
	}))
	defer tg.Close()

	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/notify", map[string]any{"enabled": true, "bot_token": "123:ABC", "chat_id": "42", "api_base": tg.URL, "user_quota_percent": 80, "user_expiry_days": 3, "password": "Passw0rd"}, 200)
	if v := mustJSON(t, admin, "GET", "/api/v1/admin/settings/notify", nil, 200); v["token_set"] != true || strings.Contains(mustString(v), "123:ABC") {
		t.Fatalf("notify view leaks token: %v", v)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/settings/notify/test", nil, 200)
	if len(paths) != 1 || paths[0] != "/bot123:ABC/sendMessage" {
		t.Fatalf("telegram path = %v", paths)
	}

	ctx := t.Context()
	soon := time.Now().Add(48 * time.Hour)
	s.Store.Ent.User.Create().SetEmail("hank@example.test").SetName("Hank").SetPasswordHash("x").SetQuota(100).SetUsed(90).SetExpiresAt(soon).SaveX(ctx)
	if err := s.CheckAlerts(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckAlerts(ctx, time.Now()); err != nil { // 第二次不重复发送
		t.Fatal(err)
	}
	mu.Lock()
	got := strings.Join(msgs[1:], "\n")
	n := len(msgs)
	mu.Unlock()
	if n != 3 || !strings.Contains(got, "Hank") || !strings.Contains(got, "到期") {
		t.Fatalf("alerts (%d):\n%s", n, got)
	}

	// 外部接口：令牌只显示一次；只能管理订阅；撤销后失效；创建者降级后失效。
	mustJSON(t, admin, "POST", "/api/v1/admin/api-tokens", map[string]any{"name": "脚本", "password": "wrong"}, 401)
	tok := mustJSON(t, admin, "POST", "/api/v1/admin/api-tokens", map[string]any{"name": "脚本", "password": "Passw0rd"}, 200)
	token := tok["token"].(string)
	if list := mustJSON(t, admin, "GET", "/api/v1/admin/api-tokens", nil, 200)["items"].([]any); strings.Contains(mustString(list), token) {
		t.Fatal("token listed after creation")
	}
	ext := &testClient{t: t, router: admin.router}
	hdr := map[string]string{"Authorization": "Bearer " + token}
	if w := ext.do("POST", "/api/ext/v1/nodes", map[string]any{"url": "trojan://p@198.51.100.7:443?sni=example.com#ext"}, hdr); w.Code != 200 {
		t.Fatalf("ext create node = %d %s", w.Code, w.Body.String())
	}
	if w := ext.do("GET", "/api/ext/v1/nodes", nil, hdr); w.Code != 200 || !strings.Contains(w.Body.String(), "198.51.100.7") {
		t.Fatalf("ext list = %d", w.Code)
	}
	if w := ext.do("GET", "/api/v1/admin/users", nil, hdr); w.Code != 401 {
		t.Fatalf("api token must not reach admin API: %d", w.Code)
	}
	if w := ext.do("GET", "/api/ext/v1/nodes", nil, map[string]string{"Authorization": "Bearer " + strings.Repeat("x", 40)}); w.Code != 401 {
		t.Fatalf("bad token = %d", w.Code)
	}
	mustJSON(t, admin, "DELETE", "/api/v1/admin/api-tokens/"+itoa(int(tok["id"].(float64))), nil, 204)
	if w := ext.do("GET", "/api/ext/v1/nodes", nil, hdr); w.Code != 401 {
		t.Fatalf("revoked token = %d", w.Code)
	}
}
