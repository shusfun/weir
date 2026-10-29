//go:build windows

package center

// diskFree 在 Windows 上不检查（中心只在 Linux 上部署）。
func diskFree(string) (uint64, bool) { return 0, false }
