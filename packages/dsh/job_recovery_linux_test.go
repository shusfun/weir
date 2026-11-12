//go:build linux

package dsh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackgroundProfileLockCancellationAndRecovery(t *testing.T) {
	p := Paths{Dir: t.TempDir()}
	profile := filepath.Join(p.Home(), "profiles", "sample-analyst")
	web := filepath.Join(p.Home(), "profiles", "web")
	for _, dir := range []string{profile, web} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{"cordis.patch.yml", "session.json"} {
			if err := os.WriteFile(filepath.Join(dir, file), []byte("SENSITIVE-FIXTURE"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	unlock, err := lockJobProfile(t.Context(), p.Dir, "sample-analyst")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if second, err := lockJobProfile(ctx, p.Dir, "sample-analyst"); !errors.Is(err, context.DeadlineExceeded) {
		if second != nil {
			second()
		}
		unlock()
		t.Fatalf("parallel profile accepted: %v", err)
	}
	unlock()
	if err = RecoverBackgroundProfiles(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(profile, "cordis.patch.yml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credential patch retained: %v", err)
	}
	for _, file := range []string{filepath.Join(profile, "session.json"), filepath.Join(web, "session.json"), filepath.Join(web, "cordis.patch.yml")} {
		if _, err = os.Stat(file); err != nil {
			t.Fatalf("user state removed: %s %v", file, err)
		}
	}
	if err = RecoverBackgroundProfiles(t.Context(), p); err != nil {
		t.Fatal("recovery is not idempotent", err)
	}
}

func TestBackgroundProfileLockRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".job-locks"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, ".job-locks", "sample-analyst.lock")); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockJobProfile(t.Context(), dir, "sample-analyst")
	if err == nil {
		unlock()
		t.Fatal("symlink lock accepted")
	}
}
