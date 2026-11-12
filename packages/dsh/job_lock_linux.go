//go:build linux

package dsh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Locks remain as empty files: unlinking a locked inode would allow another
// process to create a separate lock for the same profile.
func lockJobProfile(ctx context.Context, dir, name string) (func(), error) {
	if !filepath.IsAbs(dir) || !ValidNamespace(name) {
		return nil, errors.New("invalid background profile lock")
	}
	locks := filepath.Join(dir, ".job-locks")
	if err := os.MkdirAll(locks, 0o700); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(locks)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.OpenFile(name+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("invalid profile lock file")
	}
	for {
		if err = ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			f.Close()
			return nil, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
