//go:build !linux

package dsh

import (
	"os/exec"
	"syscall"
)

// 非 Linux 上不托管 dsh（Supported 会拒绝）；这些实现只为编译与单元测试。
func AgentUser(name string) (int, int, bool)                  { return 0, 0, false }
func AgentProcAttr(name string) (*syscall.SysProcAttr, error) { return &syscall.SysProcAttr{}, nil }
func setProcAttr(*exec.Cmd)                                   {}
func Terminate(cmd *exec.Cmd)                                 { _ = cmd.Process.Kill() }
func Kill(cmd *exec.Cmd)                                      { _ = cmd.Process.Kill() }
func terminate(cmd *exec.Cmd)                                 { Terminate(cmd) }
func kill(cmd *exec.Cmd)                                      { Kill(cmd) }
