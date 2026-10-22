//go:build linux

package gate

import (
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// dropPrivileges 让内核以 migate-gate 身份运行（辅助程序本身是 root）；账号不存在时保持当前身份。
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}, Setpgid: true}
}
