package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func testProfileOrder(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	decode := func(w *httptest.ResponseRecorder, status int) profileOrderDocument {
		t.Helper()
		if w.Code != status {
			t.Fatalf("profile order HTTP %d want %d: %s", w.Code, status, w.Body.String())
		}
		var d profileOrderDocument
		json.Unmarshal(w.Body.Bytes(), &d)
		return d
	}
	owned := []int{}
	for _, name := range []string{"order first", "order second"} {
		r, e := s.Store.Resource.Create().SetKind("profiles").SetKey(randomToken(16)).SetData(map[string]any{"name": name, "node_ids": []int{}, "source_ids": []int{}, "enabled": true}).Save(ctx)
		if e != nil {
			t.Fatal(e)
		}
		owned = append(owned, r.ID)
	}
	defer func() {
		for _, id := range owned {
			s.Store.Resource.DeleteOneID(id).Exec(ctx)
		}
	}()
	decode(request("GET", "/api/v1/admin/profiles/order", nil, "", alice), 403)
	decode(request("GET", "/api/v1/admin/profiles/order", nil, "", ""), 401)
	base := decode(request("GET", "/api/v1/admin/profiles/order", nil, "", admin), 200)
	ids := []int{}
	for i := len(base.Items) - 1; i >= 0; i-- {
		ids = append(ids, base.Items[i].ID)
	}
	req := map[string]any{"ids": ids, "revision": base.Revision}
	decode(request("POST", "/api/v1/admin/profiles/order", req, "", alice), 403)
	saved := decode(request("POST", "/api/v1/admin/profiles/order", req, "", admin), 200)
	for i, id := range ids {
		if saved.Items[i].ID != id {
			t.Fatal("order not applied")
		}
	}
	decode(request("POST", "/api/v1/admin/profiles/order", req, "", admin), 409)
	decode(request("POST", "/api/v1/admin/profiles/order", map[string]any{"ids": []int{owned[0], owned[0]}, "revision": saved.Revision}, "", admin), 400)
	// An old editor may change profile content but must preserve latest order.
	r, _ := s.Store.Resource.Get(ctx, owned[0])
	position := r.Data["position"]
	update := request("POST", "/api/v1/admin/resources/profiles", map[string]any{"id": r.ID, "data": map[string]any{"name": "edited", "enabled": true, "node_ids": []int{}, "source_ids": []int{}, "position": 9999}}, "", admin)
	if update.Code != 200 {
		t.Fatal(update.Body.String())
	}
	r, _ = s.Store.Resource.Get(ctx, r.ID)
	if r.Data["position"] != position || r.Data["name"] != "edited" {
		t.Fatal("editor overwrote order")
	}
	// New membership invalidates the complete order snapshot; no silent omission.
	newer, e := s.Store.Resource.Create().SetKind("profiles").SetKey(randomToken(16)).SetData(map[string]any{"name": "concurrent"}).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	owned = append(owned, newer.ID)
	decode(request("POST", "/api/v1/admin/profiles/order", map[string]any{"ids": ids, "revision": saved.Revision}, "", admin), 409)
	fresh := decode(request("GET", "/api/v1/admin/profiles/order", nil, "", admin), 200)
	freshIDs := []int{}
	for _, p := range fresh.Items {
		freshIDs = append(freshIDs, p.ID)
	}
	decode(request("POST", "/api/v1/admin/profiles/order", map[string]any{"ids": freshIDs, "revision": fresh.Revision, "extra": true}, "", admin), 400)
	// Original references and enable state are not rewritten by ordering.
	r, _ = s.Store.Resource.Get(ctx, owned[1])
	if r.Data["enabled"] != true || len(r.Data["node_ids"].([]any)) != 0 {
		t.Fatal("order changed business fields")
	}
}
