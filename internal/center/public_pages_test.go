package center

import (
	"fmt"
	"strings"
	"testing"

	"github.com/imzyb/MiGate/internal/store/ent/resource"
)

func TestPublicPageCardsAndGuestbook(t *testing.T) {
	s, admin := newInstalledServer(t)
	anon := &testClient{t: t, router: admin.router}
	mustJSON(t, admin, "POST", "/api/v1/admin/misub/nodes", map[string]any{"url": "trojan://p@198.51.100.1:443?sni=example.com#HK"}, 200)
	p := mustJSON(t, admin, "POST", "/api/v1/admin/misub/profiles", map[string]any{"name": "公开组", "all_sources": true}, 200)
	pid := int(p["id"].(float64))

	mustJSON(t, admin, "POST", "/api/v1/admin/public-pages", map[string]any{"slug": "Bad Slug", "data": map[string]any{"title": "x"}}, 400)
	page := mustJSON(t, admin, "POST", "/api/v1/admin/public-pages", map[string]any{"slug": "home", "data": map[string]any{
		"enabled": true, "title": "欢迎", "description": "介绍", "cards": []any{map[string]any{"profile_id": pid, "target": "clash"}}}}, 200)
	id := int(page["id"].(float64))

	// 订阅组未分享时卡片不出现；分享后出现且可下载，投影里没有令牌。
	view := mustJSON(t, anon, "GET", "/api/v1/pages/home", nil, 200)
	if len(view["subscriptions"].([]any)) != 0 || view["guestbook"] != nil {
		t.Fatalf("unshared page = %v", view)
	}
	mustJSON(t, admin, "POST", fmt.Sprintf("/api/v1/admin/misub/profiles/%d/share", pid), map[string]any{"days": 3, "password": "Passw0rd"}, 200)
	w := anon.do("GET", "/api/v1/pages/home", nil, nil)
	stored, _ := s.Store.Ent.SubProfile.Get(t.Context(), pid)
	if !strings.Contains(w.Body.String(), "/cards/0") || strings.Contains(w.Body.String(), stored.ShareToken) {
		t.Fatalf("shared page = %s", w.Body.String())
	}
	if w = anon.do("GET", "/api/v1/pages/home/cards/0", nil, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "proxies:") {
		t.Fatalf("card download = %d %.120s", w.Code, w.Body.String())
	}
	mustJSON(t, admin, "DELETE", fmt.Sprintf("/api/v1/admin/misub/profiles/%d/share", pid), nil, 204)
	if w = anon.do("GET", "/api/v1/pages/home/cards/0", nil, nil); w.Code != 404 {
		t.Fatalf("unshared card = %d", w.Code)
	}
	mustJSON(t, anon, "GET", "/api/v1/pages/nope", nil, 404)

	// 留言板：默认关闭；开启（需审核、允许匿名）需要重新验证与版本号。
	mustJSON(t, anon, "POST", "/api/v1/pages/home/guestbook", map[string]any{"nickname": "a", "content": "hi", "type": "general"}, 404)
	cfg := mustJSON(t, admin, "GET", fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook/config", id), nil, 200)
	conf := map[string]any{"enabled": true, "allow_anonymous": true, "require_audit": true}
	mustJSON(t, admin, "PUT", fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook/config", id), map[string]any{"config": conf, "revision": cfg["revision"], "password": "wrong"}, 401)
	mustJSON(t, admin, "PUT", fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook/config", id), map[string]any{"config": conf, "revision": "stale", "password": "Passw0rd"}, 409)
	mustJSON(t, admin, "PUT", fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook/config", id), map[string]any{"config": conf, "revision": cfg["revision"], "password": "Passw0rd"}, 200)

	mustJSON(t, anon, "POST", "/api/v1/pages/home/guestbook", map[string]any{"nickname": "访客", "content": "", "type": "general"}, 400)
	mustJSON(t, anon, "POST", "/api/v1/pages/home/guestbook", map[string]any{"nickname": "访客", "content": "x", "type": "exec"}, 400)
	if r := mustJSON(t, anon, "POST", "/api/v1/pages/home/guestbook", map[string]any{"nickname": "访客", "content": "<b>你好</b>", "type": "general"}, 201); r["pending"] != true {
		t.Fatalf("submit = %v", r)
	}
	if list := mustJSON(t, anon, "GET", "/api/v1/pages/home/guestbook", nil, 200)["items"].([]any); len(list) != 0 {
		t.Fatalf("pending message must be hidden: %v", list)
	}
	msgs := mustJSON(t, admin, "GET", fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook/messages?status=pending", id), nil, 200)["items"].([]any)
	m := msgs[0].(map[string]any)
	base := fmt.Sprintf("/api/v1/admin/public-pages/%d/guestbook/messages", id)
	mustJSON(t, admin, "POST", base, map[string]any{"id": m["id"], "revision": "stale", "action": "status", "status": "approved"}, 409)
	mustJSON(t, admin, "POST", base, map[string]any{"id": m["id"], "revision": m["revision"], "action": "reply", "reply": "收到"}, 200)
	list := mustJSON(t, anon, "GET", "/api/v1/pages/home/guestbook", nil, 200)["items"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["reply"] != "收到" || list[0].(map[string]any)["content"] != "<b>你好</b>" {
		t.Fatalf("visible = %v", list)
	}
	// 投稿限速先于其他校验：同一 IP 10 分钟内最多 5 次，上面被拒绝的 3 次也计入（共已用 4 次）。
	mustJSON(t, anon, "POST", "/api/v1/pages/home/guestbook", map[string]any{"nickname": "访客", "content": "again", "type": "bug"}, 201)
	mustJSON(t, anon, "POST", "/api/v1/pages/home/guestbook", map[string]any{"nickname": "访客", "content": "again", "type": "bug"}, 429)

	// 删除页面同时删除它的留言。
	mustJSON(t, admin, "DELETE", fmt.Sprintf("/api/v1/admin/public-pages/%d", id), nil, 204)
	if n, _ := s.Store.Ent.Resource.Query().Where(resource.KindEQ(kindGuestbook)).Count(t.Context()); n != 0 {
		t.Fatalf("guestbook messages left: %d", n)
	}
	mustJSON(t, anon, "GET", "/api/v1/pages/home", nil, 404)
}
