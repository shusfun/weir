package releasefiles

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestPruneKeepsPointersAndProcessReferences(t *testing.T) {
	base, proc := t.TempDir(), t.TempDir()
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0"} {
		put(t, filepath.Join(base, "releases", version, "weir"), version)
	}
	put(t, filepath.Join(base, "releases", "notes", "keep"), "unmanaged")
	link(t, filepath.Join("releases", "1.4.0"), filepath.Join(base, "current"))
	link(t, filepath.Join("releases", "1.3.0"), filepath.Join(base, "previous"))
	put(t, filepath.Join(proc, "42", "maps"), "0000-1000 r-xp 0 00:00 1 "+filepath.ToSlash(filepath.Join(base, "releases", "1.1.0", "weir"))+"\n")
	if err := os.MkdirAll(filepath.Join(proc, "42", "fd"), 0755); err != nil {
		t.Fatal(err)
	}
	link(t, filepath.Join(base, "releases", "1.2.0"), filepath.Join(proc, "42", "cwd"))
	stats, err := PruneReleases(base, proc)
	if err != nil || stats.Removed != 1 || stats.Kept != 2 || stats.Bytes != 5 {
		t.Fatalf("prune: %+v %v", stats, err)
	}
	for _, name := range []string{"1.1.0", "1.2.0", "1.3.0", "1.4.0", "notes"} {
		if _, err := os.Stat(filepath.Join(base, "releases", name)); err != nil {
			t.Fatalf("lost protected %s: %v", name, err)
		}
	}
	if err := os.RemoveAll(filepath.Join(proc, "42")); err != nil {
		t.Fatal(err)
	}
	stats, err = PruneReleases(base, proc)
	if err != nil || stats.Removed != 2 {
		t.Fatalf("retry after processes exit: %+v %v", stats, err)
	}
}

func TestPruneRefusesEscapingLinksAndRetries(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	put(t, filepath.Join(outside, "keep"), "outside")
	link(t, outside, filepath.Join(base, "1.0.0"))
	stats, err := Prune(base, nil, Version, nil)
	if err == nil || stats.Removed != 0 {
		t.Fatalf("symlink deletion must be rejected: %+v %v", stats, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(base, "1.0.0")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(base, "1.0.0", "weir"), "old")
	link(t, outside, filepath.Join(base, "1.0.0", "nested"))
	stats, err = Prune(base, nil, Version, nil)
	if err != nil || stats.Removed != 1 || stats.Bytes != 3 {
		t.Fatalf("retry: %+v %v", stats, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	link(t, outside, alias)
	if _, err := Prune(alias, nil, func(string) bool { return true }, nil); err == nil {
		t.Fatal("symlink root must be rejected")
	}
}

func TestInvalidPointerCannotDeleteReleases(t *testing.T) {
	base := t.TempDir()
	put(t, filepath.Join(base, "releases", "1.0.0", "weir"), "keep")
	link(t, "../elsewhere", filepath.Join(base, "current"))
	if _, err := PruneReleases(base, t.TempDir()); err == nil {
		t.Fatal("unsafe pointer accepted")
	}
	if _, err := os.Stat(filepath.Join(base, "releases", "1.0.0", "weir")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"..", ".", "../1.0", "notes", "1.0/child", ""} {
		if Version(name) {
			t.Fatalf("unsafe release name accepted: %q", name)
		}
	}
}

func TestUpdateTracePropagation(t *testing.T) {
	parent := "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	ctx := WithTrace(context.Background(), parent)
	if Trace(ctx) != "0123456789abcdef0123456789abcdef" || !strings.Contains(Traceparent(ctx), Trace(ctx)) {
		t.Fatal("trace was not propagated")
	}
	bad := WithTrace(context.Background(), "00-0123456789abcdef0123456789abcdef-0000000000000000-01")
	if Trace(bad) == Trace(ctx) || len(Trace(bad)) != 32 {
		t.Fatal("invalid parent must generate a new trace")
	}
}

func TestDownloadAndReleaseDirectoryGuards(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	put(t, filepath.Join(outside, "keep"), "outside")
	link(t, outside, filepath.Join(base, "alias"))
	if err := DownloadDir(filepath.Join(base, "alias", "download"), "weir-1.0.tar.gz"); err == nil {
		t.Fatal("download followed a parent symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "download")); !os.IsNotExist(err) {
		t.Fatal("download created an external directory")
	}
	link(t, filepath.Join(outside, "keep"), filepath.Join(base, "weir-1.0.tar.gz.part"))
	if err := DownloadDir(base, "weir-1.0.tar.gz"); err == nil {
		t.Fatal("download accepted a symlink partial")
	}
	put(t, filepath.Join(base, "1.0.0"), "not a directory")
	if stats, err := PruneVersionDirs(base, nil, nil); err == nil || stats.Removed != 0 {
		t.Fatalf("invalid release was removed: %+v %v", stats, err)
	}
}
