package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imzyb/MiGate/ent/resource"
)

func testAnnouncementIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, ordinary string) {
	t.Helper()
	ctx := context.Background()
	defer s.Store.Resource.Delete().Where(resource.KindEQ("settings"), resource.KeyEQ("global-announcement")).Exec(ctx)
	read := func() announcementDocument {
		t.Helper()
		w := request("GET", "/api/v1/admin/announcement", nil, "", admin)
		var d announcementDocument
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil {
			t.Fatal("announcement read failed", w.Code)
		}
		return d
	}
	if request("GET", "/api/v1/admin/announcement", nil, "", ordinary).Code != 403 || request("GET", "/api/v1/announcements", nil, "", "").Code != 401 {
		t.Fatal("announcement access boundary")
	}
	d := read()
	if d.Announcement.Enabled || d.Announcement.Public || d.Announcement.Authenticated {
		t.Fatal("unsafe defaults")
	}
	body := map[string]any{"announcement": globalAnnouncement{Content: "PRIVATE DRAFT", Type: "warning", Dismissible: true}, "revision": d.Revision, "password": "Local1-testing-password"}
	if request("PUT", "/api/v1/admin/announcement", body, "", ordinary).Code != 403 {
		t.Fatal("ordinary write")
	}
	body["password"] = "wrong"
	if request("PUT", "/api/v1/admin/announcement", body, "", admin).Code != 401 {
		t.Fatal("reauth bypass")
	}
	body["password"] = "Local1-testing-password"
	save := func(a globalAnnouncement) {
		t.Helper()
		body["announcement"] = a
		body["revision"] = read().Revision
		if w := request("PUT", "/api/v1/admin/announcement", body, "", admin); w.Code != 200 {
			t.Fatal("announcement save failed", w.Code)
		}
	}
	a := body["announcement"].(globalAnnouncement)
	save(a)
	for _, path := range []string{"/api/v1/announcements/public", "/api/v1/announcements"} {
		cookie := ""
		if path == "/api/v1/announcements" {
			cookie = ordinary
		}
		w := request("GET", path, nil, "", cookie)
		if w.Code != 200 || w.Body.String() != "{\"announcement\":null}" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("draft leaked", path)
		}
	}
	a.Enabled = true
	a.Authenticated = true
	a.Content = "<script>literal</script>"
	save(a)
	w := request("GET", "/api/v1/announcements", nil, "", ordinary)
	var projection map[string]map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &projection) != nil || len(projection["announcement"]) != 5 || projection["announcement"]["notice"] != a.Content {
		t.Fatal("authenticated projection")
	}
	if request("GET", "/api/v1/announcements/public", nil, "", "").Body.String() != "{\"announcement\":null}" {
		t.Fatal("public opt in bypass")
	}
	if request("PUT", "/api/v1/admin/announcement", body, "", admin).Code != 409 {
		t.Fatal("stale save accepted")
	}
	a.Public = true
	a.Authenticated = false
	save(a)
	if request("GET", "/api/v1/announcements", nil, "", ordinary).Body.String() != "{\"announcement\":null}" {
		t.Fatal("authenticated opt in bypass")
	}
	w = request("GET", "/api/v1/announcements/public", nil, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "notice_revision") {
		t.Fatal("public publish failed")
	}
	a.Enabled = false
	save(a)
	if request("GET", "/api/v1/announcements/public", nil, "", "").Body.String() != "{\"announcement\":null}" {
		t.Fatal("withdrawal failed")
	}
	// The same parser validates preview and persisted/public rich content.
	a.Enabled = true
	a.Public = true
	a.ContentFormat = "html"
	a.Content = `<p><strong>Notice</strong> <a href="https://example.com/help">Help</a></p>`
	save(a)
	w = request("GET", "/api/v1/announcements/public", nil, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"notice_nodes"`) || strings.Contains(w.Body.String(), `"onclick"`) {
		t.Fatal("rich projection")
	}
	if request("POST", "/api/v1/admin/announcement/preview", map[string]any{"content": a.Content, "format": "html"}, "", ordinary).Code != 403 {
		t.Fatal("ordinary preview access")
	}
	w = request("POST", "/api/v1/admin/announcement/preview", map[string]any{"content": a.Content, "format": "html"}, "", admin)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"tag":"strong"`) {
		t.Fatal("rich preview failed")
	}
	for _, content := range []string{`<script>alert(1)</script>`, `<a href="javascript:alert(1)">X</a>`, `<p style="position:fixed">X</p>`} {
		if request("POST", "/api/v1/admin/announcement/preview", map[string]any{"content": content, "format": "html"}, "", admin).Code != 400 {
			t.Fatal("unsafe preview accepted")
		}
		invalid := a
		invalid.Content = content
		body["announcement"] = invalid
		body["revision"] = read().Revision
		if request("PUT", "/api/v1/admin/announcement", body, "", admin).Code != 400 {
			t.Fatal("unsafe publish accepted")
		}
	}
	// Corrupt storage fails closed; never project an unvalidated draft.
	s.Store.Resource.Update().Where(resource.KindEQ("settings"), resource.KeyEQ("global-announcement")).SetData(map[string]any{"type": "unknown"}).SaveX(ctx)
	if request("GET", "/api/v1/announcements/public", nil, "", "").Code != 503 {
		t.Fatal("corrupt announcement silently accepted")
	}
}
