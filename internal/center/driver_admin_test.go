package center

import (
	"runtime"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/kernels"
)

func TestDriverAdminSaveHistoryRollbackRestore(t *testing.T) {
	s, admin, _ := setupGate(t)
	// Linux 上检查会真的运行缓存里的内核：换成一个总是通过的假程序（其他平台只做生成检查）。
	if _, err := s.kernelCache().Put("xray", "26.3.27", runtime.GOARCH, []byte("#!/bin/sh\nexit 0\n")); err != nil {
		t.Fatal(err)
	}
	list := mustJSON(t, admin, "GET", "/api/v1/admin/drivers", nil, 200)["drivers"].([]any)
	if len(list) < 4 {
		t.Fatalf("drivers = %v", list)
	}
	base := kernels.Builtin()["xray-26.3"]
	// 解析不了的驱动不能保存，错误原样交回。
	w := admin.do("PUT", "/api/v1/admin/drivers/xray-26.3", map[string]any{"text": base + "unknown_field: 1\n"}, nil)
	if body := decode(t, w); w.Code != 422 || !strings.Contains(body["check"].(map[string]any)["error"].(string), "unknown_field") {
		t.Fatalf("invalid driver = %d %v", w.Code, body)
	}
	// 名称不一致也拒绝。
	if w = admin.do("PUT", "/api/v1/admin/drivers/xray-26.3", map[string]any{"text": strings.Replace(base, "name: xray-26.3", "name: other", 1)}, nil); w.Code != 400 {
		t.Fatalf("mismatched name = %d", w.Code)
	}
	// 合法改动：生成检查通过（中心不是 Linux 或没下载内核时为 unknown），保存为 #1，样例覆盖每个方案。
	edited := strings.Replace(base, "notes: 26.3.27 起移除 allowInsecure。", "notes: 改过。", 1)
	saved := mustJSON(t, admin, "PUT", "/api/v1/admin/drivers/xray-26.3", map[string]any{"text": edited, "note": "test"}, 200)
	check := saved["check"].(map[string]any)
	if saved["revision"].(float64) != 1 || check["state"] == "failed" || len(check["samples"].([]any)) != 6 {
		t.Fatalf("save = %v", saved)
	}
	d, _, err := s.drivers(t.Context()).Resolve("xray-26.3")
	if err != nil || d.Notes != "改过。" {
		t.Fatalf("saved driver not active: %v %q", err, d.Notes)
	}
	detail := mustJSON(t, admin, "GET", "/api/v1/admin/drivers/xray-26.3", nil, 200)
	if detail["driver"].(map[string]any)["source"] != "modified" || len(detail["history"].([]any)) != 1 {
		t.Fatalf("detail = %v", detail)
	}
	// 停用 → 启用 → 退回到 #1 → 恢复内置：每次都是新版本。
	mustJSON(t, admin, "POST", "/api/v1/admin/drivers/xray-26.3/disable", nil, 200)
	if _, _, err = s.drivers(t.Context()).Select("xray", "26.3.27"); err == nil {
		t.Fatal("disabled driver must not be selected")
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/drivers/xray-26.3/enable", nil, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/drivers/xray-26.3/rollback", map[string]any{"revision": 1}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/drivers/xray-26.3/restore", nil, 200)
	d, _, _ = s.drivers(t.Context()).Resolve("xray-26.3")
	if d.Notes != "26.3.27 起移除 allowInsecure。" {
		t.Fatalf("restore: %q", d.Notes)
	}
	if hist := mustJSON(t, admin, "GET", "/api/v1/admin/drivers/xray-26.3", nil, 200)["history"].([]any); len(hist) != 5 {
		t.Fatalf("history = %d", len(hist))
	}
	// 新增自定义驱动：继承内置的，换一个名称与版本区间。
	custom := "kernel: xray\nname: xray-27\nextends: xray-26.10\nmin_version: 27.0.0\nnotes: 自定义\n"
	mustJSON(t, admin, "PUT", "/api/v1/admin/drivers/xray-27", map[string]any{"text": custom}, 200)
	if drv, _, err := s.drivers(t.Context()).Select("xray", "27.1.0"); err != nil || drv.Name != "xray-27" {
		t.Fatalf("custom driver: %v %v", drv.Name, err)
	}
	if w = admin.do("POST", "/api/v1/admin/drivers/xray-27/restore", nil, nil); w.Code != 404 {
		t.Fatalf("restore custom = %d", w.Code)
	}
	// 内核检查不通过（Linux 上才会运行内核）：拒绝保存，日志交回。
	if runtime.GOOS == "linux" {
		if _, err := s.kernelCache().Put("xray", "26.3.27", runtime.GOARCH, []byte("#!/bin/sh\necho bad field >&2\nexit 1\n")); err != nil {
			t.Fatal(err)
		}
		w := admin.do("PUT", "/api/v1/admin/drivers/xray-26.3", map[string]any{"text": edited + "# again\n"}, nil)
		body := decode(t, w)
		samples, _ := body["check"].(map[string]any)["samples"].([]any)
		if w.Code != 422 || len(samples) == 0 || !strings.Contains(samples[0].(map[string]any)["log"].(string), "bad field") {
			t.Fatalf("failing kernel check = %d %v", w.Code, body)
		}
	}
	// 方案来自驱动：节点向导看到的就是驱动里的方案。
	items := mustJSON(t, admin, "GET", "/api/v1/admin/presets", nil, 200)["items"].([]any)
	if len(items) != 11 || items[0].(map[string]any)["id"] != "vless-reality" || items[0].(map[string]any)["kernel"] != "xray" {
		t.Fatalf("presets = %v", items)
	}
}
