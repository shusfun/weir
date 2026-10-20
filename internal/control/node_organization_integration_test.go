package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func testNodeOrganization(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	ids := []int{}
	for _, name := range []string{"one", "two"} {
		r, e := s.Store.Resource.Create().SetKind("nodes").SetKey(randomToken(16)).SetData(map[string]any{"name": name, "url": "trojan://test@example.com:443?sni=example.com"}).Save(ctx)
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, r.ID)
		defer s.Store.Resource.DeleteOne(r).Exec(ctx)
	}
	call := func(endpoint string, body any, cookie string, status int) map[string]any {
		w := request("POST", "/api/v1/admin/nodes/"+endpoint, body, "", cookie)
		if w.Code != status {
			t.Fatalf("node organization %s HTTP %d want %d", endpoint, w.Code, status)
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	call("order", map[string]any{"id": ids[0], "other_id": ids[1]}, alice, 403)
	call("order", map[string]any{"id": ids[0], "other_id": ids[0]}, admin, 400)
	call("order", map[string]any{"id": ids[0], "other_id": ids[1]}, admin, 200)
	for i, id := range ids {
		r, e := s.Store.Resource.Get(ctx, id)
		if e != nil || number(r.Data["position"]) != -float64(ids[1-i]) || text(r.Data["url"]) != "trojan://test@example.com:443?sni=example.com" {
			t.Fatal("order persistence/credential preservation failed")
		}
	}
	w := request("POST", "/api/v1/admin/resources/nodes", map[string]any{"id": ids[0], "data": map[string]any{"name": "edited", "url": "trojan://test@example.com:443?sni=example.com", "position": 999}}, "", admin)
	if w.Code != 200 {
		t.Fatalf("node edit HTTP %d", w.Code)
	}
	rEdit, _ := s.Store.Resource.Get(ctx, ids[0])
	if number(rEdit.Data["position"]) != -float64(ids[1]) {
		t.Fatal("editor overwrote server-owned order")
	}
	out := call("group", map[string]any{"ids": []int{ids[0], 2147483647}, "group": " test "}, admin, 200)
	rows := out["items"].([]any)
	if rows[0].(map[string]any)["ok"] != true || rows[1].(map[string]any)["ok"] != false {
		t.Fatal("partial group status lost")
	}
	r, _ := s.Store.Resource.Get(ctx, ids[0])
	if text(r.Data["group"]) != "test" || number(r.Data["position"]) != -float64(ids[1]) {
		t.Fatal("group overwrote presentation")
	}
	call("group", map[string]any{"ids": ids, "group": ""}, alice, 403)
	call("group", map[string]any{"ids": ids, "group": ""}, "", 401)
}
