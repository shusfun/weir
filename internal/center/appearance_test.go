package center

import "testing"

// 外观偏好跟着账号保存，只接受可选值，按账号隔离。
func TestAppearanceFollowsAccount(t *testing.T) {
	_, admin := newInstalledServer(t)
	if r := mustJSON(t, admin, "GET", "/api/v1/me/appearance", nil, 200); r["saved"] != false {
		t.Fatalf("fresh account = %v", r)
	}
	bad := map[string]any{"theme": "neon", "motion": "lite"}
	if w := admin.do("PUT", "/api/v1/me/appearance", bad, nil); w.Code != 400 {
		t.Fatal("invalid theme accepted", w.Code)
	}
	good := map[string]any{"theme": "dark", "motion": "lite", "follow_reduce": false, "stutter": true, "globe": false}
	mustJSON(t, admin, "PUT", "/api/v1/me/appearance", good, 200)
	r := mustJSON(t, admin, "GET", "/api/v1/me/appearance", nil, 200)
	a, _ := r["appearance"].(map[string]any)
	if r["saved"] != true || a["motion"] != "lite" || a["theme"] != "dark" || a["globe"] != false {
		t.Fatalf("saved appearance = %v", r)
	}
	admin.cookie = nil
	if w := admin.do("GET", "/api/v1/me/appearance", nil, nil); w.Code != 401 {
		t.Fatal("appearance readable without session", w.Code)
	}
}
