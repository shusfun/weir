package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/imzyb/MiGate/ent/resource"
)

func testNodeGroups(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, ordinary string) {
	t.Helper()
	ctx := context.Background()
	defer s.Store.Resource.Delete().Where(resource.KindEQ("settings"), resource.KeyEQ("node-group-order")).Exec(ctx)
	ids := []int{}
	original := map[int]map[string]any{}
	for _, group := range []string{"Group A", "Group A", "Group B"} {
		r := s.Store.Resource.Create().SetKind("nodes").SetKey(randomToken(16)).SetData(map[string]any{"name": "fixture", "url": "trojan://private@example.com:443?sni=example.com", "group": group, "notes": "preserve", "position": 42}).SaveX(ctx)
		ids = append(ids, r.ID)
		original[r.ID] = r.Data
		defer s.Store.Resource.DeleteOne(r).Exec(ctx)
	}
	p := s.Store.Resource.Create().SetKind("profiles").SetKey(randomToken(16)).SetData(map[string]any{"name": "group fixture", "node_ids": ids}).SaveX(ctx)
	defer s.Store.Resource.DeleteOne(p).Exec(ctx)
	read := func() nodeGroupsDocument {
		t.Helper()
		w := request("GET", "/api/v1/admin/nodes/groups", nil, "", admin)
		var d nodeGroupsDocument
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil {
			t.Fatal("groups read", w.Code)
		}
		return d
	}
	if request("GET", "/api/v1/admin/nodes/groups", nil, "", ordinary).Code != 403 || request("GET", "/api/v1/admin/nodes/groups", nil, "", "").Code != 401 {
		t.Fatal("groups access boundary")
	}
	call := func(body map[string]any, status int) {
		t.Helper()
		w := request("POST", "/api/v1/admin/nodes/groups", body, "", admin)
		if w.Code != status {
			t.Fatal("groups change", w.Code, status)
		}
	}
	d := read()
	body := map[string]any{"operation": "rename", "name": "Group A", "new_name": "Group C", "revision": d.Revision}
	if request("POST", "/api/v1/admin/nodes/groups", body, "", ordinary).Code != 403 {
		t.Fatal("ordinary mutation")
	}
	call(map[string]any{"operation": "rename", "name": "Group A", "new_name": "Group B", "revision": d.Revision}, 409)
	call(map[string]any{"operation": "rename", "name": "Group A", "new_name": " \n ", "revision": d.Revision}, 400)
	call(map[string]any{"operation": "rename", "name": "Group A", "new_name": "C", "revision": d.Revision, "unknown": 1}, 400)
	call(body, 200)
	call(body, 409)
	for _, id := range ids {
		r := s.Store.Resource.GetX(ctx, id)
		expected := original[id]
		if expected["group"] == "Group A" {
			expected["group"] = "Group C"
		}
		left, _ := json.Marshal(r.Data)
		right, _ := json.Marshal(expected)
		if string(left) != string(right) {
			t.Fatal("group rename overwrote node fields")
		}
	}
	after := read()
	names := []string{}
	for i := len(after.Groups) - 1; i >= 0; i-- {
		names = append(names, after.Groups[i].Name)
	}
	call(map[string]any{"operation": "order", "order": names, "revision": after.Revision}, 200)
	saved := read()
	for i, g := range saved.Groups {
		if g.Name != names[i] {
			t.Fatal("order not persistent")
		}
	}
	call(map[string]any{"operation": "order", "order": []string{"Group C", "Group C"}, "revision": saved.Revision}, 400)
	// A new member invalidates the prior snapshot, including unchanged group counts elsewhere.
	s.Store.Resource.UpdateOneID(ids[2]).SetData(map[string]any{"group": "Group C", "name": "fixture", "url": "trojan://private@example.com:443?sni=example.com", "notes": "preserve", "position": 42}).SaveX(ctx)
	call(map[string]any{"operation": "dissolve", "name": "Group C", "revision": saved.Revision}, 409)
	current := read()
	call(map[string]any{"operation": "dissolve", "name": "Group C", "revision": current.Revision}, 200)
	for _, id := range ids {
		r := s.Store.Resource.GetX(ctx, id)
		if r.Data["group"] != "" || r.Data["url"] != original[id]["url"] || r.Data["notes"] != "preserve" {
			t.Fatal("dissolve deleted or changed node")
		}
	}
	savedProfile, _ := json.Marshal(s.Store.Resource.GetX(ctx, p.ID).Data)
	originalProfile, _ := json.Marshal(p.Data)
	if string(savedProfile) != string(originalProfile) {
		t.Fatal("group management changed profile references")
	}
	for _, name := range []string{"", " ", "a\nb", " leading", "trailing "} {
		if validGroupName(name) {
			t.Fatal("invalid group name")
		}
	}
	if !validGroupName("香港 · 线路 A") {
		t.Fatal("valid group rejected")
	}
}
