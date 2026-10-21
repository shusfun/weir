package control

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/imzyb/MiGate/ent/resource"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testProfilePublication(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, ordinary string) {
	t.Helper()
	ctx := context.Background()
	node, e := s.Store.Resource.Create().SetKind("nodes").SetKey(randomToken(16)).SetData(map[string]any{"name": "share synthetic", "url": "trojan://synthetic-share-password@example.com:443#Shared"}).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	profile, e := s.Store.Resource.Create().SetKind("profiles").SetKey(randomToken(16)).SetData(map[string]any{"name": "share test", "enabled": true, "node_ids": []any{float64(node.ID)}}).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Store.Resource.DeleteOneID(node.ID).Exec(ctx)
	defer s.Store.Resource.DeleteOneID(profile.ID).Exec(ctx)
	defer s.Store.Resource.Delete().Where(resource.KindEQ("profile-publication"), resource.KeyEQ(publicationKey(profile.ID))).Exec(ctx)
	url := fmt.Sprintf("/api/v1/admin/profiles/%d/publication", profile.ID)
	read := func() map[string]any {
		t.Helper()
		w := request("GET", url, nil, "", admin)
		if w.Code != 200 {
			t.Fatal("read", w.Code)
		}
		var d map[string]any
		json.Unmarshal(w.Body.Bytes(), &d)
		if strings.Contains(w.Body.String(), "synthetic-share-password") || strings.Contains(w.Body.String(), "token_hash") || strings.Contains(w.Body.String(), "sealed_output") {
			t.Fatal("publication secret projection")
		}
		return d
	}
	if request("GET", url, nil, "", ordinary).Code != 403 {
		t.Fatal("ordinary metadata")
	}
	body := map[string]any{"action": "publish", "target": "clash", "days": 1, "revision": read()["revision"], "confirm": true, "password": "Local1-testing-password"}
	if request("PUT", url, body, "", ordinary).Code != 403 {
		t.Fatal("ordinary publish")
	}
	body["password"] = "wrong"
	if request("PUT", url, body, "", admin).Code != 401 {
		t.Fatal("reauth bypass")
	}
	body["password"] = "Local1-testing-password"
	publish := func() string {
		t.Helper()
		body["revision"] = read()["revision"]
		w := request("PUT", url, body, "", admin)
		if w.Code != 200 {
			t.Fatal("publish", w.Code, w.Body.String())
		}
		var d map[string]any
		json.Unmarshal(w.Body.Bytes(), &d)
		u := d["url"].(string)
		return "/share/" + u[strings.LastIndex(u, "/")+1:]
	}
	share := publish()
	if request("PUT", url, body, "", admin).Code != 409 {
		t.Fatal("stale publish")
	}
	w := request("GET", share, nil, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "synthetic-share-password") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("actual share output", w.Code)
	}
	// A later node edit must not silently alter the published snapshot.
	s.Store.Resource.UpdateOneID(node.ID).SetData(map[string]any{"url": "trojan://edited-password@example.com:443#Edited"}).Save(ctx)
	if !strings.Contains(request("GET", share, nil, "", "").Body.String(), "synthetic-share-password") {
		t.Fatal("snapshot changed")
	}
	p, _ := s.Store.Resource.Query().Where(resource.KindEQ("profile-publication"), resource.KeyEQ(publicationKey(profile.ID))).Only(ctx)
	raw, _ := json.Marshal(p.Data)
	if strings.Contains(string(raw), "synthetic-share-password") || strings.Contains(string(raw), strings.TrimPrefix(share, "/share/")) {
		t.Fatal("plaintext at rest")
	}
	view := read()
	if view["total"] != float64(2) {
		t.Fatal("actual visit count")
	}
	visits := view["visits"].([]any)
	if len(visits[0].(map[string]any)) != 3 {
		t.Fatal("visit secret fields")
	}
	// Corrupt encrypted content and denied record persistence cannot serve a snapshot.
	p, _ = s.Store.Resource.Get(ctx, p.ID)
	original := p.Data["sealed_output"]
	p.Data["sealed_output"] = "invalid"
	s.Store.Resource.UpdateOneID(p.ID).SetData(p.Data).Save(ctx)
	if request("GET", share, nil, "", "").Code != 503 {
		t.Fatal("corrupt snapshot served")
	}
	p.Data["sealed_output"] = original
	s.Store.Resource.UpdateOneID(p.ID).SetData(p.Data).Save(ctx)
	// More than one page, deterministic newest-first, bounded retained list.
	raw, _ = json.Marshal(p.Data)
	var pub profilePublication
	json.Unmarshal(raw, &pub)
	pub.Visits = nil
	for i := 0; i < 205; i++ {
		pub.Visits = append(pub.Visits, publicationVisit{At: time.Now().Unix(), Result: "served", Target: "clash"})
	}
	raw, _ = json.Marshal(pub)
	var dataMap map[string]any
	json.Unmarshal(raw, &dataMap)
	s.Store.Resource.UpdateOneID(p.ID).SetData(dataMap).Save(ctx)
	if request("GET", share, nil, "", "").Code != 200 || read()["total"] != float64(200) {
		t.Fatal("unbounded visit retention")
	}
	if request("GET", url+"?page=2", nil, "", admin).Code != 200 {
		t.Fatal("visit pagination")
	}

	next := publish()
	if request("GET", share, nil, "", "").Code != 404 || request("GET", next, nil, "", "").Code != 200 {
		t.Fatal("token rotation")
	}
	data := profile.Data
	data["enabled"] = false
	s.Store.Resource.UpdateOneID(profile.ID).SetData(data).Save(ctx)
	if request("GET", next, nil, "", "").Code != 404 {
		t.Fatal("disabled share")
	}
	data["enabled"] = true
	s.Store.Resource.UpdateOneID(profile.ID).SetData(data).Save(ctx)
	p, _ = s.Store.Resource.Get(ctx, p.ID)
	p.Data["expires_at"] = time.Now().Add(-time.Second).Unix()
	s.Store.Resource.UpdateOneID(p.ID).SetData(p.Data).Save(ctx)
	if request("GET", next, nil, "", "").Code != 404 {
		t.Fatal("expired share")
	}
	next = publish()
	body["action"] = "revoke"
	body["revision"] = read()["revision"]
	if request("PUT", url, body, "", admin).Code != 200 || request("GET", next, nil, "", "").Code != 404 {
		t.Fatal("revocation")
	}
	body["action"] = "publish"
	next = publish()
	if request("DELETE", fmt.Sprintf("/api/v1/admin/resources/profiles/%d", profile.ID), nil, "", admin).Code != 200 {
		t.Fatal("delete profile")
	}
	if exists, _ := s.Store.Resource.Query().Where(resource.KindEQ("profile-publication"), resource.KeyEQ(publicationKey(profile.ID))).Exist(ctx); exists {
		t.Fatal("orphan snapshot")
	}
	if request("GET", next, nil, "", "").Code != 404 {
		t.Fatal("deleted profile")
	}
	if request("GET", "/share/"+strings.Repeat("z", 64), nil, "", "").Code != 404 {
		t.Fatal("invalid token")
	}
}
