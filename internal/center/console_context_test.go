package center

import (
	"strings"
	"testing"
)

func TestConsoleContextPerDrawerRedactedAndNavigation(t *testing.T) {
	s, admin := newInstalledServer(t)
	m, _ := s.ensureMCP()
	h := s.mcpHandler(m)
	read := func() string {
		text, _ := toolText(t, h, m.webToken, "get_console_context", map[string]any{})
		return text
	}
	// 还没有打开过抽屉：未知并说明原因。
	if text := read(); !strings.Contains(text, "未知") || !strings.Contains(text, "reason") {
		t.Fatalf("no context = %s", text)
	}
	tabA, tabB := "drawer-aaaaaaaaaaaaaaaa", "drawer-bbbbbbbbbbbbbbbb"
	report := func(drawer string, body map[string]any) {
		body["drawer"] = drawer
		mustJSON(t, admin, "POST", "/api/v1/admin/dsh/context", body, 204)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/dsh/context", map[string]any{"drawer": "x"}, 400)
	report(tabA, map[string]any{"page": "VPS 与节点", "declared": true, "visible": true, "drawer_open": true,
		"object": map[string]any{"type": "gate", "name": "HK-1", "id": 1},
		"dialog": map[string]any{"title": "新建节点", "step": "第 2 步 VPS"},
		"form": []any{
			map[string]any{"key": "name", "label": "节点名称", "value": "香港-01"},
			map[string]any{"key": "password", "label": "SOCKS5 密码", "value": "hunter2"},
			map[string]any{"key": "api_key", "label": "1Panel 密钥", "value": "SECRET-KEY"},
		}})
	text := read()
	if !strings.Contains(text, "HK-1") || !strings.Contains(text, "第 2 步") || !strings.Contains(text, "香港-01") || !strings.Contains(text, `"status": "当前"`) {
		t.Fatalf("context = %s", text)
	}
	// 密码、密钥类字段不传，只标已填。
	if strings.Contains(text, "hunter2") || strings.Contains(text, "SECRET-KEY") || !strings.Contains(text, `"filled": true`) {
		t.Fatalf("form must be redacted: %s", text)
	}
	// 另一个标签：各自独立。B 在后台时不覆盖 A；B 到前台打开抽屉后成为当前会话。
	report(tabB, map[string]any{"page": "用户", "visible": false, "drawer_open": true})
	if text := read(); !strings.Contains(text, "HK-1") {
		t.Fatalf("background tab overwrote the active one: %s", text)
	}
	report(tabB, map[string]any{"page": "用户", "visible": true, "drawer_open": true})
	if text := read(); !strings.Contains(text, "用户") || !strings.Contains(text, "此页未提供详细信息") {
		t.Fatalf("tab B context = %s", text)
	}
	// 切到后台：标明不在前台，不拿旧内容冒充当前。
	report(tabB, map[string]any{"page": "用户", "visible": false, "drawer_open": true})
	if text := read(); !strings.Contains(text, "不在前台") {
		t.Fatalf("hidden tab = %s", text)
	}
	report(tabB, map[string]any{"page": "用户", "visible": true, "drawer_open": true})

	// 导航：只作用于当前抽屉会话（B），A 收不到；只能去白名单里的页面；取一次即删。
	if text, isErr := toolText(t, h, m.webToken, "navigate_console", map[string]any{"page": "secret-admin"}); !isErr {
		t.Fatalf("unknown page = %s", text)
	}
	if text, isErr := toolText(t, h, m.webToken, "navigate_console", map[string]any{"page": "gates", "object_id": 3}); isErr {
		t.Fatal(text)
	}
	if w := admin.do("GET", "/api/v1/admin/dsh/navigation?drawer="+tabA, nil, nil); w.Code != 204 {
		t.Fatalf("other tab got navigation: %d", w.Code)
	}
	nav := mustJSON(t, admin, "GET", "/api/v1/admin/dsh/navigation?drawer="+tabB, nil, 200)
	if nav["path"] != "/gates" || nav["object"] != "gate" || nav["id"].(float64) != 3 {
		t.Fatalf("navigation = %v", nav)
	}
	if w := admin.do("GET", "/api/v1/admin/dsh/navigation?drawer="+tabB, nil, nil); w.Code != 204 {
		t.Fatal("navigation must be taken once")
	}

	// 后台分析看不到控制台上下文。
	if _, isErr := toolText(t, h, m.analystToken, "get_console_context", map[string]any{}); !isErr {
		t.Fatal("analysis must not read the console context")
	}
	// 退出登录：上下文立即失效。
	mustJSON(t, admin, "POST", "/api/v1/auth/logout", nil, 200)
	if text := read(); !strings.Contains(text, "未知") || !strings.Contains(text, "退出登录") {
		t.Fatalf("after logout = %s", text)
	}
}
