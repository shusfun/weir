package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/lockfile"
)

func TestUpdateBoundsWorkAndReportsCleanupFailure(t *testing.T) {
	setup(t, "center", false)
	if err := os.MkdirAll(WorkDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"weir-0.1.12-linux-amd64.tar.gz.part", "weir-0.1.13-linux-arm64.tar.gz", "notes"} {
		if err := os.WriteFile(filepath.Join(WorkDir, name), []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "keep"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(WorkDir, "v0.1.11")
	if err := os.Symlink(out, unsafe); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	request(t, Request{ID: "cleanup-00000001", Action: "update", Version: "0.1.14"})
	if err := Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	st, _ := ReadStatus()
	if st.State != "done" || !strings.Contains(strings.Join(st.Logs, "\n"), "WARN release cleanup update=cleanup-00000001") {
		t.Fatalf("cleanup warning must not turn installed update into failure: %+v", st)
	}
	for _, name := range []string{"weir-0.1.12-linux-amd64.tar.gz.part", "weir-0.1.13-linux-arm64.tar.gz", "v0.1.14"} {
		if _, err := os.Lstat(filepath.Join(WorkDir, name)); !os.IsNotExist(err) {
			t.Fatalf("obsolete work remains %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(WorkDir, "notes")); err != nil {
		t.Fatal("unmanaged work file removed")
	}
	if _, err := os.Stat(filepath.Join(out, "keep")); err != nil {
		t.Fatal("symlink cleanup touched external files")
	}
	if err := os.Remove(unsafe); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(unsafe, 0700); err != nil {
		t.Fatal(err)
	}
	request(t, Request{ID: "cleanup-00000002", Action: "update", Version: "0.1.14"})
	if err := Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unsafe); !os.IsNotExist(err) {
		t.Fatalf("next update did not retry cleanup: %v", err)
	}
}

func TestFailedExtractionAndInstallerCleanStaging(t *testing.T) {
	for _, brokenArchive := range []bool{true, false} {
		t.Run(map[bool]string{true: "extract", false: "installer"}[brokenArchive], func(t *testing.T) {
			e := setup(t, "center", false)
			if err := os.MkdirAll(WorkDir, 0700); err != nil {
				t.Fatal(err)
			}
			body := e.body
			if brokenArchive {
				body = pkg(t, map[string]string{"weir": "partial"})
			} else {
				runInstaller = func(context.Context, string, []string, func(string)) error { return errors.New("install failed") }
			}
			archive := filepath.Join(WorkDir, "weir-0.1.14-linux-amd64.tar.gz")
			if err := os.WriteFile(archive, body, 0600); err != nil {
				t.Fatal(err)
			}
			w := &writer{st: Status{ID: "cleanup-failure", Version: "0.1.14"}}
			if err := w.install(t.Context(), archive, "0.1.14", nil); err == nil {
				t.Fatal("failure was hidden")
			}
			if _, err := os.Stat(filepath.Join(WorkDir, "v0.1.14")); !os.IsNotExist(err) {
				t.Fatalf("partial staging remains: %v", err)
			}
			if _, err := os.Stat(archive); err != nil {
				t.Fatal("retryable package was removed")
			}
		})
	}
}

func TestConcurrentUpdaterDoesNotConsumeRequest(t *testing.T) {
	setup(t, "center", false)
	request(t, Request{ID: "cleanup-lock", Action: "update", Version: "0.1.14"})
	unlock, err := lockfile.TryAcquire(WorkDir + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := Apply(t.Context()); err == nil {
		t.Fatal("concurrent updater accepted")
	}
	if _, err := os.Stat(filepath.Join(Dir, "request.json")); err != nil {
		t.Fatal("busy updater consumed pending request")
	}
}

func TestTargetPartialSurvivesCleanupAndResumes(t *testing.T) {
	e := setup(t, "center", false)
	if err := os.MkdirAll(WorkDir, 0700); err != nil {
		t.Fatal(err)
	}
	name := "weir-0.1.14-linux-" + runtime.GOARCH + ".tar.gz"
	if err := os.WriteFile(filepath.Join(WorkDir, name+".part"), e.body[:40], 0600); err != nil {
		t.Fatal(err)
	}
	request(t, Request{ID: "cleanup-resume", Action: "update", Version: "0.1.14"})
	if err := Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	st, _ := ReadStatus()
	if !strings.Contains(strings.Join(st.Logs, "\n"), "处续传") {
		t.Fatalf("target partial was lost: %v", st.Logs)
	}
	if entries, _ := os.ReadDir(WorkDir); len(entries) != 0 {
		t.Fatalf("successful update left work files: %v", entries)
	}
}
