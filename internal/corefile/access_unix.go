//go:build !windows

package corefile

import "golang.org/x/sys/unix"

func access(path string, mode uint32) error { return unix.Access(path, mode) }
