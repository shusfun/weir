package center

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/kernels"
)

func TestMihomoDriverWithoutServerPresets(t *testing.T) {
	s, admin, _ := setupGate(t)
	if _, err := s.kernelCache().Put("mihomo", "1.19.33", runtime.GOARCH, []byte("#!/bin/sh\nexit 0\n")); err != nil {
		t.Fatal(err)
	}
	body := mustJSON(t, admin, "PUT", "/api/v1/admin/drivers/mihomo-1", map[string]any{"text": kernels.Builtin()["mihomo-1"]}, 200)
	check := body["check"].(map[string]any)
	samples := check["samples"].([]any)
	if len(samples) != 1 || samples[0].(map[string]any)["preset"] != "client" || check["state"] == "failed" {
		t.Fatalf("mihomo 客户端驱动未检查：%v", check)
	}
}

func TestCachedMihomoMustPassDriverBeforeDefault(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("需要运行 Linux 内核检查命令")
	}
	s, _, _ := setupGate(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cache := s.kernelCache()
	for _, arch := range []string{"amd64", "arm64"} {
		if _, err := cache.Put("mihomo", "1.19.33", arch, []byte("#!/bin/sh\necho invalid-client-config >&2\nexit 1\n")); err != nil {
			t.Fatal(err)
		}
	}
	j := s.startKernelFetch(ctx, "mihomo", "1.19.33", 1, "")
	j, ok := s.jobs.Wait(ctx, j.ID)
	if !ok || j.Status != jobFailed || !strings.Contains(j.Error, "invalid-client-config") {
		t.Fatalf("无效的缓存内核被接受：%+v", j)
	}
	if _, _, err := cache.Get("mihomo", "1.19.33", runtime.GOARCH); err == nil {
		t.Fatal("未移除检查失败的本机程序")
	}
	other := "arm64"
	if runtime.GOARCH == other {
		other = "amd64"
	}
	if _, _, err := cache.Get("mihomo", "1.19.33", other); err != nil {
		t.Fatalf("误删其他架构缓存：%v", err)
	}
	if v := s.explicitDefault(ctx, "mihomo"); v != "" {
		t.Fatalf("检查失败却设为默认：%s", v)
	}
}
