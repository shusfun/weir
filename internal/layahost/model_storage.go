package layahost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/shusfun/weir/packages/laya"
)

// ModelStorage describes the fixed model on the approved Docker volume. Paths
// and file contents never cross the privileged boundary.
type ModelStorage struct {
	RequiredBytes  uint64 `json:"required_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

// Missing/corrupt blobs need their full replacement size. Partial download
// files are deliberately not credited: only verified immutable blobs count.
func missingModelBytes(ctx context.Context, dir string, model laya.Model) (uint64, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	var missing uint64
	for _, artifact := range model.Files {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		name := filepath.Join("models", artifact.Name)
		info, err := root.Lstat(name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		valid := false
		if err == nil && info.Mode().IsRegular() && info.Size() == artifact.Size {
			file, err := openModelBlob(root, name)
			if err != nil {
				return 0, err
			}
			opened, err := file.Stat()
			if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
				file.Close()
				return 0, errors.New("model blob changed while opening")
			}
			hash := sha256.New()
			n, copyErr := io.Copy(hash, io.LimitReader(contextReader{ctx, file}, artifact.Size+1))
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil {
				return 0, errors.Join(copyErr, closeErr)
			}
			valid = n == artifact.Size && hex.EncodeToString(hash.Sum(nil)) == artifact.SHA256
		}
		if !valid {
			missing += uint64(artifact.Size)
		}
	}
	// Keep a bounded allowance for manifest publication and directory metadata.
	return missing + 1<<20, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
