package center

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveConfigKeepsExistingMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 只保留只读位，不能断言完整 Unix 权限")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(path, Config{Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %#o, want 0640", info.Mode().Perm())
	}
	text, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(text), "127.0.0.1:19810") {
		t.Fatalf("rewritten config = %q err=%v", text, err)
	}
}

func TestWriteReplacingUsesPreviousOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotUID, gotGID int
	var called bool
	restoreIdentityHooks(t)
	ownerOf = func(os.FileInfo) (int, int, bool) { return 4242, 4343, true }
	chownFile = func(target string, uid, gid int) error {
		called = true
		gotUID, gotGID = uid, gid
		if target != path+".tmp" {
			t.Fatalf("chown target = %s", target)
		}
		return nil
	}
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeReplacing(path, []byte("updated")); err != nil {
		t.Fatal(err)
	}
	if !called || gotUID != 4242 || gotGID != 4343 {
		t.Fatalf("chown called=%v uid=%d gid=%d", called, gotUID, gotGID)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "updated" {
		t.Fatalf("body = %q err=%v", body, err)
	}
}

func TestWriteReplacingKeepsOriginalWhenChownFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	restoreIdentityHooks(t)
	ownerOf = func(os.FileInfo) (int, int, bool) { return 7, 8, true }
	chownFile = func(string, int, int) error { return errors.New("chown denied") }
	if err := writeReplacing(path, []byte("updated")); err == nil {
		t.Fatal("chown failure was ignored")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "original" {
		t.Fatalf("body = %q err=%v", body, err)
	}
	if _, err = os.Lstat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temporary file remains")
	}
}

func TestNewCredentialFollowsDirectoryOwnerAndMode(t *testing.T) {
	dir := t.TempDir()
	var gotUID, gotGID int
	restoreIdentityHooks(t)
	ownerOf = func(os.FileInfo) (int, int, bool) { return 5150, 5250, true }
	chownFile = func(target string, uid, gid int) error {
		gotUID, gotGID = uid, gid
		return nil
	}
	token, err := Initialize(filepath.Join(dir, "config.json"), Config{Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "setup-token"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("setup-token mode = %#o", info.Mode().Perm())
	}
	if gotUID != 5150 || gotGID != 5250 {
		t.Fatalf("owner = %d:%d", gotUID, gotGID)
	}
	stored, err := os.ReadFile(filepath.Join(dir, "setup-token"))
	if err != nil || string(stored) != token {
		t.Fatalf("token file = %q err=%v", stored, err)
	}
}

func TestReplaceFileRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "config.json")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink unavailable")
	}
	src := filepath.Join(dir, "next.json")
	if err := os.WriteFile(src, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(src, link); err == nil {
		t.Fatal("symlink replacement accepted")
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "keep" {
		t.Fatalf("target = %q err=%v", body, err)
	}
}

func restoreIdentityHooks(t *testing.T) {
	t.Helper()
	prevOwner, prevChown := ownerOf, chownFile
	t.Cleanup(func() {
		ownerOf = prevOwner
		chownFile = prevChown
	})
}
