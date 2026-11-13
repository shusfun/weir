//go:build linux

package layahost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

func ReadDeployment(dir string) (Deployment, error) {
	var doc Deployment
	if err := trustedDirectoryPath(dir); err != nil {
		return doc, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return doc, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return doc, errors.New("untrusted deployment directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return doc, err
	}
	defer root.Close()
	info, err = root.Lstat("runtime.json")
	if err != nil {
		return doc, err
	}
	stat, ok = info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() > 64<<10 {
		return doc, errors.New("untrusted deployment file")
	}
	f, err := root.Open("runtime.json")
	if err != nil {
		return doc, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return doc, errors.New("deployment file changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err == nil {
		err = contracts.DecodeStrict(b, &doc, 64<<10)
	}
	return doc, err
}

// Every ancestor is part of the approval boundary: an unprivileged owner of
// a parent could replace an otherwise root-owned runtime directory.
func trustedDirectoryPath(dir string) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return errors.New("deployment path must be absolute and clean")
	}
	leaf := true
	for {
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		// A root-owned sticky ancestor (such as /tmp) protects root-owned
		// descendants against rename; the approved leaf itself stays immutable.
		writable := info.Mode().Perm()&0022 != 0
		if !ok || stat.Uid != 0 || !info.IsDir() || writable && (leaf || info.Mode()&os.ModeSticky == 0) {
			return errors.New("untrusted deployment path")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
		leaf = false
	}
}

func TrustedDirectory(dir string) error { return trustedDirectoryPath(dir) }

func TrustedFile(path string) error {
	if err := trustedDirectoryPath(filepath.Dir(path)); err != nil {
		return err
	}
	i, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := i.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || !i.Mode().IsRegular() || i.Mode().Perm()&0022 != 0 {
		return errors.New("untrusted file")
	}
	return nil
}

type boundedDockerOutput struct {
	bytes.Buffer
	exceeded bool
}

func (b *boundedDockerOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		b.exceeded = true
		return 0, errors.New("Docker output limit")
	}
	return b.Buffer.Write(p)
}

func ExecuteDocker(ctx context.Context, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	path := "/usr/bin/docker"
	if _, err := os.Stat(path); err != nil {
		path = "/usr/local/bin/docker"
	}
	// Pin the local socket and discard inherited Docker context/config variables.
	cmd := exec.CommandContext(ctx, path, append([]string{"--host", "unix:///var/run/docker.sock"}, args...)...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/root", "DOCKER_CONFIG=/etc/weir/docker"}
	var stdout, stderr boundedDockerOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, failure("laya_docker_output_limit", "layahost.ExecuteDocker")
	}
	if err != nil {
		if len(args) > 0 && args[0] == "inspect" && (strings.Contains(stderr.String(), "No such object") || strings.Contains(stderr.String(), "No such container")) {
			return nil, laya.ErrContainerMissing
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, failure("laya_docker_unavailable", "layahost.ExecuteDocker")
		}
		if len(args) > 0 && args[0] == "pull" {
			return nil, failure("laya_image_pull_failed", "layahost.ExecuteDocker")
		}
		return nil, failure("laya_container_operation_failed", "layahost.ExecuteDocker")
	}
	if len(args) > 0 && args[0] != "inspect" && !(len(args) > 1 && (args[0] == "image" || args[0] == "volume") && args[1] == "inspect") {
		return json.RawMessage(`{}`), nil
	}
	return stdout.Bytes(), nil
}
