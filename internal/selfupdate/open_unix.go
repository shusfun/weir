//go:build !windows

package selfupdate

import (
	"os"
	"syscall"
)

// copyNoFollow 复制中心目录里的文件：O_NOFOLLOW 防止中心在检查之后换成指向别处的符号链接。
func copyNoFollow(src, dst string) error {
	in, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	return copyTo(in, dst)
}
