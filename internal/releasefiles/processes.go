package releasefiles

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Processes includes executables, mapped libraries, working directories and
// open files. Failure to inspect a live process prevents pruning.
func Processes(proc string) ([]string, error) {
	if proc == "" {
		proc = "/proc"
	}
	entries, err := os.ReadDir(proc)
	if err != nil {
		if runtime.GOOS != "linux" && errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil || !e.IsDir() {
			continue
		}
		pid := filepath.Join(proc, e.Name())
		err := func() error {
			links := []string{filepath.Join(pid, "exe"), filepath.Join(pid, "cwd")}
			fds, err := os.ReadDir(filepath.Join(pid, "fd"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			for _, fd := range fds {
				links = append(links, filepath.Join(pid, "fd", fd.Name()))
			}
			for _, link := range links {
				target, err := os.Readlink(link)
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				if filepath.IsAbs(target) {
					paths = append(paths, strings.TrimSuffix(target, " (deleted)"))
				}
			}
			maps, err := os.Open(filepath.Join(pid, "maps"))
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			defer maps.Close()
			scan := bufio.NewScanner(maps)
			scan.Buffer(make([]byte, 4096), 1<<20)
			for scan.Scan() {
				line := scan.Text()
				fields := strings.Fields(line)
				if len(fields) >= 6 && filepath.IsAbs(filepath.FromSlash(fields[5])) {
					target := strings.TrimSuffix(line[strings.Index(line, fields[5]):], " (deleted)")
					paths = append(paths, filepath.FromSlash(target))
				}
			}
			return scan.Err()
		}()
		if err != nil {
			if _, statErr := os.Stat(pid); errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("inspect process %s: %w", e.Name(), err)
		}
	}
	return paths, nil
}
