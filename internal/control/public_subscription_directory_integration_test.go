package control

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/imzyb/MiGate/ent/resource"
	"net/http/httptest"
	"strings"
	"testing"
)

func testPublicSubscriptionDirectory(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, ordinary string) {
	t.Helper()
	ctx := context.Background()
	owned := []int{}
	create := func(kind string, data map[string]any) int {
		r, e := s.Store.Resource.Create().SetKind(kind).SetKey(randomToken(12)).SetData(data).Save(ctx)
		if e != nil {
			t.Fatal(e)
		}
		owned = append(owned, r.ID)
		return r.ID
	}
	node := create("nodes", map[string]any{"url": "trojan://directory-only-fixture@example.com:443#Only"})
	profile := create("profiles", map[string]any{"name": "private inventory name", "enabled": true, "node_ids": []any{float64(node)}})
	slug := "directory-integration"
	page := create("public-page", map[string]any{"name": "private page name", "slug": slug, "title": "Public text", "enabled": true})
	s.Store.Resource.UpdateOneID(page).SetKey(slug).Save(ctx)
	defer func() {
		for _, id := range owned {
			s.Store.Resource.DeleteOneID(id).Exec(ctx)
		}
		for _, pair := range [][2]string{{"profile-publication", publicationKey(profile)}, {"page-publications", publicationKey(page)}} {
			s.Store.Resource.Delete().Where(resource.KindEQ(pair[0]), resource.KeyEQ(pair[1])).Exec(ctx)
		}
	}()
	read := func(path string) map[string]any {
		t.Helper()
		w := request("GET", path, nil, "", admin)
		if w.Code != 200 {
			t.Fatal("directory read", w.Code)
		}
		var d map[string]any
		json.Unmarshal(w.Body.Bytes(), &d)
		return d
	}
	publication := fmt.Sprintf("/api/v1/admin/profiles/%d/publication", profile)
	publish := func() {
		w := request("PUT", publication, map[string]any{"action": "publish", "target": "links", "days": 1, "revision": read(publication)["revision"], "confirm": true, "password": "local-testing-password"}, "", admin)
		if w.Code != 200 {
			t.Fatal("publish", w.Code)
		}
	}
	publish()
	endpoint := fmt.Sprintf("/api/v1/admin/public-pages/%d/subscriptions", page)
	public := "/api/v1/pages/" + slug
	download := public + fmt.Sprintf("/subscriptions/%d", profile)
	for _, method := range []string{"GET", "PUT"} {
		if request(method, endpoint, map[string]any{}, "", ordinary).Code != 403 {
			t.Fatal("ordinary directory boundary")
		}
	}
	if request("GET", download, nil, "", "").Code != 404 {
		t.Fatal("unselected inventory exposed")
	}
	doc := read(endpoint)
	choices := doc["choices"].([]any)
	choice := choices[0].(map[string]any)
	if choice["profile_id"] != float64(profile) || strings.Contains(fmt.Sprint(doc), "directory-only-fixture") {
		t.Fatal("choice fields")
	}
	entry := directoryPublication{profile, choice["generation"].(string), "Explicit public name"}
	body := map[string]any{"entries": []directoryPublication{entry}, "revision": doc["revision"], "confirm": true, "password": "wrong"}
	if request("PUT", endpoint, body, "", admin).Code != 401 {
		t.Fatal("directory reauth")
	}
	body["password"] = "local-testing-password"
	save := func(entries []directoryPublication) {
		t.Helper()
		body["entries"] = entries
		body["revision"] = read(endpoint)["revision"]
		w := request("PUT", endpoint, body, "", admin)
		if w.Code != 200 {
			t.Fatal("save", w.Code, w.Body.String())
		}
	}
	save([]directoryPublication{entry})
	if request("PUT", endpoint, map[string]any{"entries": []directoryPublication{}, "revision": doc["revision"], "confirm": true, "password": "local-testing-password"}, "", admin).Code != 409 {
		t.Fatal("stale directory")
	}
	w := request("GET", public, nil, "", "")
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	cards := result["subscriptions"].([]any)
	if len(cards) != 1 || len(cards[0].(map[string]any)) != 4 || cards[0].(map[string]any)["title"] != "Explicit public name" || strings.Contains(w.Body.String(), "private inventory") || strings.Contains(w.Body.String(), "generation") || strings.Contains(w.Body.String(), "token") {
		t.Fatal("public card projection")
	}
	if w = request("GET", download, nil, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "directory-only-fixture") {
		t.Fatal("directory actual output")
	}
	// Republishing cannot quietly replace content behind a previously confirmed public card.
	publish()
	if request("GET", download, nil, "", "").Code != 404 || strings.Contains(request("GET", public, nil, "", "").Body.String(), "subscriptions") {
		t.Fatal("generation binding bypass")
	}
	body["revision"] = read(endpoint)["revision"]
	if request("PUT", endpoint, body, "", admin).Code != 409 {
		t.Fatal("stale snapshot accepted")
	}
	newChoices := read(endpoint)["choices"].([]any)
	entry.Generation = newChoices[0].(map[string]any)["generation"].(string)
	save([]directoryPublication{entry})
	p, _ := s.Store.Resource.Get(ctx, page)
	p.Data["enabled"] = false
	s.Store.Resource.UpdateOneID(page).SetData(p.Data).Save(ctx)
	if request("GET", download, nil, "", "").Code != 404 {
		t.Fatal("withdrawn page download")
	}
	p.Data["enabled"] = true
	s.Store.Resource.UpdateOneID(page).SetData(p.Data).Save(ctx)
	save([]directoryPublication{})
	if request("GET", download, nil, "", "").Code != 404 {
		t.Fatal("removed card download")
	}
	save([]directoryPublication{entry})
	if request("DELETE", fmt.Sprintf("/api/v1/admin/resources/public-page/%d", page), nil, "", admin).Code != 200 {
		t.Fatal("delete page")
	}
	if exists, _ := s.Store.Resource.Query().Where(resource.KindEQ("page-publications"), resource.KeyEQ(publicationKey(page))).Exist(ctx); exists {
		t.Fatal("orphan directory")
	}
	if request("GET", download, nil, "", "").Code != 404 {
		t.Fatal("deleted page download")
	}
	for _, kind := range []string{"settings", "profile-publication", "page-publications"} {
		if request("DELETE", fmt.Sprintf("/api/v1/admin/resources/%s/%d", kind, profile), nil, "", admin).Code != 400 {
			t.Fatal("generic delete bypasses protected-kind boundary")
		}
	}
	t.Log("directory explicit reauth/CAS, no credentials in choices/cards, exact generation, actual anonymous snapshot, rotation/withdraw/remove/delete isolation passed")
}
