package control

import (
	"context"
	"encoding/json"
	"github.com/imzyb/MiGate/ent/resource"
	"net/http/httptest"
	"strings"
	"testing"
)

func testSubscriptionDefaults(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, ordinary string) {
	t.Helper()
	ctx := context.Background()
	defer s.Store.Resource.Delete().Where(resource.KindEQ("settings"), resource.KeyEQ("subscription-defaults")).Exec(ctx)
	endpoint := "/api/v1/admin/subscription-defaults"
	read := func() map[string]any {
		t.Helper()
		w := request("GET", endpoint, nil, "", admin)
		if w.Code != 200 {
			t.Fatal("default read", w.Code)
		}
		var d map[string]any
		json.Unmarshal(w.Body.Bytes(), &d)
		return d
	}
	if request("GET", endpoint, nil, "", ordinary).Code != 403 {
		t.Fatal("default access")
	}
	operators := []any{map[string]any{"type": "rename", "params": map[string]any{"template": map[string]any{"enabled": true, "template": "Global-{name}"}}}}
	body := map[string]any{"defaults": map[string]any{"operators": operators}, "revision": read()["revision"], "confirm": true, "password": "wrong"}
	if request("PUT", endpoint, body, "", admin).Code != 401 {
		t.Fatal("default reauth")
	}
	body["password"] = "local-testing-password"
	if request("PUT", endpoint, body, "", ordinary).Code != 403 || request("PUT", endpoint, body, "", admin).Code != 200 {
		t.Fatal("default save")
	}
	if request("PUT", endpoint, body, "", admin).Code != 409 {
		t.Fatal("default conflict")
	}
	n, e := s.Store.Resource.Create().SetKind("nodes").SetKey(randomToken(12)).SetData(map[string]any{"url": "trojan://defaults-fixture@example.com:443#Original"}).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Store.Resource.DeleteOneID(n.ID).Exec(ctx)
	data := map[string]any{"enabled": true, "node_ids": []any{float64(n.ID)}}
	p, e := s.Store.Resource.Create().SetKind("profiles").SetKey(randomToken(12)).SetData(data).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Store.Resource.DeleteOneID(p.ID).Exec(ctx)
	preview := func(want string) {
		t.Helper()
		out, status, code := s.renderProfile(ctx, p.ID, "links")
		if status != 200 || code != "" || !strings.Contains(out["output"].(string), want) {
			t.Fatal("default preview", status, code)
		}
	}
	preview("Global-Original")
	data["skip_default_operators"] = true
	s.Store.Resource.UpdateOneID(p.ID).SetData(data).Save(ctx)
	preview("#Original")
	data["skip_default_operators"] = false
	data["operators"] = []any{map[string]any{"type": "rename", "params": map[string]any{"template": map[string]any{"enabled": true, "template": "Own-{name}"}}}}
	s.Store.Resource.UpdateOneID(p.ID).SetData(data).Save(ctx)
	preview("Own-Original")
	r, _ := s.Store.Resource.Query().Where(resource.KindEQ("settings"), resource.KeyEQ("subscription-defaults")).Only(ctx)
	s.Store.Resource.UpdateOneID(r.ID).SetData(map[string]any{"operators": "corrupt"}).Save(ctx)
	if request("GET", endpoint, nil, "", admin).Code != 503 {
		t.Fatal("corrupt defaults read")
	}
	delete(data, "operators")
	s.Store.Resource.UpdateOneID(p.ID).SetData(data).Save(ctx)
	if _, status, _ := s.renderProfile(ctx, p.ID, "links"); status != 422 {
		t.Fatal("corrupt default silently skipped")
	}
	data["skip_default_operators"] = true
	s.Store.Resource.UpdateOneID(p.ID).SetData(data).Save(ctx)
	preview("#Original")
	body["defaults"] = map[string]any{"operators": []any{}, "unknown": true}
	if request("PUT", endpoint, body, "", admin).Code != 400 {
		t.Fatal("unknown default field")
	}
	t.Log("defaults admin reauth/CAS, inherited chain, independent whole-chain override, explicit empty skip, corrupt read/output fail closed and unknown-field rejection passed")
}
