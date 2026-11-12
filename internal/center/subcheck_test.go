package center

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeChecker 是代替 mihomo / sing-box 的脚本：配置里含 BAD 时失败并输出原因。
func fakeChecker(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("checker fixture is a POSIX executable script; exercised on Linux")
	}
	bin := filepath.Join(t.TempDir(), "checker")
	script := "#!/bin/sh\nfor a; do f=\"$a\"; done\nif grep -q BAD \"$f\"; then echo \"parse error: proxy BAD\" >&2; exit 1; fi\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestSubscriptionCheckDiagnosticsAndRetry(t *testing.T) {
	s, _ := newInstalledServer(t)
	t.Cleanup(func() { subCheckBinary, subCheckRun = defaultSubCheckBinary, runSubCheck })
	subCheckBinary = func(*Server, context.Context, string) (string, string, error) { return "checker", "diagnostics", nil }
	calls := 0
	errRun := context.DeadlineExceeded
	subCheckRun = func(context.Context, string, string, string, []byte) ([]byte, error) {
		calls++
		return []byte(`download https://example.test/private-subscription?token=secret failed; password="hidden" uuid=12345678-1234-1234-1234-123456789abc`), errRun
	}
	ctx := t.Context()
	r := s.checkSubscription(ctx, "clash", []byte("diagnostics"))
	if r.Status != "unknown" || !strings.Contains(r.Error, "超时") || !strings.Contains(r.Error, "download") || strings.Contains(r.Error, "secret") || strings.Contains(r.Error, "hidden") || strings.Contains(r.Error, "12345678") {
		t.Fatalf("timeout diagnostics: %+v", r)
	}
	errRun = context.Canceled
	if r = s.checkSubscription(ctx, "clash", []byte("diagnostics")); r.Status != "unknown" || r.Error != "检查被取消" || calls != 2 {
		t.Fatalf("cancellation should not cache: %+v, %d", r, calls)
	}
	errRun = errors.New("exit 1")
	r = s.checkSubscription(ctx, "clash", []byte("diagnostics"))
	if r.Status != "failed" || strings.Contains(r.Error, "hidden") {
		t.Fatalf("failed diagnostics: %+v", r)
	}
	errRun = nil
	if r = s.checkSubscription(ctx, "clash", []byte("diagnostics")); r.Status != "failed" || calls != 3 {
		t.Fatalf("failed cache: %+v, %d", r, calls)
	}
	if r = s.checkSubscription(context.WithValue(ctx, freshSubCheckKey{}, true), "clash", []byte("diagnostics")); r.Status != "passed" || calls != 4 {
		t.Fatalf("explicit retry: %+v, %d", r, calls)
	}
	parent, cancel := context.WithCancel(withTrace(context.WithValue(ctx, httpTraceKey{}, httpTrace{ID: strings.Repeat("1", 32)}), "check-request"))
	cancel()
	s.recordSubCheck(parent, "trace-test", "fixture", "clash", []byte("diagnostics"))
	deadline := time.After(3 * time.Second)
	for {
		raw, ok, err := s.Store.KV.Get(ctx, "subcheck-last:trace-test:mihomo")
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			var recorded SubCheck
			if json.Unmarshal([]byte(raw), &recorded) != nil || recorded.TraceID != strings.Repeat("1", 32) || recorded.RequestID != "check-request" || recorded.Status != "passed" {
				t.Fatalf("background trace: %s", raw)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("background check not recorded")
		case <-time.After(10 * time.Millisecond):
		}
	}
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
