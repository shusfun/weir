package control

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func testUserDirectoryIntegration(t *testing.T, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	get := func(path, cookie string, code int) map[string]any {
		t.Helper()
		w := request("GET", "/api/v1/admin/users"+path, nil, "", cookie)
		if w.Code != code {
			t.Fatalf("user directory %s: got %d expected %d", path, w.Code, code)
		}
		var data map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	a := get("?page=1&page_size=1", admin, 200)
	b := get("?page=2&page_size=1", admin, 200)
	first := a["items"].([]any)
	second := b["items"].([]any)
	if len(first) != 1 || len(second) != 1 || first[0].(map[string]any)["id"] == second[0].(map[string]any)["id"] || a["total"].(float64) < 3 {
		t.Fatal("pagination overlaps or loses total")
	}
	matched := get("?q=ALICE%40example&status=enabled&page=1&page_size=20", admin, 200)
	items := matched["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["email"] != "alice@example.test" {
		t.Fatal("email search/filter mismatch")
	}
	id := int(items[0].(map[string]any)["id"].(float64))
	detail := get(fmt.Sprintf("/%d", id), admin, 200)
	allowed := map[string]bool{"id": true, "email": true, "role": true, "enabled": true, "quota": true, "used": true, "expires": true, "email_verified": true, "totp_enrolled": true, "created_at": true}
	for key := range detail {
		if !allowed[key] {
			t.Fatalf("unexpected account field %s", key)
		}
	}
	get("?status=disabled", admin, 200)
	get("?q=missing-user-unique", admin, 200)
	for _, path := range []string{"?page=0", "?page=-1", "?page=oops", "?page_size=201", "?page_size=0", "?status=invalid", "?q=" + strings.Repeat("a", 201), "/invalid", "/0"} {
		get(path, admin, 400)
	}
	get("/9999999", admin, 404)
	get(fmt.Sprintf("/%d", id), alice, 403)
	get("?page=1", alice, 403)
	get(fmt.Sprintf("/%d", id), "", 401)
}
