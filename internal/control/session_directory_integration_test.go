package control

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testSessionDirectory(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	decode := func(w *httptest.ResponseRecorder, status int) map[string]any {
		t.Helper()
		if w.Code != status {
			t.Fatalf("session directory HTTP %d expected %d: %s", w.Code, status, w.Body.String())
		}
		d := map[string]any{}
		json.Unmarshal(w.Body.Bytes(), &d)
		return d
	}
	me := decode(request("GET", "/api/v1/me", nil, "", alice), 200)
	uid := int(me["id"].(float64))
	keys := []string{}
	for i := 0; i < 15; i++ {
		token := digest(fmt.Sprintf("session-directory-fixture-%d", i))
		key := "migate:session:" + token
		keys = append(keys, key)
		raw, _ := json.Marshal(Session{UserID: uid, CreatedAt: time.Now().Add(time.Duration(i) * time.Second), IP: fmt.Sprintf("192.0.2.%d", i), MFA: i%2 == 0})
		if e := s.Redis.Set(ctx, key, raw, time.Hour).Err(); e != nil {
			t.Fatal(e)
		}
	}
	defer s.Redis.Del(ctx, keys...)
	first := decode(request("GET", "/api/v1/sessions?page=1&page_size=10", nil, "", alice), 200)
	items := first["items"].([]any)
	if len(items) != 10 || items[0].(map[string]any)["current"] != true || first["total"].(float64) < 16 {
		t.Fatal("pagination or current-first failed")
	}
	next := decode(request("GET", "/api/v1/sessions?page=2&page_size=10", nil, "", alice), 200)
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.(map[string]any)["id"].(string)] = true
	}
	for _, item := range next["items"].([]any) {
		if seen[item.(map[string]any)["id"].(string)] {
			t.Fatal("duplicate page item")
		}
	}
	current := decode(request("GET", "/api/v1/sessions?page=1&scope=current", nil, "", alice), 200)
	if current["total"] != float64(1) {
		t.Fatal("current scope wrong")
	}
	search := decode(request("GET", "/api/v1/sessions?page=1&q=192.0.2.14", nil, "", alice), 200)
	if search["total"] != float64(1) {
		t.Fatal("search wrong")
	}
	empty := decode(request("GET", "/api/v1/sessions?page=999&q=no-match", nil, "", alice), 200)
	if empty["page"] != float64(1) || len(empty["items"].([]any)) != 0 {
		t.Fatal("empty paging failed")
	}
	for _, q := range []string{"page=0", "page_size=101", "scope=unknown", "q=%00"} {
		decode(request("GET", "/api/v1/sessions?"+q, nil, "", alice), 400)
	}
	base := fmt.Sprintf("/api/v1/admin/users/%d/sessions?page=1&page_size=10", uid)
	decode(request("GET", base, nil, "", admin), 200)
	decode(request("GET", base, nil, "", alice), 403)
	adminItems := decode(request("GET", "/api/v1/sessions", nil, "", admin), 200)["items"].([]any)
	foreign := adminItems[0].(map[string]any)["id"].(string)
	decode(request("DELETE", "/api/v1/sessions/"+foreign, nil, "", alice), 404)
	decode(request("DELETE", "/api/v1/sessions/bad", nil, "", alice), 400)
	decode(request("POST", "/api/v1/sessions/revoke-others", map[string]any{}, "", alice), 400)
	decode(request("POST", "/api/v1/sessions/revoke-others", map[string]any{"confirm": true}, "", ""), 401)
	one := strings.TrimPrefix(keys[0], "migate:session:")
	decode(request("DELETE", "/api/v1/sessions/"+one, nil, "", alice), 200)
	decode(request("DELETE", "/api/v1/sessions/"+one, nil, "", alice), 404)
	result := decode(request("POST", "/api/v1/sessions/revoke-others", map[string]any{"confirm": true}, "", alice), 200)
	if result["revoked"].(float64) < 14 {
		t.Fatal("other sessions not revoked")
	}
	remaining := decode(request("GET", "/api/v1/sessions?page=2", nil, "", alice), 200)
	if remaining["total"] != float64(1) || remaining["page"] != float64(1) {
		t.Fatal("current session lost or page did not clamp")
	}
	decode(request("GET", "/api/v1/me", nil, "", admin), 200)
	decode(request("GET", "/api/v1/me", nil, "", alice), 200)
	again := decode(request("POST", "/api/v1/sessions/revoke-others", map[string]any{"confirm": true}, "", alice), 200)
	if again["revoked"] != float64(0) {
		t.Fatal("repeat revoke changed state")
	}
	// A wrong-type record in the legacy namespace must fail the complete scan.
	root := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379"})
	defer root.Close()
	broken := "migate:session:broken-layout"
	if e := root.LPush(ctx, broken, "wrong-type").Err(); e != nil {
		t.Fatal(e)
	}
	defer root.Del(ctx, broken)
	decode(request("GET", "/api/v1/sessions?page=1", nil, "", alice), 503)
	decode(request("POST", "/api/v1/sessions/revoke-others", map[string]any{"confirm": true}, "", alice), 503)
	if e := root.Del(ctx, broken).Err(); e != nil {
		t.Fatal(e)
	}
	decode(request("GET", "/api/v1/me", nil, "", alice), 200)
}
