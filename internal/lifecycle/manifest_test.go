package lifecycle

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(role string) Manifest {
	root := "/opt/migate-" + role
	return Manifest{Version: 1, Role: role, Current: "v2", Previous: "v1", Resources: []Resource{{root + "/releases", "program"}, {root + "/current", "program"}, {"/etc/migate-" + role, "config"}, {"/var/lib/migate-" + role, "data"}, {"/etc/systemd/system/migate-" + role + ".service", "service"}}}
}
func TestScopes(t *testing.T) {
	if filepath.Separator != '/' {
		t.Skip("Linux filesystem contract")
	}
	m := fixture("gate")
	m.Resources = append(m.Resources, Resource{"/opt/migate-gate/cores", "core"}, Resource{"/etc/systemd/system/migate-gate-xray.service", "core-service"})
	for _, tt := range []struct {
		o           Options
		cores, data bool
	}{{Options{Command: "uninstall"}, false, false}, {Options{Command: "uninstall", WithCores: true}, true, false}, {Options{Command: "uninstall", Purge: true, BackupDir: "/var/backups/migate"}, true, true}} {
		plan, e := Plan(m, tt.o)
		if e != nil {
			t.Fatal(e)
		}
		var core, dat bool
		backup := -1
		stop := -1
		for i, a := range plan {
			if a.Operation == "backup" {
				backup = i
			}
			if a.Operation == "stop-service" {
				stop = i
			}
			if a.Operation == "remove" {
				if a.Target == "/opt/migate-gate/cores" {
					core = true
				}
				if a.Target == "/var/lib/migate-gate" {
					dat = true
				}
			}
		}
		if core != tt.cores || dat != tt.data {
			t.Fatalf("wrong scope %+v", plan)
		}
		if tt.o.Purge && (backup < stop || backup < 0) {
			t.Fatal("backup must follow stopped writers")
		}
	}
	for _, p := range []string{"/", "/opt/x-ui", "/etc/systemd/system/x-ui.service", "/opt/migate-gate/../x-ui"} {
		bad := fixture("gate")
		bad.Resources = append(bad.Resources, Resource{p, "program"})
		if _, e := Plan(bad, Options{Command: "uninstall"}); e == nil {
			t.Fatalf("unowned path accepted: %s", p)
		}
	}
	if _, e := Plan(m, Options{Command: "uninstall", Purge: true, BackupDir: "/var/lib/migate-gate/backup"}); e == nil {
		t.Fatal("backup in purge scope accepted")
	}
}
func TestDryRunAndConfirmation(t *testing.T) {
	if filepath.Separator != '/' {
		t.Skip("Linux contract")
	}
	m := fixture("control")
	if e := Execute(context.Background(), m, Options{Command: "uninstall", DryRun: true}, io.Discard); e != nil {
		t.Fatal(e)
	}
	if e := Execute(context.Background(), m, Options{Command: "uninstall"}, io.Discard); e == nil || !strings.Contains(e.Error(), "confirm") {
		t.Fatal("missing confirmation accepted")
	}
}
func TestRejectSymlinkParents(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if e := os.Mkdir(real, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(real, link); e != nil {
		t.Skip(e)
	}
	if e := checkParents(filepath.Join(link, "child")); e == nil {
		t.Fatal("symlink ancestor accepted")
	}
}
