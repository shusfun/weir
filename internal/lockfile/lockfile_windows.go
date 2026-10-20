package lockfile

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

func TryAcquire(path string) (func(), error) {
	if path == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	ol := &windows.Overlapped{}
	if err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol); err != nil {
		f.Close()
		return nil, err
	}
	return func() { windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol); f.Close() }, nil
}
