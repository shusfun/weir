package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func testProvenanceIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin string) {
	t.Helper()
	ctx := context.Background()
	owned := []int{}
	defer func() {
		for i := len(owned) - 1; i >= 0; i-- {
			s.Store.Resource.DeleteOneID(owned[i]).Exec(ctx)
		}
	}()
	create := func(kind string, data map[string]any) int {
		t.Helper()
		r, e := s.Store.Resource.Create().SetKind(kind).SetKey(randomToken(16)).SetData(data).Save(ctx)
		if e != nil {
			t.Fatal(e)
		}
		owned = append(owned, r.ID)
		return r.ID
	}
	link := "trojan://synthetic@example.com:443#original"
	sid := create("sources", map[string]any{"name": "source fixture", "group": "Z", "url": "https://example.com/sub", "links": []string{link}, "enabled": true, "last_update": time.Now().UTC().Format(time.RFC3339)})
	nid := create("nodes", map[string]any{"name": "manual fixture", "group": "A", "url": link})
	pid := create("profiles", map[string]any{"source_ids": []int{sid}, "node_ids": []int{nid}, "operators": []any{
		map[string]any{"type": "sort", "params": map[string]any{"keys": []any{map[string]any{"key": "group"}}}},
		map[string]any{"type": "rename", "params": map[string]any{"template": map[string]any{"enabled": true, "template": "{group}-{provider}-{originalName}"}}},
	}})
	w := request("POST", "/api/v1/admin/profiles/preview", map[string]any{"id": pid, "target": "links"}, "", admin)
	if w.Code != 200 {
		t.Fatal("provenance preview failed", w.Code)
	}
	var reply struct {
		Links []string `json:"links"`
	}
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || len(reply.Links) != 2 {
		t.Fatal("invalid preview")
	}
	if reply.Links[0] != "trojan://synthetic@example.com:443#A-manual%20fixture-original" || reply.Links[1] != "trojan://synthetic@example.com:443#Z-source%20fixture-original" {
		t.Fatal("occurrence source metadata lost")
	}
	for _, bad := range []any{12, true, "bad\nname"} {
		w = request("POST", "/api/v1/admin/resources/sources", map[string]any{"id": sid, "data": map[string]any{"name": "source fixture", "url": "https://example.com/sub", "group": bad}}, "", admin)
		if w.Code != 400 {
			t.Fatal("invalid group accepted")
		}
	}
	r, e := s.Store.Resource.Get(ctx, sid)
	if e != nil || r.Data["group"] != "Z" || r.Data["links"] == nil {
		t.Fatal("rejected source update changed inventory")
	}
}
