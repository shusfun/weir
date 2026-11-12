//go:build linux

package gate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLayaDeploymentRequiresRootOwnedUnwritableFiles(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned deployment checks require a root test process")
	}
	dir := filepath.Join(t.TempDir(), "approved")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "runtime.json")
	raw, _ := json.Marshal(layaDeployment{Schema: 1, Version: "v0.18.0", Model: "laya:multilingual"})
	write := func() {
		t.Helper()
		if err := os.WriteFile(file, raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if _, err := readLayaDeployment(dir); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name            string
		mutate, restore func() error
	}{
		{"writable_file", func() error { return os.Chmod(file, 0666) }, func() error { return os.Chmod(file, 0644) }},
		{"writable_directory", func() error { return os.Chmod(dir, 0777) }, func() error { return os.Chmod(dir, 0755) }},
		{"foreign_file", func() error { return os.Chown(file, 1000, 1000) }, func() error { return os.Chown(file, 0, 0) }},
		{"foreign_directory", func() error { return os.Chown(dir, 1000, 1000) }, func() error { return os.Chown(dir, 0, 0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mutate(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := tc.restore(); err != nil {
					t.Fatal(err)
				}
			}()
			if _, err := readLayaDeployment(dir); err == nil {
				t.Fatal("untrusted deployment accepted")
			}
		})
	}
	if err := os.Rename(file, file+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file+".backup", file); err != nil {
		t.Fatal(err)
	}
	if _, err := readLayaDeployment(dir); err == nil {
		t.Fatal("symlink file accepted")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"schema":1,"args":["--privileged"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readLayaDeployment(dir); err == nil {
		t.Fatal("unknown deployment fields accepted")
	}
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readLayaDeployment(link); err == nil {
		t.Fatal("symlink directory accepted")
	}
}
