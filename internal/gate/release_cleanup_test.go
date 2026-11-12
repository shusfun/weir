//go:build !windows

package gate

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/lockfile"
	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/packages/contracts"
)

func TestSuccessfulCenterFallbackRemovesPartialDownload(t *testing.T) {
	data := []byte("new binary")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/agent/release/1.2.0" {
			w.Header().Set("X-Release-SHA256", sumOf(data))
			_, _ = w.Write(data)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "update"), 0700); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(dir, "update", "weir-1.2.0.tar.gz.part")
	if err := os.WriteFile(partial, []byte("interrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &Agent{P: Paths{Data: dir}, center: &centerClient{center: srv.URL, http: srv.Client(), long: srv.Client(), token: "test", until: time.Now().Add(time.Hour)}}
	a.replaceFn = func(ctx context.Context, version, sum string, got []byte) error {
		if version != "1.2.0" || sum != sumOf(data) || !bytes.Equal(got, data) || releasefiles.Trace(ctx) == "" {
			t.Fatal("replacement lost payload or operation ID")
		}
		return nil
	}
	if err := a.selfUpdate(t.Context(), contracts.ReleaseTarget{Version: "1.2.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatal("successful fallback left a partial download", err)
	}
}

func TestStartupRetriesUnfinishedReleaseCleanup(t *testing.T) {
	h := NewHelper(Paths{Root: t.TempDir()})
	h.ProcRoot = t.TempDir()
	for _, v := range []string{"1.0.0", "1.1.0", "1.2.0"} {
		if err := h.storeRelease(v, []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	bad := filepath.Join(h.P.Root, "releases", "0.9.0")
	if err := os.Symlink(t.TempDir(), bad); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	h.maintainReleases(ctx)
	if _, err := os.Lstat(bad); err != nil {
		t.Fatal("unsafe release removed", err)
	}
	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(bad, 0755); err != nil {
		t.Fatal(err)
	}
	h.maintainReleases(t.Context())
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("startup did not retry unfinished cleanup", err)
	}
	for _, v := range []string{"1.1.0", "1.2.0"} {
		if _, err := os.Stat(filepath.Join(h.P.Root, "releases", v, "weir")); err != nil {
			t.Fatal("startup lost rollback version", err)
		}
	}
}

func TestReleaseUpdatesPruneAndKeepRollback(t *testing.T) {
	h := NewHelper(Paths{Root: t.TempDir()})
	h.ProcRoot = t.TempDir()
	h.releaseRestart = func() {}
	parent := "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	apply := func(v string) {
		t.Helper()
		data := []byte("binary " + v)
		r := httptest.NewRequest(http.MethodPost, "/release", bytes.NewReader(data))
		r.Header.Set("X-Version", v)
		r.Header.Set("X-SHA256", sumOf(data))
		r.Header.Set("traceparent", parent)
		w := httptest.NewRecorder()
		h.Handler().ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("X-Trace-Id") != "0123456789abcdef0123456789abcdef" {
			t.Fatalf("replace %s: %d %s", v, w.Code, w.Body.String())
		}
	}
	for _, v := range []string{"1.0.0", "1.1.0", "1.2.0", "1.2.0"} {
		apply(v)
	}
	keep, err := releasefiles.Pointers(h.P.Root)
	if err != nil || !keep["1.1.0"] || !keep["1.2.0"] || len(keep) != 2 {
		t.Fatalf("pointers: %v %v", keep, err)
	}
	if _, err := os.Stat(filepath.Join(h.P.Root, "releases", "1.0.0")); !os.IsNotExist(err) {
		t.Fatalf("old version not removed: %v", err)
	}
	apply("1.1.0")
	current, _ := os.Readlink(filepath.Join(h.P.Root, "current"))
	previous, _ := os.Readlink(filepath.Join(h.P.Root, "previous"))
	if current != filepath.Join("releases", "1.1.0") || previous != filepath.Join("releases", "1.2.0") {
		t.Fatalf("rollback pointers: %s %s", current, previous)
	}
	data := []byte("bad")
	r := httptest.NewRequest(http.MethodPost, "/release", bytes.NewReader(data))
	r.Header.Set("X-Version", "1.3.0")
	r.Header.Set("X-SHA256", sumOf([]byte("different")))
	r.Header.Set("traceparent", parent)
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	if w.Code != 422 || w.Header().Get("X-Trace-Id") != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("rejected update: %d %v", w.Code, w.Header())
	}
	if after, _ := os.Readlink(filepath.Join(h.P.Root, "current")); after != current {
		t.Fatal("rejected update changed current")
	}
}

func TestReleaseHonorsInstallerLockAndRefusesSymlink(t *testing.T) {
	h := NewHelper(Paths{Root: t.TempDir()})
	h.ProcRoot = t.TempDir()
	h.releaseRestart = func() {}
	unlock, err := lockfile.TryAcquire(h.releaseLock())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("binary")
	r := httptest.NewRequest(http.MethodPost, "/release", bytes.NewReader(data))
	r.Header.Set("X-Version", "1.0.0")
	r.Header.Set("X-SHA256", sumOf(data))
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	unlock()
	if w.Code != 409 {
		t.Fatalf("concurrent installation accepted: %d %s", w.Code, w.Body.String())
	}
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(h.P.Root, "releases"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(h.P.Root, "releases", "1.0.0")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := h.storeRelease("1.0.0", data); err == nil {
		t.Fatal("symlink destination accepted")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("wrote outside release root")
	}
}
