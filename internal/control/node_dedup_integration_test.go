package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func testInventoryDedup(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	ids := []int{}
	for i, item := range []map[string]any{
		{"name": "discard-long-display-name", "url": "trojan://private-fixture@dedup.example:443?sni=one.example#one"},
		{"name": "keep", "group": "grouped", "url": "trojan://private-fixture@dedup.example:443?sni=one.example#two"},
		{"name": "different-sni", "url": "trojan://private-fixture@dedup.example:443?sni=two.example#three"},
	} {
		r, e := s.Store.Resource.Create().SetKind("nodes").SetKey(randomToken(16)).SetData(item).Save(ctx)
		if e != nil {
			t.Fatal(i, e)
		}
		ids = append(ids, r.ID)
		defer s.Store.Resource.DeleteOne(r).Exec(ctx)
	}
	profile, e := s.Store.Resource.Create().SetKind("profiles").SetKey(randomToken(16)).SetData(map[string]any{"name": "dedup-reference", "node_ids": []int{ids[0], ids[1], ids[2]}, "user_ids": []int{1234}, "enabled": true}).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Store.Resource.DeleteOne(profile).Exec(ctx)
	call := func(body any, cookie string, status int) map[string]any {
		w := request("POST", "/api/v1/admin/nodes/dedup", body, "", cookie)
		if w.Code != status {
			t.Fatalf("inventory dedup HTTP %d want %d", w.Code, status)
		}
		if strings.Contains(w.Body.String(), "private-fixture") {
			t.Fatal("dedup preview exposed credential")
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	call(map[string]any{"preview": true}, alice, 403)
	call(map[string]any{"preview": true}, "", 401)
	preview := call(map[string]any{"preview": true}, admin, 200)
	n, _ := s.Store.Resource.Get(ctx, ids[0])
	data := n.Data
	data["notes"] = "changed"
	s.Store.Resource.UpdateOne(n).SetData(data).Save(ctx)
	call(map[string]any{"snapshot": preview["snapshot"]}, admin, 409)
	if _, e = s.Store.Resource.Get(ctx, ids[0]); e != nil {
		t.Fatal("stale preview deleted data")
	}
	preview = call(map[string]any{"preview": true}, admin, 200)
	found := false
	for _, v := range preview["items"].([]any) {
		r := v.(map[string]any)
		if int(number(r["remove_id"])) == ids[0] && int(number(r["keep_id"])) == ids[1] {
			found = true
		}
	}
	if !found {
		t.Fatal("upstream metadata priority not retained")
	}
	call(map[string]any{"snapshot": preview["snapshot"]}, admin, 200)
	if _, e = s.Store.Resource.Get(ctx, ids[0]); e == nil {
		t.Fatal("duplicate not removed")
	}
	if _, e = s.Store.Resource.Get(ctx, ids[2]); e != nil {
		t.Fatal("different SNI lost")
	}
	profile, e = s.Store.Resource.Get(ctx, profile.ID)
	if e != nil {
		t.Fatal(e)
	}
	refs := profile.Data["node_ids"].([]any)
	if len(refs) != 2 || int(number(refs[0])) != ids[1] || int(number(refs[1])) != ids[2] || int(number(profile.Data["user_ids"].([]any)[0])) != 1234 {
		t.Fatal("references or authorization changed")
	}
}
