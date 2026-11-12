// Package releasefiles confines maintenance of installed programs and update files.
package releasefiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var versionName = regexp.MustCompile(`^[0-9][0-9A-Za-z._+-]{0,63}$`)

func Version(name string) bool { return versionName.MatchString(name) }

type Stats struct {
	Removed int
	Bytes   int64
	Kept    int
}

func (s Stats) String() string {
	return fmt.Sprintf("removed=%d bytes=%d protected=%d", s.Removed, s.Bytes, s.Kept)
}

// Open rejects symlinks in the directory itself and its ancestors. Root keeps
// subsequent operations confined even if a child is replaced during cleanup.
func Open(dir string) (*os.Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for p := abs; ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe maintenance directory: %s", p)
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return os.OpenRoot(abs)
}

// Ensure creates a managed directory without traversing symbolic links.
func Ensure(dir string, mode fs.FileMode) (*os.Root, error) {
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		return Open(dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	parent, err := Ensure(filepath.Dir(abs), mode)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err = parent.Mkdir(filepath.Base(abs), mode); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return Open(abs)
}

// DownloadDir also rejects existing non-regular download targets and partials.
func DownloadDir(dir, name string) error {
	r, err := Ensure(dir, 0700)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, n := range []string{name, name + ".part"} {
		st, err := r.Lstat(n)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("unsafe download file: %s", n)
		}
	}
	return nil
}

// Pointers accepts only direct release directories. An invalid pointer aborts
// pruning rather than turning a malformed current/previous into a deletion list.
func Pointers(base string) (map[string]bool, error) {
	r, err := Open(base)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	keep := map[string]bool{}
	for _, name := range []string{"current", "previous"} {
		link, err := r.Readlink(name)
		if errors.Is(err, os.ErrNotExist) && name == "previous" {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		v := filepath.Base(link)
		if !Version(v) || filepath.Clean(link) != filepath.Join("releases", v) {
			return nil, fmt.Errorf("invalid %s release pointer", name)
		}
		st, err := r.Lstat(filepath.Join("releases", v))
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("invalid %s release directory", name)
		}
		keep[v] = true
	}
	return keep, nil
}

// Prune only considers direct children explicitly owned by the caller. It does
// not follow symlinks, including during sizing or recursive deletion.
func Prune(dir string, keep map[string]bool, owned func(string) bool, protected []string) (Stats, error) {
	return prune(dir, keep, owned, protected, false)
}

func PruneVersionDirs(dir string, keep map[string]bool, protected []string) (Stats, error) {
	return prune(dir, keep, Version, protected, true)
}

func prune(dir string, keep map[string]bool, owned func(string) bool, protected []string, dirsOnly bool) (Stats, error) {
	var stats Stats
	r, err := Open(dir)
	if errors.Is(err, os.ErrNotExist) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	defer r.Close()
	entries, err := fs.ReadDir(r.FS(), ".")
	if err != nil {
		return stats, err
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if !owned(name) || keep[name] {
			continue
		}
		path, _ := filepath.Abs(filepath.Join(dir, name))
		busy := false
		for _, p := range protected {
			if p == path || strings.HasPrefix(p, path+string(filepath.Separator)) {
				busy = true
				break
			}
		}
		if busy {
			stats.Kept++
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			errs = append(errs, fmt.Errorf("refusing symlink %s", name))
			continue
		}
		if dirsOnly && !e.IsDir() {
			errs = append(errs, fmt.Errorf("refusing non-directory release %s", name))
			continue
		}
		var size int64
		err := fs.WalkDir(r.FS(), name, func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				info, err := d.Info()
				if err != nil {
					return err
				}
				size += info.Size()
			}
			return nil
		})
		if err == nil {
			err = r.RemoveAll(name)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", name, err))
			continue
		}
		stats.Removed++
		stats.Bytes += size
	}
	return stats, errors.Join(errs...)
}

func PruneReleases(base, proc string) (Stats, error) {
	if _, err := os.Lstat(filepath.Join(base, "releases")); errors.Is(err, os.ErrNotExist) {
		return Stats{}, nil
	}
	keep, err := Pointers(base)
	if err != nil {
		return Stats{}, err
	}
	protected, err := Processes(proc)
	if err != nil {
		return Stats{}, err
	}
	return PruneVersionDirs(filepath.Join(base, "releases"), keep, protected)
}
