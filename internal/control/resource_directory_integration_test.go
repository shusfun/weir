package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
)

func testResourceDirectoryIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	for _, name := range []string{"directory_100% Alpha", "directoryX100Y Beta", "directory-third"} {
		r, err := s.Store.Resource.Create().SetKind("nodes").SetKey(randomToken(16)).SetData(map[string]any{"name": name, "url": "socks5://example.com:1080"}).Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Store.Resource.DeleteOneID(r.ID).Exec(ctx)
	}
	get := func(q, cookie string, code int) map[string]any {
		t.Helper()
		w := request("GET", "/api/v1/admin/resources/nodes"+q, nil, "", cookie)
		if w.Code != code {
			t.Fatalf("resources %s: got %d want %d", q, w.Code, code)
		}
		var d map[string]any
		json.Unmarshal(w.Body.Bytes(), &d)
		return d
	}
	a := get("?q=directory&page=1&page_size=1", admin, 200)
	b := get("?q=directory&page=2&page_size=1", admin, 200)
	if a["total"].(float64) != 3 || a["items"].([]any)[0].(map[string]any)["id"] == b["items"].([]any)[0].(map[string]any)["id"] {
		t.Fatal("resource pagination mismatch")
	}
	for _, q := range []string{"ALPHA", "directory_100%"} {
		d := get("?q="+url.QueryEscape(q), admin, 200)
		if len(d["items"].([]any)) != 1 {
			t.Fatal("literal/case-insensitive query mismatch")
		}
		item := d["items"].([]any)[0].(map[string]any)
		node := item["node"].(map[string]any)
		if node["valid"] != true || node["protocol"] != "socks5" || node["server"] != "example.com" || len(node) != 4 {
			t.Fatal("node directory metadata mismatch or credential leak")
		}
	}
	for _, q := range []string{"?page=0", "?page_size=501", "?owner_id=oops", "?owner_id=-1"} {
		get(q, admin, 400)
	}
	get("?page=1", alice, 403)
	get("?page=1", "", 401)
}
