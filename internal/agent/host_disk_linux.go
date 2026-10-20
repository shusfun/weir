//go:build linux

package agent

import (
	"math"
	"syscall"
)

func hostDisk(path string) (*uint64, *uint64) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil || st.Bsize <= 0 {
		return nil, nil
	}
	size := uint64(st.Bsize)
	if st.Blocks == 0 || st.Blocks > math.MaxUint64/size || st.Bavail > st.Blocks {
		return nil, nil
	}
	total, available := st.Blocks*size, st.Bavail*size
	return &total, &available
}
