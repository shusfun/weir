package selfupdate

import "os"

// copyNoFollow 在 Windows 上只用于开发与测试（自更新只在 Linux 上运行）。
func copyNoFollow(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return copyTo(in, dst)
}
