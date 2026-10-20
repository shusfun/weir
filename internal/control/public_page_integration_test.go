package control

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
)

func testPublicPageIntegration(t *testing.T, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	data := map[string]any{"name": "private operator label", "slug": "test-public-page", "title": "<script>attack()</script>", "description": "plain text", "notice": "test", "enabled": false, "show_login": false}
	save := func(id int) *httptest.ResponseRecorder {
		return request("POST", "/api/v1/admin/resources/public-page", map[string]any{"id": id, "data": data}, "", admin)
	}
	created := save(0)
	if created.Code != 200 {
		t.Fatal("page creation failed", created.Code)
	}
	var saved struct{ ID int }
	json.Unmarshal(created.Body.Bytes(), &saved)
	defer request("DELETE", "/api/v1/admin/resources/public-page/"+strconv.Itoa(saved.ID), nil, "", admin)
	if request("GET", "/api/v1/pages/test-public-page", nil, "", "").Code != 404 {
		t.Fatal("unpublished page visible")
	}
	data["enabled"] = true
	if save(saved.ID).Code != 200 {
		t.Fatal("publish failed")
	}
	public := request("GET", "/api/v1/pages/test-public-page", nil, "", "")
	var view map[string]any
	json.Unmarshal(public.Body.Bytes(), &view)
	if public.Code != 200 || len(view) != 11 || view["title"] != data["title"] || view["theme"] != "violet" || view["show_share"] != false {
		t.Fatal("public allowlist wrong")
	}

	if view["notice_type"] != "info" || view["notice_dismissible"] != false || view["notice_revision"] == "" {
		t.Fatal("legacy announcement defaults")
	}
	revision := view["notice_revision"]
	data["notice_title"], data["notice_type"], data["notice_dismissible"] = "Owned title", "warning", true
	if save(saved.ID).Code != 200 {
		t.Fatal("announcement update")
	}
	public = request("GET", "/api/v1/pages/test-public-page", nil, "", "")
	json.Unmarshal(public.Body.Bytes(), &view)
	if view["notice_revision"] == revision || view["notice_title"] != "Owned title" || view["notice_type"] != "warning" || view["notice_dismissible"] != true {
		t.Fatal("announcement revision/projection")
	}
	revision = view["notice_revision"]
	data["description"] = "unrelated description"
	if save(saved.ID).Code != 200 {
		t.Fatal("unrelated update")
	}
	public = request("GET", "/api/v1/pages/test-public-page", nil, "", "")
	json.Unmarshal(public.Body.Bytes(), &view)
	if view["notice_revision"] != revision {
		t.Fatal("unrelated content resets dismissal")
	}
	data["notice_enabled"] = false
	if save(saved.ID).Code != 200 {
		t.Fatal("announcement disable")
	}
	public = request("GET", "/api/v1/pages/test-public-page", nil, "", "")
	json.Unmarshal(public.Body.Bytes(), &view)
	if view["notice"] != "" {
		t.Fatal("disabled announcement visible")
	}
	delete(data, "notice_enabled")
	if save(0).Code != 409 {
		t.Fatal("duplicate public slug allowed")
	}
	data["password"] = "must-not-publish"
	if save(saved.ID).Code != 400 {
		t.Fatal("private page field accepted")
	}
	delete(data, "password")
	childData := map[string]any{"name": "never public operator label", "slug": "public-child", "title": "Published guide", "description": "Public content only", "notice": "private to child page", "enabled": true}
	childResponse := request("POST", "/api/v1/admin/resources/public-page", map[string]any{"data": childData}, "", admin)
	var child struct{ ID int }
	if childResponse.Code != 200 || json.Unmarshal(childResponse.Body.Bytes(), &child) != nil || child.ID <= 0 {
		t.Fatal("child page create failed")
	}
	defer request("DELETE", "/api/v1/admin/resources/public-page/"+strconv.Itoa(child.ID), nil, "", admin)
	data["page_ids"] = []int{child.ID}
	data["theme"], data["show_share"] = "forest", true
	if save(saved.ID).Code != 200 {
		t.Fatal("directory save failed")
	}
	public = request("GET", "/api/v1/pages/test-public-page", nil, "", "")
	json.Unmarshal(public.Body.Bytes(), &view)
	cards, ok := view["cards"].([]any)
	if public.Code != 200 || !ok || len(cards) != 1 || view["theme"] != "forest" || view["show_share"] != true || public.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("directory response incorrect")
	}
	card := cards[0].(map[string]any)
	if len(card) != 3 || card["title"] != childData["title"] || card["slug"] != childData["slug"] {
		t.Fatal("directory card leaked private fields")
	}
	childData["enabled"] = false
	if request("POST", "/api/v1/admin/resources/public-page", map[string]any{"id": child.ID, "data": childData}, "", admin).Code != 200 {
		t.Fatal("child withdrawal failed")
	}
	public = request("GET", "/api/v1/pages/test-public-page", nil, "", "")
	json.Unmarshal(public.Body.Bytes(), &view)
	if len(view["cards"].([]any)) != 0 {
		t.Fatal("withdrawn child leaked through directory")
	}
	data["page_ids"] = []int{saved.ID}
	if save(saved.ID).Code != 400 {
		t.Fatal("self-reference accepted")
	}
	data["page_ids"] = []int{2147483647}
	if save(saved.ID).Code != 400 {
		t.Fatal("unknown card reference accepted")
	}
	delete(data, "page_ids")
	if request("POST", "/api/v1/admin/resources/public-page", map[string]any{"data": data}, "", alice).Code != 403 {
		t.Fatal("ordinary user can publish")
	}
	data["enabled"] = false
	if save(saved.ID).Code != 200 || request("GET", "/api/v1/pages/test-public-page", nil, "", "").Code != 404 {
		t.Fatal("unpublish not effective")
	}
	t.Log("public directory theme/default compatibility, published-only cards, exact field allowlist, no-store, withdrawal, self/unknown rejection and ordinary-user isolation passed")
}
