package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testGuestbook(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, ordinary string) {
	t.Helper()
	ctx := context.Background()
	page := func(slug string) int {
		p, e := s.Store.Resource.Create().SetKind("public-page").SetKey(slug).SetData(map[string]any{"name": "owned guest", "slug": slug, "title": "Guest", "enabled": true}).Save(ctx)
		if e != nil {
			t.Fatal(e)
		}
		return p.ID
	}
	id := page("owned-guestbook")
	other := page("owned-guestbook-other")
	defer s.Store.Resource.DeleteOneID(other).Exec(ctx)
	base := fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook", id)
	pub := "/api/v1/pages/owned-guestbook/guestbook"
	decode := func(w *httptest.ResponseRecorder) map[string]any {
		var d map[string]any
		if json.Unmarshal(w.Body.Bytes(), &d) != nil {
			t.Fatal("bad JSON", w.Code)
		}
		return d
	}
	readConfig := func() map[string]any {
		w := request("GET", base+"/config", nil, "", admin)
		if w.Code != 200 {
			t.Fatal("config", w.Code)
		}
		return decode(w)
	}
	if request("GET", pub, nil, "", "").Code != 404 || request("GET", base+"/config", nil, "", ordinary).Code != 403 {
		t.Fatal("default disabled/permissions")
	}
	cfg := map[string]any{"enabled": true, "allow_anonymous": true, "require_audit": true}
	body := map[string]any{"config": cfg, "revision": readConfig()["revision"], "confirm": true, "password": "wrong"}
	if request("PUT", base+"/config", body, "", admin).Code != 401 {
		t.Fatal("reauth")
	}
	body["password"] = "local-testing-password"
	if request("PUT", base+"/config", body, "", admin).Code != 200 || request("PUT", base+"/config", body, "", admin).Code != 409 {
		t.Fatal("save/CAS")
	}
	post := map[string]any{"nickname": "访客", "content": "<script>not executed</script> hello", "type": "bug"}
	if request("POST", pub, post, "", "").Code != 201 {
		t.Fatal("submit")
	}
	if decode(request("GET", pub, nil, "", ""))["total"] != float64(0) {
		t.Fatal("pending leaked")
	}
	list := func() map[string]any { return decode(request("GET", base+"/messages", nil, "", admin)) }
	m := list()["items"].([]any)[0].(map[string]any)
	mid := int(m["id"].(float64))
	act := func(action, revision string) *httptest.ResponseRecorder {
		return request("POST", base+"/messages", map[string]any{"id": mid, "revision": revision, "action": action, "reply": "安全回复"}, "", admin)
	}
	if request("POST", fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook/messages", other), map[string]any{"id": mid, "revision": m["revision"], "action": "approve"}, "", admin).Code != 404 {
		t.Fatal("cross page")
	}
	if request("DELETE", fmt.Sprintf("/api/v1/admin/resources/guestbook-message/%d", mid), nil, "", admin).Code != 400 {
		t.Fatal("generic bypass")
	}
	if act("approve", m["revision"].(string)).Code != 200 || act("hide", m["revision"].(string)).Code != 409 {
		t.Fatal("moderation/CAS")
	}
	public := decode(request("GET", pub, nil, "", ""))["items"].([]any)[0].(map[string]any)
	if len(public) != 8 || public["revision"] != nil || public["page_id"] != nil || public["content"] != post["content"] {
		t.Fatal("public whitelist")
	}
	m = list()["items"].([]any)[0].(map[string]any)
	if act("reply", m["revision"].(string)).Code != 200 {
		t.Fatal("reply")
	}
	m = list()["items"].([]any)[0].(map[string]any)
	if m["status"] != "replied" || m["reply_at"] == nil {
		t.Fatal("reply status")
	}
	if act("hide", m["revision"].(string)).Code != 200 || decode(request("GET", pub, nil, "", ""))["total"] != float64(0) {
		t.Fatal("hide")
	}
	// Bounded pagination/search on real stored records, without weakening public rate limits.
	for i := 0; i < 22; i++ {
		msg := guestbookMessage{PageID: id, Nickname: "pagination", Content: fmt.Sprint("record ", i), Type: "general", Status: "approved", Visible: true, CreatedAt: time.Now().UTC()}
		raw, _ := json.Marshal(msg)
		if _, e := s.DB.ExecContext(ctx, `INSERT INTO migate.resources(kind,key,data,owner_id,created_at) VALUES('guestbook-message',$1,$2,0,NOW())`, randomToken(12), raw); e != nil {
			t.Fatal(e)
		}
	}
	w := request("GET", base+"/messages?page=2&search=pagination", nil, "", admin)
	d := decode(w)
	if w.Code != 200 || d["total"] != float64(22) || len(d["items"].([]any)) != 2 {
		t.Fatal("pagination")
	}
	cfg["allow_anonymous"] = false
	body["revision"] = readConfig()["revision"]
	if request("PUT", base+"/config", body, "", admin).Code != 200 || request("POST", pub, post, "", "").Code != 401 || request("POST", pub, post, "", ordinary).Code != 201 {
		t.Fatal("login required")
	}
	if request("POST", base+"/messages", map[string]any{}, "", ordinary).Code != 403 {
		t.Fatal("ordinary moderation")
	}
	// Invalid types/length/unknown fields are explicit refusals.
	post["content"] = strings.Repeat("字", 501)
	if request("POST", pub, post, "", ordinary).Code != 400 {
		t.Fatal("content bounds")
	}
	s.Store.Resource.UpdateOneID(id).SetData(map[string]any{"slug": "owned-guestbook", "title": "Guest", "enabled": false}).Save(ctx)
	if request("GET", pub, nil, "", "").Code != 404 || request("POST", pub, map[string]any{"content": "hello"}, "", ordinary).Code != 404 {
		t.Fatal("withdraw")
	}
	if request("POST", pub, map[string]any{"content": "sixth"}, "", ordinary).Code != 404 || request("POST", pub, map[string]any{"content": "seventh"}, "", ordinary).Code != 429 {
		t.Fatal("submission rate limit")
	}
	if request("DELETE", fmt.Sprintf("/api/v1/admin/resources/public-page/%d", id), nil, "", admin).Code != 200 {
		t.Fatal("delete page")
	}
	var count int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM migate.resources WHERE (kind='guestbook-config' AND key=$1) OR (kind='guestbook-message' AND data->>'page_id'=$1)`, strconv.Itoa(id)).Scan(&count)
	if count != 0 {
		t.Fatal("orphans")
	}
	t.Log("guestbook disabled default, reauth/CAS, pending whitelist, cross-page and ordinary guards, approve/reply/hide, search/pagination, login-only submission, size refusal, withdrawal and atomic owned cleanup passed")
}
