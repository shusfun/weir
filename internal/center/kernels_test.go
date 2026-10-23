package center

import (
	"testing"

	"github.com/imzyb/MiGate/packages/contracts"
)

// 下载新版本不能悄悄替换 Gate 正在运行的内核；只有管理员设置默认或锁定版本后才切换。
func TestNewKernelDownloadDoesNotReplaceRunningVersion(t *testing.T) {
	s, admin, g := setupGate(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "vless-reality", "skip_probe": true}, 200)
	if code, _ := g.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Version: "test", Hostname: "hk", Arch: "amd64", Kernels: map[string]string{"xray": "26.3.27"}}); code != 200 {
		t.Fatalf("heartbeat = %d", code)
	}
	if _, err := s.kernelCache().Put("xray", "26.10.21", "amd64", []byte("newer-xray")); err != nil {
		t.Fatal(err)
	}
	if v := g.config(0).Kernels["xray"].Version; v != "26.3.27" {
		t.Fatalf("running version replaced by a fresh download: %s", v)
	}
	mustJSON(t, admin, "PUT", "/api/v1/admin/kernels/defaults", map[string]string{"xray": "26.10.21"}, 200)
	if v := g.config(0).Kernels["xray"].Version; v != "26.10.21" {
		t.Fatalf("explicit default not applied: %s", v)
	}
	mustJSON(t, admin, "PATCH", "/api/v1/admin/gates/"+itoa(int(g.id)), map[string]any{"kernel_pins": map[string]string{"xray": "26.3.27"}}, 200)
	if v := g.config(0).Kernels["xray"].Version; v != "26.3.27" {
		t.Fatalf("pin not applied: %s", v)
	}
}
