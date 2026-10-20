package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func testAuditDirectoryIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	for _, fixture := range []struct{ action, target, result string }{
		{"acceptance.directory_100%", "task-1", "ok"},
		{"acceptance.directoryX100Y", "task-2", "denied"},
		{"acceptance.directory.other", "task-3", "ok"},
	} {
		r, err := s.Store.Audit.Create().SetActor(0).SetAction(fixture.action).SetTarget(fixture.target).SetResult(fixture.result).Save(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer s.Store.Audit.DeleteOneID(r.ID).Exec(context.Background())
	}
	get := func(q, cookie string, status int) map[string]any {
		t.Helper()
		w := request("GET", "/api/v1/admin/audit"+q, nil, "", cookie)
		if w.Code != status {
			t.Fatalf("audit directory HTTP %d want %d", w.Code, status)
		}
		var body map[string]any
		json.Unmarshal(w.Body.Bytes(), &body)
		return body
	}
	a := get("?q=acceptance.directory&page=1&page_size=1", admin, 200)
	b := get("?q=acceptance.directory&page=2&page_size=1", admin, 200)
	if a["total"].(float64) != 3 || a["items"].([]any)[0].(map[string]any)["id"] == b["items"].([]any)[0].(map[string]any)["id"] {
		t.Fatal("audit pagination skipped or duplicated records")
	}
	if get("?q=DIRECTORY_100%25", admin, 200)["total"].(float64) != 1 {
		t.Fatal("audit wildcard search not literal / case insensitive")
	}
	if get("?q=acceptance.directory&result=denied&actor=0", admin, 200)["total"].(float64) != 1 {
		t.Fatal("combined audit filters not applied")
	}
	if get("?q=task-3", admin, 200)["total"].(float64) != 1 {
		t.Fatal("audit target search failed")
	}
	for _, q := range []string{"?page=0", "?page_size=201", "?page=oops", "?actor=-1", "?actor=oops"} {
		get(q, admin, 400)
	}
	get("?q=acceptance.directory", alice, 403)
	get("?q=acceptance.directory", "", 401)
}
