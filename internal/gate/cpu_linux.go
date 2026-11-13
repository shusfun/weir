//go:build linux

package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/shusfun/weir/packages/contracts"
)

var cpuHierarchyMu sync.Mutex

// isolateCPU 在本进程已被委托的 cgroup 子树里准备限额。
// 正式安装给 weir-gate / weir-center 设置 Delegate=cpu，进程不能、也不去创建根上的 /sys/fs/cgroup/weir-tasks。
func isolateCPU(_ context.Context, budget contracts.WorkerBudget) (string, error) {
	cpuHierarchyMu.Lock()
	defer cpuHierarchyMu.Unlock()
	if budget.CPUMaxPercent <= 0 {
		budget.CPUMaxPercent = 20
	}
	parent, err := ownCgroup()
	if err != nil {
		return "", err
	}
	if filepath.Base(parent) == "weir-manager" {
		parent = filepath.Dir(parent)
	}
	if _, err = os.Stat(filepath.Join(parent, "cgroup.controllers")); err != nil {
		return "", &contracts.TaskError{Code: "cpu_cgroup_unavailable", Origin: "gate.isolateCPU"}
	}
	controllers, err := os.ReadFile(filepath.Join(parent, "cgroup.controllers"))
	if err != nil {
		return "", cpuIOError("read_controllers", err)
	}
	if !strings.Contains(string(controllers), "cpu") {
		return "", &contracts.TaskError{Code: "cpu_controller_missing", Origin: "gate.isolateCPU"}
	}
	// cgroup v2 forbids enabling a domain controller while the parent contains
	// processes. Move the service into its own leaf before enabling CPU below it.
	manager := filepath.Join(parent, "weir-manager")
	if err = os.MkdirAll(manager, 0755); err != nil {
		return "", cpuIOError("manager", err)
	}
	if err = writeCgroup(filepath.Join(manager, "cgroup.procs"), strconv.Itoa(os.Getpid())); err != nil {
		return "", cpuIOError("delegate_process", err)
	}
	if err = writeCgroup(filepath.Join(parent, "cgroup.subtree_control"), "+cpu"); err != nil {
		return "", cpuIOError("enable_controller", err)
	}
	dir, err := os.MkdirTemp(parent, "weir-task-")
	if err != nil {
		return "", cpuIOError("mkdir", err)
	}
	quota := budget.CPUMaxPercent * 1000
	if err = writeCgroup(filepath.Join(dir, "cpu.max"), strconv.Itoa(quota)+" 100000\n"); err != nil {
		_ = os.Remove(dir)
		return "", cpuIOError("cpu.max", err)
	}
	return dir, nil
}

func writeCgroup(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = f.WriteString(value)
	return errors.Join(err, f.Close())
}

func ownCgroup() (string, error) {
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", cpuIOError("self_cgroup", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && parts[0] == "0" && parts[1] == "" {
			path := parts[2]
			if path == "" || path == "/" {
				return "", &contracts.TaskError{Code: "cpu_cgroup_not_delegated", Origin: "gate.ownCgroup"}
			}
			return filepath.Join("/sys/fs/cgroup", path), nil
		}
	}
	return "", &contracts.TaskError{Code: "cpu_cgroup_unavailable", Origin: "gate.ownCgroup"}
}

func cpuIOError(op string, err error) error {
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		return &contracts.TaskError{Code: "cpu_cgroup_permission", Origin: "gate." + op}
	}
	return fmt.Errorf("cpu_%s: %w", op, err)
}

func runCPUChild(ctx context.Context, paths Paths, budget contracts.WorkerBudget, task contracts.Task) (json.RawMessage, error) {
	dir, err := isolateCPU(ctx, budget)
	if err != nil {
		return nil, err
	}
	defer os.Remove(dir)
	bin, err := os.Executable()
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "weir-cpu-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	in := filepath.Join(tmp, "task.json")
	out := filepath.Join(tmp, "result.json")
	raw, err := json.Marshal(task)
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(in, raw, 0600); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, "gate", "cpu-exec", "--config", paths.Config, "--data", paths.Data, "--socket", paths.Socket, "--task", in, "--result", out)
	cmd.Env = append(os.Environ(), "WEIR_CPU_CGROUP="+dir)
	if err = cmd.Run(); err != nil {
		var detail *contracts.TaskError
		if errors.As(err, &detail) {
			return nil, detail
		}
		return nil, fmt.Errorf("cpu_isolated_process_failed: %w", err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// JoinCPUGroup 让独立执行进程把自己放进受限组。读不到组时失败，不继续在无限制进程里算。
func JoinCPUGroup() error {
	dir := os.Getenv("WEIR_CPU_CGROUP")
	if dir == "" {
		return &contracts.TaskError{Code: "cpu_cgroup_unavailable", Origin: "gate.JoinCPUGroup"}
	}
	if err := writeCgroup(filepath.Join(dir, "cgroup.procs"), strconv.Itoa(os.Getpid())+"\n"); err != nil {
		return cpuIOError("join", err)
	}
	return nil
}
