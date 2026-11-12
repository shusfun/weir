package center

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/releasefiles"
)

const kernelArchiveCacheBytes int64 = 512 << 20
const kernelArchiveCacheTTL = 24 * time.Hour

// The caller holds center-file-distribution through download and serving, so
// pruning cannot remove a partial or archive being used by another center.
func (s *Server) pruneKernelArchives(ctx context.Context, reserve int64, digest string, now time.Time) error {
	return pruneKernelArchiveDir(ctx, filepath.Join(s.dataDir(), "kernel-archives"), reserve, digest, now)
}

func pruneKernelArchiveDir(ctx context.Context, dir string, reserve int64, digest string, now time.Time) error {
	if reserve < 0 || reserve > kernelArchiveCacheBytes || digest != "" && !validTraceHex(digest, 64) {
		return errors.New("kernel_archive_cache_reservation_invalid")
	}
	root, err := releasefiles.Ensure(dir, 0700)
	if err != nil {
		return err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	type cached struct {
		name string
		size int64
		at   time.Time
	}
	var files []cached
	var used int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		sha := strings.TrimSuffix(name, ".part")
		if !validTraceHex(sha, 64) {
			continue
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("kernel_archive_cache_file_unsafe")
		}
		if sha == digest {
			if info.Size() > reserve {
				return errors.New("kernel_archive_cache_partial_oversize")
			}
			continue
		}
		if now.Sub(info.ModTime()) >= kernelArchiveCacheTTL {
			if err := root.Remove(name); err != nil {
				return err
			}
			continue
		}
		used += info.Size()
		files = append(files, cached{name, info.Size(), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].at.Equal(files[j].at) {
			return files[i].name < files[j].name
		}
		return files[i].at.Before(files[j].at)
	})
	// Reserve both a complete target and its resumable partial during replacement.
	for _, file := range files {
		if used+2*reserve <= kernelArchiveCacheBytes {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := root.Remove(file.name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		used -= file.size
	}
	if used+2*reserve > kernelArchiveCacheBytes {
		return errors.New("kernel_archive_cache_full")
	}
	return nil
}
