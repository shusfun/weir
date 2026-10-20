//go:build unix

package control

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockInstall(path string) (func(), error) {
	if err := os.MkdirAll(dirOf(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		file.Close()
	}, nil
}
