//go:build linux

package gate

import "syscall"

func diskUsage(path string) (*uint64, *uint64) {
	var fs syscall.Statfs_t
	if syscall.Statfs(path, &fs) != nil || fs.Blocks == 0 {
		return nil, nil
	}
	total := fs.Blocks * uint64(fs.Bsize)
	used := (fs.Blocks - fs.Bfree) * uint64(fs.Bsize)
	return &total, &used
}
