//go:build windows

package control

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockInstall(path string) (func(), error) {
	if err := os.MkdirAll(dirOf(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped)
	if err != nil {
		file.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
		file.Close()
	}, nil
}
