//go:build !linux

package gate

import "os/exec"

// 非 Linux（本机开发与测试）不降权。
func dropPrivileges(*exec.Cmd, string) {}
