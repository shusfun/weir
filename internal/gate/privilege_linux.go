//go:build linux

package gate

import (
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// capNetBindService 是 CAP_NET_BIND_SERVICE：降权后仍可监听 1024 以下端口（REALITY 节点优先用 443）。
const capNetBindService = 10

// dropPrivileges 让内核以 weir-gate 身份运行（辅助程序本身是 root），只保留绑定低端口这一项能力；
// 账号不存在时保持当前身份。
func dropPrivileges(cmd *exec.Cmd, account string) {
	if os.Geteuid() != 0 || account == "" {
		return
	}
	u, err := user.Lookup(account)
	if err != nil {
		return
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential:  &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
		AmbientCaps: []uintptr{capNetBindService},
		Setpgid:     true,
	}
}
