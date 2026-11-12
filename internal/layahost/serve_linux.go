//go:build linux

package layahost

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/shusfun/weir/internal/lockfile"
)

// Serve owns only a dedicated local socket. It exposes no Gate/core endpoints.
func Serve(ctx context.Context, directory, socket, group string) error {
	if os.Geteuid() != 0 || !filepath.IsAbs(directory) || !filepath.IsAbs(socket) || group == "" {
		return errors.New("Laya helper requires root, absolute paths and a client group")
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return err
	}
	parent := filepath.Dir(socket)
	if filepath.Clean(socket) != socket {
		return errors.New("helper socket path must be clean")
	}
	if err = trustedDirectoryPath(filepath.Dir(parent)); err != nil {
		return err
	}
	if err = os.Mkdir(parent, 0750); err == nil {
		if err = os.Chown(parent, 0, gid); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	if err = trustedDirectoryPath(parent); err != nil {
		return err
	}
	info, err := os.Stat(parent)
	if err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if info.Mode().Perm()&0001 == 0 && (int(stat.Gid) != gid || info.Mode().Perm()&0010 == 0) {
		return errors.New("helper directory is not traversable by the client group")
	}
	release, err := lockfile.TryAcquire(socket + ".lock")
	if err != nil {
		return err
	}
	defer release()
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("helper socket path is occupied")
		}
		if err = os.Remove(socket); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chown(socket, 0, gid); err != nil {
		return err
	}
	if err = os.Chmod(socket, 0660); err != nil {
		return err
	}
	controller := Controller{Load: func() (Deployment, error) { return ReadDeployment(directory) }}
	server := &http.Server{Handler: controller.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-done:
		}
	}()
	if err = server.Serve(listener); errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
