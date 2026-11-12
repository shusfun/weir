//go:build linux

package dsh

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// RunUser 是运行 dsh 管理进程的系统用户（安装脚本创建）。dsh 以它的身份运行，
// 只能写自己的家目录与状态目录，读不到中心的配置、数据库与密钥。

// AgentUser 返回中心直接启动管理进程时要切换到的用户：中心以 root 运行且该用户存在时为它，否则为空。
// 宿主应以受限用户启动管理进程，无需赋予业务进程 root 权限。
func AgentUser(name string) (uid, gid int, ok bool) {
	if os.Geteuid() != 0 {
		return 0, 0, false
	}
	u, err := user.Lookup(name)
	if err != nil {
		return 0, 0, false
	}
	uid, _ = strconv.Atoi(u.Uid)
	gid, _ = strconv.Atoi(u.Gid)
	return uid, gid, true
}

// AgentProcAttr 是中心启动管理进程时的进程属性：独立进程组，必要时切换用户。
func AgentProcAttr(name string) (*syscall.SysProcAttr, error) {
	attr := &syscall.SysProcAttr{Setpgid: true}
	if os.Geteuid() == 0 {
		uid, gid, ok := AgentUser(name)
		if !ok {
			return nil, errors.New("configured runtime identity is unavailable")
		}
		attr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	}
	return attr, nil
}

func setProcAttr(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// Terminate / Kill 结束整个进程组（连同 dsh 启动的子进程）。
func Terminate(cmd *exec.Cmd) { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
func Kill(cmd *exec.Cmd)      { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }

func terminate(cmd *exec.Cmd) { Terminate(cmd) }
func kill(cmd *exec.Cmd)      { Kill(cmd) }
