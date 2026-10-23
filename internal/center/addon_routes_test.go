package center

import (
	"strings"
	"testing"
)

// 公告按受众投影：登录受众只给已登录用户，匿名受众只给未登录访问；草稿不投影；旧版本号保存被拒绝。
func TestAnnouncementAudiences(t *testing.T) {
	s, admin := newInstalledServer(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "erin@example.test", "password": "Passw0rd"}, 200)
	erin := login(t, s, "erin@example.test", "Passw0rd")
	anon := &testClient{t: t, router: admin.router}

	doc := mustJSON(t, admin, "GET", "/api/v1/admin/announcement", nil, 200)
	rev := doc["revision"].(string)
	body := func(enabled, authed, public bool, rev string) map[string]any {
		return map[string]any{"announcement": map[string]any{"enabled": enabled, "title": "维护通知", "content": "今晚维护", "type": "info", "authenticated": authed, "public": public, "dismissible": true}, "revision": rev, "password": "Passw0rd"}
	}
	saved := mustJSON(t, admin, "PUT", "/api/v1/admin/announcement", body(true, true, false, rev), 200)
	mustJSON(t, admin, "PUT", "/api/v1/admin/announcement", body(true, true, false, rev), 409)

	if a := mustJSON(t, erin, "GET", "/api/v1/announcements", nil, 200)["announcement"]; a == nil || a.(map[string]any)["notice_title"] != "维护通知" {
		t.Fatalf("signed-in audience = %v", a)
	}
	if a := mustJSON(t, anon, "GET", "/api/v1/announcements/public", nil, 200)["announcement"]; a != nil {
		t.Fatalf("anonymous must not see a signed-in-only notice: %v", a)
	}
	mustJSON(t, admin, "PUT", "/api/v1/admin/announcement", body(false, true, true, saved["revision"].(string)), 200)
	if w := anon.do("GET", "/api/v1/announcements/public", nil, nil); strings.Contains(w.Body.String(), "维护") {
		t.Fatalf("disabled draft leaked: %s", w.Body.String())
	}
	// 普通用户只能读取启用的客户端目录，不能修改。
	if w := erin.do("PUT", "/api/v1/admin/clients", map[string]any{}, nil); w.Code != 403 {
		t.Fatalf("user catalog write = %d", w.Code)
	}
	if items := mustJSON(t, erin, "GET", "/api/v1/clients", nil, 200)["clients"].([]any); len(items) == 0 {
		t.Fatal("default client catalog empty")
	}
}
