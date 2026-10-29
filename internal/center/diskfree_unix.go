//go:build !windows

package center

import "syscall"

// diskFree 返回目录所在文件系统的可用字节数。
func diskFree(dir string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}
