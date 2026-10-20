package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

func testOperatorContractIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	bad := []any{map[string]any{"type": "dedup", "params": map[string]any{"unknown": true}}}
	data := map[string]any{"name": "strict-operator-fixture", "operators": bad}
	if w := request("POST", "/api/v1/admin/resources/operators", map[string]any{"data": data}, "", admin); w.Code != 400 {
		t.Fatal("invalid operator save accepted", w.Code)
	}
	if w := request("POST", "/api/v1/admin/resources/profiles", map[string]any{"data": data}, "", admin); w.Code != 400 {
		t.Fatal("invalid inline operator save accepted", w.Code)
	}
	if w := request("POST", "/api/v1/admin/subscriptions/preview", map[string]any{"links": []string{}, "operators": bad}, "", admin); w.Code != 422 {
		t.Fatal("empty preview ignored unsupported settings", w.Code)
	}
	valid := []any{map[string]any{"type": "dedup", "params": map[string]any{"includeProtocol": true}}}
	data["operators"] = valid
	w := request("POST", "/api/v1/admin/resources/operators", map[string]any{"data": data}, "", admin)
	var result struct {
		ID int `json:"id"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.ID == 0 {
		t.Fatal("compatible operator save failed", w.Code)
	}
	defer s.Store.Resource.DeleteOneID(result.ID).Exec(context.Background())
	data["operators"] = bad
	if w = request("POST", "/api/v1/admin/resources/operators", map[string]any{"id": result.ID, "data": data}, "", admin); w.Code != 400 {
		t.Fatal("invalid update accepted", w.Code)
	}
	stored, err := s.Store.Resource.Get(context.Background(), result.ID)
	if err != nil || fmt.Sprint(stored.Data["operators"]) != fmt.Sprint(valid) {
		t.Fatal("rejected edit altered original operator")
	}
	if w = request("POST", "/api/v1/admin/subscriptions/preview", map[string]any{"links": []string{}, "operators": valid}, "", alice); w.Code != 403 {
		t.Fatal("ordinary user can manage pipeline", w.Code)
	}
	t.Log("operator save/inline profile/empty preview reject unsupported params; valid camelCase saves; rejected update preserves data; ordinary user 403")
}
