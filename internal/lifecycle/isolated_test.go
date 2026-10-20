package lifecycle

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Execute only inside a disposable Linux container. systemctl is a controlled
// test double: this verifies real filesystem scopes, not systemd/core behavior.
func TestIsolatedUninstallScopes(t *testing.T) {
	if os.Getenv("MIGATE_LIFECYCLE_INTEGRATION") != "1" || filepath.Separator != '/' {
		t.Skip("requires disposable Linux container")
	}
	tools := t.TempDir()
	if e := os.WriteFile(filepath.Join(tools, "systemctl"), []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", tools+":"+os.Getenv("PATH"))
	for _, scope := range []string{"preserve", "cores", "purge", "failed-backup"} {
		t.Run(scope, func(t *testing.T) {
			m := fixture("gate")
			m.Resources = append(m.Resources, Resource{"/opt/migate-gate/cores", "core"}, Resource{"/etc/migate-gate/cores", "core"}, Resource{"/etc/systemd/system/migate-gate-xray.service", "core-service"})
			m.Releases = []string{"v0", "v1", "v2"}
			for _, p := range []string{"/opt/migate-gate/releases/v0/bin", "/opt/migate-gate/releases/v1/bin", "/opt/migate-gate/releases/v2/bin", "/opt/migate-gate/releases/unregistered/marker", "/opt/migate-gate/cores/xray", "/etc/migate-gate/cores/xray.json", "/etc/migate-gate/installation-state", "/var/lib/migate-gate/agent.db", "/etc/systemd/system/migate-gate.service", "/etc/systemd/system/migate-gate-xray.service"} {
				if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(p, []byte("fixture"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			other := filepath.Join(t.TempDir(), "x-ui-marker")
			os.WriteFile(other, []byte("untouched"), 0600)
			if e := Execute(context.Background(), m, Options{Command: "clean", Confirm: "gate:clean"}, io.Discard); e != nil {
				t.Fatal(e)
			}
			for _, p := range []string{"/opt/migate-gate/releases/v1/bin", "/opt/migate-gate/releases/v2/bin", "/opt/migate-gate/releases/unregistered/marker"} {
				if _, e := os.Stat(p); e != nil {
					t.Fatal("clean removed preserved resource", p)
				}
			}
			if _, e := os.Stat("/opt/migate-gate/releases/v0"); !os.IsNotExist(e) {
				t.Fatal("registered old version not cleaned")
			}
			o := Options{Command: "uninstall", Confirm: "gate:uninstall"}
			if scope == "cores" {
				o.WithCores = true
				o.Confirm += ":with-cores"
			}
			if scope == "purge" || scope == "failed-backup" {
				o.Purge = true
				o.Confirm += ":purge"
				o.BackupDir = t.TempDir()
				if scope == "failed-backup" {
					bad := filepath.Join(o.BackupDir, "not-a-directory")
					os.WriteFile(bad, []byte("fixture"), 0600)
					o.BackupDir = bad
				}
			}
			if scope == "preserve" {
				before, _ := os.ReadFile("/etc/migate-gate/installation-state")
				dry := o
				dry.DryRun = true
				if e := Execute(context.Background(), m, dry, io.Discard); e != nil {
					t.Fatal(e)
				}
				after, _ := os.ReadFile("/etc/migate-gate/installation-state")
				if string(before) != string(after) {
					t.Fatal("dry run mutated data")
				}
			}
			e := Execute(context.Background(), m, o, io.Discard)
			if scope == "failed-backup" {
				if e == nil {
					t.Fatal("backup failure ignored")
				}
				if _, e = os.Stat("/var/lib/migate-gate/agent.db"); e != nil {
					t.Fatal("data deleted after backup failure")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			_, dataErr := os.Stat("/var/lib/migate-gate/agent.db")
			if (scope == "purge") != os.IsNotExist(dataErr) {
				t.Fatalf("data scope mismatch %s %v", scope, dataErr)
			}
			_, coreErr := os.Stat("/opt/migate-gate/cores/xray")
			if (scope != "preserve") != os.IsNotExist(coreErr) {
				t.Fatalf("core scope mismatch %s %v", scope, coreErr)
			}
			if b, e := os.ReadFile(other); e != nil || string(b) != "untouched" {
				t.Fatal("unrelated data changed")
			}
			if scope == "purge" {
				files, _ := filepath.Glob(filepath.Join(o.BackupDir, "*.tar.gz"))
				if len(files) != 1 {
					t.Fatal("external backup missing")
				}
			}
			// Removing already absent resources remains scoped and idempotent.
			if scope == "preserve" {
				if e = Execute(context.Background(), m, o, io.Discard); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
