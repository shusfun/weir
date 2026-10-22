//go:build !linux

package gate

// 非 Linux 不读取数据盘用量（显示为未知）。
func diskUsage(string) (*uint64, *uint64) { return nil, nil }
