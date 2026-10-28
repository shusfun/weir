package center

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeChecker 是代替 mihomo / sing-box 的脚本：配置里含 BAD 时失败并输出原因。
func fakeChecker(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "checker")
	script := "#!/bin/sh\nfor a; do f=\"$a\"; done\nif grep -q BAD \"$f\"; then echo \"parse error: proxy BAD\" >&2; exit 1; fi\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestSubscriptionCheckUsesKernelAndCaches(t *testing.T) {
	s, _ := newInstalledServer(t)
	bin := fakeChecker(t)
	calls := 0
	subCheckBinary = func(_ *Server, _ context.Context, kernel string) (string, string, error) {
		calls++
		return bin, "1.0.0", nil
	}
	t.Cleanup(func() { subCheckBinary = defaultSubCheckBinary })
	ctx := t.Context()

	if r := s.checkSubscription(ctx, "surge", []byte("x")); r.Status != "unknown" || r.Kernel != "" {
		t.Fatalf("surge = %+v", r)
	}
	if r := s.checkSubscription(ctx, "clash", []byte("proxies: []\n")); r.Status != "passed" || r.Kernel != "mihomo" || r.Version != "1.0.0" {
		t.Fatalf("clash ok = %+v", r)
	}
	r := s.checkSubscription(ctx, "singbox", []byte(`{"outbounds":[{"tag":"BAD"}]}`))
	if r.Status != "failed" || r.Kernel != "sing-box" || r.Error == "" {
		t.Fatalf("singbox bad = %+v", r)
	}
	// 同一内容再检查直接用缓存结果，不再运行内核。
	before := calls
	if again := s.checkSubscription(ctx, "singbox", []byte(`{"outbounds":[{"tag":"BAD"}]}`)); again.Status != "failed" || calls != before+1 {
		t.Fatalf("cached = %+v calls %d→%d", again, before, calls)
	}
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if again := s.checkSubscription(ctx, "singbox", []byte(`{"outbounds":[{"tag":"BAD"}]}`)); again.Status != "failed" {
		t.Fatalf("cache must not rerun the kernel: %+v", again)
	}
}

func TestSubscriptionCheckUnknownWithoutKernel(t *testing.T) {
	s, admin := newInstalledServer(t)
	// 默认实现：测试机上没有下载内核（或不是 Linux），结果是「未知」而不是「通过」。
	if r := s.checkSubscription(t.Context(), "clash", []byte("proxies: []\n")); r.Status != "unknown" || r.Error == "" {
		t.Fatalf("no kernel = %+v", r)
	}
	v := mustJSON(t, admin, "GET", "/api/v1/admin/sub-checks", nil, 200)
	if k := v["kernels"].(map[string]any)["mihomo"].(map[string]any); k["available"] != false {
		t.Fatalf("kernels = %v", v["kernels"])
	}
}
