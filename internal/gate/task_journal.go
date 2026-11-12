package gate

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func readJournal(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return err
	}
	return contracts.DecodeStrict(raw, out, 65536)
}

func prepareJournals(dir string, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		// No task payload is ever persisted. Abandoned atomic-write files only
		// contain metadata and may be removed after the lease window.
		if strings.HasPrefix(entry.Name(), ".task-") && now.Sub(info.ModTime()) > 2*contracts.TaskLease {
			if err = os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if now.Sub(info.ModTime()) > 14*24*time.Hour {
			var journal taskJournal
			if readJournal(filepath.Join(dir, entry.Name()), &journal) == nil && !journal.Deadline.IsZero() && !journal.Deadline.After(now.Add(-14*24*time.Hour)) && journal.Status != "running" {
				if err = os.Remove(filepath.Join(dir, entry.Name())); err != nil {
					return err
				}
				continue
			}
		}
		count++
	}
	if count >= 10000 {
		return errors.New("task journal capacity reached")
	}
	return nil
}
func writeJournal(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".task-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
