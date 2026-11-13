//go:build linux

package gate

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

// Opt-in: run this binary in a real systemd service with Delegate=cpu.
func TestCPUDelegatedHierarchyAndQuota(t *testing.T) {
	if os.Getenv("WEIR_CGROUP_TEST") != "1" {
		t.Skip("requires a delegated Linux cgroup v2 service")
	}
	var groups []string
	t.Cleanup(func() {
		for _, group := range groups {
			_ = os.Remove(group)
		}
	})
	for i := 0; i < 2; i++ {
		group, err := isolateCPU(t.Context(), contracts.WorkerBudget{CPUMaxPercent: 20})
		if err != nil {
			t.Fatal(err)
		}
		groups = append(groups, group)
	}
	if groups[0] == groups[1] {
		t.Fatal("concurrent tasks share a cgroup")
	}
	parent, err := ownCgroup()
	if err != nil || filepath.Base(parent) != "weir-manager" {
		t.Fatalf("manager not moved to leaf: %s %v", parent, err)
	}
	for _, group := range groups {
		quota, err := os.ReadFile(filepath.Join(group, "cpu.max"))
		if err != nil || strings.TrimSpace(string(quota)) != "20000 100000" {
			t.Fatalf("quota=%q err=%v", quota, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var commands []*exec.Cmd
	defer func() {
		cancel()
		for _, cmd := range commands {
			_ = cmd.Wait()
		}
	}()
	for _, group := range groups {
		fd, err := os.Open(group)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, "sha256sum", "/dev/zero")
		cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(fd.Fd())}
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		err = cmd.Start()
		fd.Close()
		if err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	<-ctx.Done()
	for _, cmd := range commands {
		_ = cmd.Wait()
	}
	for _, group := range groups {
		raw, err := os.ReadFile(filepath.Join(group, "cpu.stat"))
		if err != nil {
			t.Fatal(err)
		}
		usage := int64(0)
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "usage_usec" {
				usage, err = strconv.ParseInt(fields[1], 10, 64)
			}
		}
		if err != nil || usage < 50_000 || usage > 1_400_000 {
			t.Fatalf("CPU quota not enforced: usage_us=%d err=%v", usage, err)
		}
		t.Logf("%s quota_percent=20 elapsed_s=4 usage_us=%d", filepath.Base(group), usage)
		if err = os.Remove(group); err != nil {
			t.Fatal(fmt.Errorf("reclaim task cgroup: %w", err))
		}
	}
}
