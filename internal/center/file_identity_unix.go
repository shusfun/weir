//go:build unix

package center

import (
	"os"
	"syscall"
)

func init() {
	ownerOf = unixOwner
	chownFile = func(path string, uid, gid int) error {
		return os.Lchown(path, uid, gid)
	}
}

func defaultIsRoot() bool { return os.Geteuid() == 0 }

func unixOwner(info os.FileInfo) (int, int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return 0, 0, false
	}
	return int(stat.Uid), int(stat.Gid), true
}
