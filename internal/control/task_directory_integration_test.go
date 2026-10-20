package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func testTaskDirectoryIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	ids := []int{}
	for i, status := range []string{"pending", "failed", "applied"} {
		expires := time.Now().Add(time.Hour)
		if i == 0 {
			expires = time.Now().Add(-time.Hour)
		}
		item, err := s.Store.Task.Create().SetDeviceID(987654321).SetOperation("directory.status").SetKey(randomToken(24)).SetPayload(map[string]any{"password": "must-not-return"}).SetRevision(1).SetStatus(status).SetExpiresAt(expires).SetResult(map[string]any{"message": "directory result"}).Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, item.ID)
		defer s.Store.Task.DeleteOneID(item.ID).Exec(ctx)
	}
	get := func(q, cookie string, code int) map[string]any {
		t.Helper()
		w := request("GET", "/api/v1/admin/tasks"+q, nil, "", cookie)
		if w.Code != code {
			t.Fatalf("task directory %s got %d want %d", q, w.Code, code)
		}
		var data map[string]any
		json.Unmarshal(w.Body.Bytes(), &data)
		return data
	}
	a := get("?device_id=987654321&page=1&page_size=1", admin, 200)
	b := get("?device_id=987654321&page=2&page_size=1", admin, 200)
	if a["total"].(float64) != 3 || a["items"].([]any)[0].(map[string]any)["id"] == b["items"].([]any)[0].(map[string]any)["id"] {
		t.Fatal("task pagination mismatch")
	}
	for _, status := range []string{"expired", "failed", "applied"} {
		data := get("?device_id=987654321&status="+status, admin, 200)
		if data["total"].(float64) != 1 {
			t.Fatal("task status mismatch")
		}
		item := data["items"].([]any)[0].(map[string]any)
		if item["status"] != status || item["payload"] != nil || item["expires_at"] == nil {
			t.Fatal("task view leaked payload or lacks status/time")
		}
	}
	if get("?device_id=987654321&status=pending", admin, 200)["total"].(float64) != 0 {
		t.Fatal("expired pending returned as runnable")
	}
	if get("?q=directory.status", admin, 200)["total"].(float64) != 3 {
		t.Fatal("operation search mismatch")
	}
	for _, q := range []string{"?device_id=bad", "?device_id=0", "?status=nope", "?page=0", "?page_size=201"} {
		get(q, admin, 400)
	}
	get("?page=1", alice, 403)
	get("?page=1", "", 401)
}
