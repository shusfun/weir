package center

import (
	"strings"
	"testing"
)

func TestDisguiseHidesBrowserPageButNotClients(t *testing.T) {
	_, admin, g := setupGate(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "d@example.test"}, 200)
	detail := mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(int(u["id"].(float64))), nil, 200)
	token := detail["link_items"].([]any)[0].(map[string]any)["token"].(string)
	browser := map[string]string{"User-Agent": "Mozilla/5.0 Chrome/130", "Accept": "text/html"}

	// 默认关闭：浏览器看到一键导入页，订阅信息接口可用。
	if w := admin.do("GET", "/sub/"+token, nil, browser); !strings.Contains(w.Body.String(), "console") {
		t.Fatalf("default browser page = %s", w.Body.String())
	}
	mustJSON(t, admin, "GET", "/api/v1/sub-info/"+token, nil, 200)

	set := mustJSON(t, admin, "GET", "/api/v1/admin/misub/settings", nil, 200)
	set["disguise"] = true
	mustJSON(t, admin, "PUT", "/api/v1/admin/misub/settings", set, 200)

	// 打开后：浏览器只看到伪装页，不出现 Weir 与用量；信息接口与不存在的订阅一样 404。
	w := admin.do("GET", "/sub/"+token, nil, browser)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "It works.") || strings.Contains(w.Body.String(), "Weir") || strings.Contains(w.Body.String(), "console") {
		t.Fatalf("disguised page = %d %s", w.Code, w.Body.String())
	}
	mustJSON(t, admin, "GET", "/api/v1/sub-info/"+token, nil, 404)
	// 客户端拉取不受影响。
	if w := admin.do("GET", "/sub/"+token, nil, map[string]string{"User-Agent": "clash.meta/1.19"}); w.Code != 200 || !strings.Contains(w.Body.String(), "proxies:") {
		t.Fatalf("client pull = %d %s", w.Code, w.Body.String())
	}
	// 登录后的「我的订阅」仍然可用。
	mustJSON(t, admin, "GET", "/api/v1/me/subscription", nil, 200)
}
