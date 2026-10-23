package center

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMisubSourcesProfilesGrantsAndShare(t *testing.T) {
	s, admin := newInstalledServer(t)
	s.AllowPrivateSources = true // 测试源在本机回环；生产默认拒绝
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var lines []string
		for i := 1; i <= 12; i++ {
			lines = append(lines, fmt.Sprintf("trojan://p%d@198.51.100.%d:443?sni=example.com#HK-%02d", i, i, i))
		}
		w.Header().Set("Subscription-Userinfo", "upload=100; download=200; total=10737418240; expire=1999999999")
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))))
	}))
	defer upstream.Close()

	src := mustJSON(t, admin, "POST", "/api/v1/admin/misub/sources", map[string]any{"name": "机场A", "url": upstream.URL, "filter_exclude": "HK-12"}, 200)
	sid := int(src["id"].(float64))
	ref := mustJSON(t, admin, "POST", fmt.Sprintf("/api/v1/admin/misub/sources/%d/refresh", sid), nil, 200)
	if n := len(ref["nodes"].([]any)); n != 11 {
		t.Fatalf("refresh nodes = %d %v", n, ref["result"])
	}
	list := mustJSON(t, admin, "GET", "/api/v1/admin/misub/sources", nil, 200)
	if tr := list["traffic"].(map[string]any); tr["total"].(float64) != 10737418240 {
		t.Fatalf("traffic = %v", tr)
	}
	// 生产行为（关闭测试开关）：私网、回环、云元数据、带账号、非 http 的地址都被拒绝。
	s.AllowPrivateSources = false
	for _, bad := range []string{"http://user:pw@example.com/s", "ftp://example.com/s", "http://169.254.169.254/", upstream.URL, "http://localhost/s"} {
		mustJSON(t, admin, "POST", "/api/v1/admin/misub/sources", map[string]any{"url": bad}, 400)
	}
	s.AllowPrivateSources = true
	mustJSON(t, admin, "POST", "/api/v1/admin/misub/sources", map[string]any{"url": "https://example.com/s", "user_agent": "bad\r\nX-Injected: 1"}, 400)

	ss := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pw")) + "@203.0.113.9:8388#home"
	imp := mustJSON(t, admin, "POST", "/api/v1/admin/misub/nodes/import", map[string]any{"content": ss + "\n" + strings.Replace(ss, "203.0.113.9", "203.0.113.10", 1), "group": "家宽"}, 200)
	if imp["imported"].(float64) != 2 {
		t.Fatalf("import = %v", imp)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/misub/nodes", map[string]any{"url": "https://example.com/not-a-node"}, 400)
	nodes := mustJSON(t, admin, "GET", "/api/v1/admin/misub/nodes", nil, 200)["items"].([]any)
	nid := int(nodes[0].(map[string]any)["id"].(float64))
	// 分组重命名：目标已存在时拒绝，不静默合并。
	mustJSON(t, admin, "POST", "/api/v1/admin/misub/nodes", map[string]any{"url": strings.Replace(ss, "203.0.113.9", "203.0.113.11", 1), "group": "其他"}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/misub/nodes/groups/rename", map[string]any{"from": "家宽", "to": "其他"}, 409)
	mustJSON(t, admin, "POST", "/api/v1/admin/misub/nodes/groups/rename", map[string]any{"from": "家宽", "to": "住宅"}, 204)

	mustJSON(t, admin, "POST", "/api/v1/admin/misub/profiles", map[string]any{"name": "坏组", "settings": map[string]any{"no_such": 1}}, 400)
	mustJSON(t, admin, "POST", "/api/v1/admin/misub/profiles", map[string]any{"name": "坏组", "operators": []any{map[string]any{"type": "exec"}}}, 400)
	p := mustJSON(t, admin, "POST", "/api/v1/admin/misub/profiles", map[string]any{"name": "日常", "source_ids": []int{sid}, "node_ids": []int{nid},
		"settings": map[string]any{"template": "builtin:clash_acl4ssr_lite"}}, 200)
	pid := int(p["id"].(float64))
	pre := mustJSON(t, admin, "POST", fmt.Sprintf("/api/v1/admin/misub/profiles/%d/preview", pid), map[string]any{"target": "clash"}, 200)
	if n := len(pre["nodes"].([]any)); n != 14 || !strings.Contains(pre["output"].(string), "proxy-groups:") { // 2 条提示 + 1 手动 + 11 机场
		t.Fatalf("preview %d nodes, output_error=%v", n, pre["output_error"])
	}

	// 分配给用户：用户在个人中心看到独立地址；撤销后立即失效。
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "carol@example.test", "password": "Passw0rd"}, 200)
	uid := int(u["id"].(float64))
	mustJSON(t, admin, "PUT", fmt.Sprintf("/api/v1/admin/misub/profiles/%d/grants", pid), map[string]any{"user_ids": []int{uid}}, 200)
	carol := login(t, s, "carol@example.test", "Passw0rd")
	groups := mustJSON(t, carol, "GET", "/api/v1/me/groups", nil, 200)["items"].([]any)
	if len(groups) != 1 {
		t.Fatalf("groups = %v", groups)
	}
	gurl := groups[0].(map[string]any)["url"].(string)
	path := gurl[strings.Index(gurl, "/sub/g/"):]
	for ua, want := range map[string]string{"clash.meta/1.19": "proxy-groups:", "sing-box/1.12": `"outbounds"`, "v2rayN/7": ""} {
		w := carol.do("GET", path, nil, map[string]string{"User-Agent": ua})
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("%s: %d %.200s", ua, w.Code, w.Body.String())
		}
	}
	mustJSON(t, carol, "GET", "/api/v1/admin/misub/profiles", nil, 403)
	mustJSON(t, admin, "PATCH", fmt.Sprintf("/api/v1/admin/users/%d", uid), map[string]any{"enabled": false}, 200)
	if w := admin.do("GET", path, nil, nil); w.Code != 403 {
		t.Fatalf("disabled user's group link = %d", w.Code)
	}
	mustJSON(t, admin, "PUT", fmt.Sprintf("/api/v1/admin/misub/profiles/%d/grants", pid), map[string]any{"user_ids": []int{}}, 200)
	if w := admin.do("GET", path, nil, nil); w.Code != 404 {
		t.Fatalf("revoked grant = %d", w.Code)
	}

	// 匿名分享：需重新验证；令牌只显示一次、库里存摘要；撤下后拒绝。
	mustJSON(t, admin, "POST", fmt.Sprintf("/api/v1/admin/misub/profiles/%d/share", pid), map[string]any{"days": 7, "password": "wrong"}, 401)
	sh := mustJSON(t, admin, "POST", fmt.Sprintf("/api/v1/admin/misub/profiles/%d/share", pid), map[string]any{"days": 7, "password": "Passw0rd"}, 200)
	spath := sh["url"].(string)[strings.Index(sh["url"].(string), "/sub/g/"):]
	if w := admin.do("GET", spath, nil, map[string]string{"User-Agent": "clash.meta"}); w.Code != 200 {
		t.Fatalf("share = %d", w.Code)
	}
	stored, _ := s.Store.Ent.SubProfile.Get(t.Context(), pid)
	if strings.HasSuffix(spath, stored.ShareToken) {
		t.Fatal("share token must be stored as a digest")
	}
	mustJSON(t, admin, "DELETE", fmt.Sprintf("/api/v1/admin/misub/profiles/%d/share", pid), nil, 204)
	if w := admin.do("GET", spath, nil, nil); w.Code != 404 {
		t.Fatalf("unshared = %d", w.Code)
	}

	// 删除源后订阅组的引用同步移除。
	mustJSON(t, admin, "DELETE", fmt.Sprintf("/api/v1/admin/misub/sources/%d", sid), nil, 204)
	after, _ := s.Store.Ent.SubProfile.Get(t.Context(), pid)
	if len(after.SourceIds) != 0 {
		t.Fatalf("source still referenced: %v", after.SourceIds)
	}
}
