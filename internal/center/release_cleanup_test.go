package center

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/selfupdate"
)

func TestReleaseCacheRetainsCurrentPreviousAndResumableDownloads(t *testing.T) {
	oldVersion, oldBase := buildinfo.Version, selfupdate.Base
	buildinfo.Version, selfupdate.Base = "1.2.0", t.TempDir()
	t.Cleanup(func() { buildinfo.Version, selfupdate.Base = oldVersion, oldBase })
	put := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("cached"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	put(filepath.Join(selfupdate.Base, "releases", "1.1.0", "weir"))
	if err := os.Symlink(filepath.Join("releases", "1.1.0"), filepath.Join(selfupdate.Base, "previous")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s := &Server{Config: Config{DataDir: t.TempDir()}}
	dir := filepath.Join(s.dataDir(), "releases")
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0", "notes"} {
		put(filepath.Join(dir, version, "arm64", "weir"))
	}
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0"} {
		put(filepath.Join(dir, ".downloads", releaseAssetName(version, "arm64")+".part"))
	}
	s.pruneReleaseCache(context.Background(), nil)
	for _, version := range []string{"1.1.0", "1.2.0", "notes"} {
		if _, err := os.Stat(filepath.Join(dir, version, "arm64", "weir")); err != nil {
			t.Fatalf("protected cache lost: %s %v", version, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "1.0.0")); !os.IsNotExist(err) {
		t.Fatalf("obsolete cache remains: %v", err)
	}
	for _, version := range []string{"1.1.0", "1.2.0"} {
		if _, err := os.Stat(filepath.Join(dir, ".downloads", releaseAssetName(version, "arm64")+".part")); err != nil {
			t.Fatal("resumable package removed", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".downloads", releaseAssetName("1.0.0", "arm64")+".part")); !os.IsNotExist(err) {
		t.Fatal("obsolete partial remains", err)
	}
	outside := t.TempDir()
	put(filepath.Join(outside, "keep"))
	bad := filepath.Join(dir, "0.9.0")
	if err := os.Symlink(outside, bad); err != nil {
		t.Fatal(err)
	}
	run := JobRun{m: &jobManager{}, e: &jobEntry{job: Job{Trace: "cleanup-test"}}}
	s.pruneReleaseCache(context.Background(), &run)
	if len(run.e.job.Logs) != 1 || !strings.Contains(run.e.job.Logs[0].Text, "WARN") || !strings.Contains(run.e.job.Logs[0].Text, "trace=cleanup-test") {
		t.Fatal("cleanup warning lost its operation ID", run.e.job.Logs)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal("cache cleanup escaped through symlink", err)
	}
	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	put(filepath.Join(bad, "arm64", "weir"))
	s.pruneReleaseCache(context.Background(), &run)
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("cache cleanup was not retried", err)
	}
}
