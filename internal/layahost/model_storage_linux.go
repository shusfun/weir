//go:build linux

package layahost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"

	"github.com/shusfun/weir/packages/laya"
)

func openModelBlob(root *os.Root, name string) (*os.File, error) {
	// A container can modify its model volume. Do not let a concurrent FIFO
	// replacement block the privileged helper before file type validation.
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

func inspectModelStorage(ctx context.Context, execute laya.DockerExecute, volume string) (*ModelStorage, error) {
	fail := func() (*ModelStorage, error) {
		return nil, failure("laya_resources_unknown", "layahost.modelStorage")
	}
	raw, err := execute(ctx, []string{"volume", "inspect", volume})
	if err != nil {
		return fail()
	}
	var rows []struct {
		Name, Driver, Mountpoint string
		Options                  map[string]string
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &rows) != nil || len(rows) != 1 || rows[0].Name != volume || rows[0].Driver != "local" || len(rows[0].Options) != 0 || !filepath.IsAbs(rows[0].Mountpoint) {
		return fail()
	}
	required, err := missingModelBytes(ctx, rows[0].Mountpoint, laya.FixedManifest())
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return fail()
	}
	var stat syscall.Statfs_t
	if syscall.Statfs(rows[0].Mountpoint, &stat) != nil || stat.Bsize <= 0 {
		return fail()
	}
	return &ModelStorage{RequiredBytes: required, AvailableBytes: stat.Bavail * uint64(stat.Bsize)}, nil
}
