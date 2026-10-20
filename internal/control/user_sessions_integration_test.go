package control

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

func testAdminSessionsIntegration(t *testing.T, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	decode := func(w *httptest.ResponseRecorder, code int) map[string]any {
		t.Helper()
		if w.Code != code {
			t.Fatalf("session API got %d want %d", w.Code, code)
		}
		var d map[string]any
		json.Unmarshal(w.Body.Bytes(), &d)
		return d
	}
	me := decode(request("GET", "/api/v1/me", nil, "", alice), 200)
	id := int(me["id"].(float64))
	base := fmt.Sprintf("/api/v1/admin/users/%d/sessions", id)
	other := decode(request("GET", "/api/v1/me", nil, "", admin), 200)
	adminID := int(other["id"].(float64))
	decode(request("GET", base, nil, "", alice), 403)
	decode(request("GET", base, nil, "", ""), 401)
	items := decode(request("GET", base, nil, "", admin), 200)["items"].([]any)
	if len(items) == 0 {
		t.Fatal("admin cannot find user session")
	}
	item := items[0].(map[string]any)
	if item["user_id"] != nil || item["token"] != nil || item["expires_in"].(float64) <= 0 || len(item["id"].(string)) != 64 {
		t.Fatal("session view incorrect")
	}
	// Path ownership is checked atomically, even for an administrator.
	decode(request("DELETE", fmt.Sprintf("/api/v1/admin/users/%d/sessions/%s", adminID, item["id"]), nil, "", admin), 404)
	decode(request("GET", "/api/v1/me", nil, "", alice), 200)
	decode(request("DELETE", base+"/bad", nil, "", admin), 400)
	decode(request("DELETE", base+"/"+item["id"].(string), nil, "", admin), 200)
	decode(request("GET", "/api/v1/me", nil, "", alice), 401)
	decode(request("GET", "/api/v1/me", nil, "", admin), 200)
	decode(request("DELETE", base+"/"+item["id"].(string), nil, "", admin), 404)
}
