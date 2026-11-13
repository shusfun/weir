//go:build linux

package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func localGateFixture(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root-owned installed package; run the privileged selfupdate test binary")
	}
	setup(t, "center", false)
	oldUnits, oldConfig := UnitsDir, LocalGateConfig
	UnitsDir = filepath.Join(Base, "units")
	LocalGateConfig = filepath.Join(Base, "agent.json")
	t.Cleanup(func() { UnitsDir, LocalGateConfig = oldUnits, oldConfig })
	if err := os.MkdirAll(UnitsDir, 0755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(Base, "releases", "0.1.14")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := ""
	for _, name := range []string{"weir", "install.sh", "VERSION"} {
		body := []byte(name)
		if err := os.WriteFile(filepath.Join(dir, name), body, 0755); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		manifest += hex.EncodeToString(sum[:]) + "  " + name + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("releases/0.1.14", filepath.Join(Base, "current")); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(Base, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$1\" in is-enabled|is-active) [ \"$3\" = weir-center.service ] ;; *) exit 0 ;; esac\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return dir
}

func TestLocalGateUsesVerifiedPackageAndPreservesListen(t *testing.T) {
	dir := localGateFixture(t)
	if err := PreflightLocalGate(); err != nil {
		t.Fatal(err)
	}
	calls := 0
	runInstaller = func(_ context.Context, got string, args []string, _ func(string)) error {
		calls++
		if got != dir || strings.Join(args, " ") != "standalone 127.0.0.1:20000" {
			t.Fatalf("unexpected installer %s %v", got, args)
		}
		return nil
	}
	request(t, Request{ID: "local-test-enable", Action: "enable-local-gate"})
	if err := Apply(t.Context()); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	request(t, Request{ID: "local-test-enable", Action: "enable-local-gate"})
	// A completed operation must be returned rather than rerun by the API;
	// the persisted receipt remains readable independently of current status.
	if st, ok := ReadOperation("local-test-enable"); !ok || st.State != "done" {
		t.Fatal(st, ok)
	}
	_ = os.Remove(filepath.Join(Dir, "request.json"))
	if err := os.WriteFile(filepath.Join(dir, "weir"), []byte("tampered"), 0755); err != nil {
		t.Fatal(err)
	}
	if PreflightLocalGate() == nil {
		t.Fatal("tampered current package accepted")
	}
}

func TestLocalGateConflictAndFailedConversionRestoreRole(t *testing.T) {
	localGateFixture(t)
	if err := os.WriteFile(LocalGateConfig, []byte(`{"center":"https://another.example","gate_id":9,"private_key":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	w := &writer{}
	if err := w.enableLocalGate(t.Context()); err == nil || err.Error() != "local_gate_identity_conflict" {
		t.Fatal("identity conflict accepted", err)
	}
	_ = os.Remove(LocalGateConfig)
	unit := filepath.Join(UnitsDir, "weir-center.service")
	if err := os.WriteFile(unit, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	runInstaller = func(_ context.Context, _ string, _ []string, _ func(string)) error {
		_ = os.WriteFile(RoleFile, []byte("standalone"), 0644)
		_ = os.WriteFile(unit, []byte("changed"), 0644)
		_ = os.WriteFile(filepath.Join(UnitsDir, "weir-helper.service"), []byte("new"), 0644)
		return errors.New("fixture installer failed")
	}
	if err := w.enableLocalGate(t.Context()); err == nil || err.Error() != "local_gate_enable_failed_restored" {
		t.Fatal(err)
	}
	if LocalRole() != "center" {
		t.Fatal("role not restored")
	}
	if b, _ := os.ReadFile(unit); string(b) != "original" {
		t.Fatal("unit not restored")
	}
	if _, err := os.Stat(filepath.Join(UnitsDir, "weir-helper.service")); !os.IsNotExist(err) {
		t.Fatal("new unit retained", err)
	}
}
