package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type versionTestRunner struct {
	active          bool
	rejectConfig    bool
	restartFailures int
	calls           []string
}

func (r *versionTestRunner) Run(ctx context.Context, name string, args ...string) error {
	_, e := r.RunOutput(ctx, name, args...)
	return e
}
func (r *versionTestRunner) RunOutput(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if name == "systemctl" {
		switch args[0] {
		case "is-active":
			if !r.active {
				return nil, errors.New("inactive")
			}
		case "stop":
			r.active = false
		case "restart":
			if r.restartFailures > 0 {
				r.restartFailures--
				return nil, errors.New("restart failure")
			}
			r.active = true
		}
		return []byte("active"), nil
	}
	if args[0] != "version" && r.rejectConfig {
		return nil, errors.New("bad config")
	}
	return []byte("version"), nil
}
func versionFixture(t *testing.T) (coreVersionManager, *versionTestRunner, []byte, []byte) {
	t.Helper()
	base := t.TempDir()
	r := &versionTestRunner{active: true}
	m := coreVersionManager{root: filepath.Join(base, "cores"), packages: filepath.Join(base, "packages"), config: filepath.Join(base, "config"), runner: r, ready: func(context.Context, string) error { return nil }}
	for _, d := range []string{m.root, m.packages, m.config} {
		if e := os.Mkdir(d, 0750); e != nil {
			t.Fatal(e)
		}
	}
	old, newBinary := []byte("\x7fELFold-binary"), []byte("\x7fELFnew-binary")
	os.WriteFile(filepath.Join(m.root, "xray"), old, 0750)
	os.WriteFile(filepath.Join(m.config, "xray.json"), []byte(`{}`), 0640)
	os.WriteFile(filepath.Join(m.packages, "xray-2"), newBinary, 0750)
	catalog, _ := json.Marshal([]corePackage{{Core: "xray", Version: "2", SHA256: coreHash(newBinary)}})
	os.WriteFile(filepath.Join(m.packages, "catalog.json"), catalog, 0640)
	return m, r, old, newBinary
}
func TestCoreVersionInstallRollbackReplayAndOwnedClean(t *testing.T) {
	m, r, old, newBinary := versionFixture(t)
	ctx := context.Background()
	req := coreVersionRequest{TaskID: 1, Revision: 1, Core: "xray", Operation: "core.install", Version: "2"}
	result, e := m.execute(ctx, req)
	if e != nil || result["current"] != coreHash(newBinary) {
		t.Fatalf("install: %v %v", result, e)
	}
	raw, _ := os.ReadFile(filepath.Join(m.root, "xray"))
	if string(raw) != string(newBinary) || !r.active {
		t.Fatal("binary switch not applied")
	}
	if info, e := os.Stat(filepath.Join(m.root, "xray")); e != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0001 == 0) {
		t.Fatal("unprivileged core service cannot execute installed binary")
	}
	req.TaskID = 2
	req.Revision = 2
	req.Operation = "core.rollback"
	req.Version = ""
	result, e = m.execute(ctx, req)
	if e != nil || result["current"] != coreHash(old) {
		t.Fatalf("rollback: %v %v", result, e)
	}
	calls := len(r.calls)
	result, e = m.execute(ctx, req)
	if e != nil || result["replayed"] != true || len(r.calls) != calls {
		t.Fatal("rollback replay toggled version")
	}
	req.Core = "sing-box"
	if _, e = m.execute(ctx, req); e == nil {
		t.Fatal("task identity reuse allowed")
	}
	// A historical registered version is removable; unregistered neighbor is not.
	reg, _ := m.load()
	retired := []byte("\x7fELFretired")
	hash := coreHash(retired)
	m.storeVersion("xray", retired)
	reg.Engines["xray"].Versions[hash] = coreVersion{SHA256: hash, Version: "retired"}
	m.save(reg)
	neighbor := filepath.Join(m.root, "versions", "unregistered-backup")
	os.WriteFile(neighbor, []byte("keep"), 0600)
	req = coreVersionRequest{TaskID: 3, Revision: 3, Core: "xray", Operation: "core.clean"}
	if _, e = m.execute(ctx, req); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(m.versionPath("xray", hash)); !os.IsNotExist(e) {
		t.Fatal("registered history not removed")
	}
	if _, e = os.Stat(neighbor); e != nil {
		t.Fatal("unregistered file touched")
	}
	for _, hash := range []string{coreHash(old), coreHash(newBinary)} {
		if _, e = os.Stat(m.versionPath("xray", hash)); e != nil {
			t.Fatal("current/rollback removed")
		}
	}
}
func TestCoreVersionFailureRestoresBinaryRegistryAndService(t *testing.T) {
	for _, kind := range []string{"checksum", "validation", "restart", "readiness"} {
		t.Run(kind, func(t *testing.T) {
			m, r, old, _ := versionFixture(t)
			switch kind {
			case "checksum":
				os.WriteFile(filepath.Join(m.packages, "xray-2"), []byte("\x7fELFtamper"), 0750)
			case "validation":
				r.rejectConfig = true
			case "restart":
				r.restartFailures = 1
			case "readiness":
				calls := 0
				m.ready = func(context.Context, string) error {
					calls++
					if calls == 1 {
						return errors.New("not ready")
					}
					return nil
				}
			}
			if _, e := m.execute(context.Background(), coreVersionRequest{TaskID: 1, Revision: 1, Core: "xray", Operation: "core.install", Version: "2"}); e == nil {
				t.Fatal("failure marked successful")
			}
			b, _ := os.ReadFile(filepath.Join(m.root, "xray"))
			if string(b) != string(old) || !r.active {
				t.Fatal("failed update did not restore old binary/service")
			}
			reg, e := m.load()
			if e != nil {
				t.Fatal(e)
			}
			if h := reg.Engines["xray"]; h != nil && h.Current != "" && h.Current != coreHash(old) {
				t.Fatal("registry drift after failure")
			}
		})
	}
}
func TestCoreVersionColdRecoveryAndStoppedService(t *testing.T) {
	m, r, old, newBinary := versionFixture(t)
	ctx := context.Background()
	r.active = false
	req := coreVersionRequest{TaskID: 1, Revision: 1, Core: "xray", Operation: "core.install", Version: "2"}
	if _, e := m.execute(ctx, req); e != nil {
		t.Fatal(e)
	}
	if r.active {
		t.Fatal("install started intentionally stopped service")
	}
	reg, _ := m.load()
	journal := coreJournal{Core: "xray", Hash: coreHash(old), Active: true, Registry: reg}
	journal.Registry.Engines["xray"].Current = coreHash(old)
	b, _ := json.Marshal(journal)
	os.WriteFile(filepath.Join(m.root, "version-transaction.json"), b, 0640)
	os.WriteFile(filepath.Join(m.root, "xray"), newBinary, 0750)
	if e := m.recover(ctx); e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(filepath.Join(m.root, "xray"))
	if string(raw) != string(old) || !r.active {
		t.Fatal("interrupted transaction not recovered")
	}
}
func TestCoreVersionRejectsPathsAndUnapprovedPackages(t *testing.T) {
	m, _, _, _ := versionFixture(t)
	for _, req := range []coreVersionRequest{{TaskID: 1, Revision: 1, Core: "../x-ui", Operation: "core.install", Version: "2"}, {TaskID: 1, Revision: 1, Core: "xray", Operation: "core.install", Version: "../2"}, {TaskID: 1, Revision: 1, Core: "xray", Operation: "core.install", Version: "unapproved"}} {
		if _, e := m.execute(context.Background(), req); e == nil {
			t.Fatal("unsafe package accepted")
		}
	}
}
