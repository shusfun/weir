package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Resource struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}
type Manifest struct {
	Version   int        `json:"version"`
	Role      string     `json:"role"`
	Current   string     `json:"current"`
	Previous  string     `json:"previous"`
	Releases  []string   `json:"releases"`
	Resources []Resource `json:"resources"`
}
type Options struct {
	Command   string
	WithCores bool
	Purge     bool
	DryRun    bool
	Confirm   string
	BackupDir string
	PGDump    string
	PGRestore string
	Snapshot  string
}
type Action struct {
	Operation string `json:"operation"`
	Target    string `json:"target"`
}

func Load(path string) (Manifest, error) {
	var m Manifest
	b, e := os.ReadFile(path)
	if e != nil {
		return m, e
	}
	e = json.Unmarshal(b, &m)
	if e != nil {
		return m, e
	}
	return m, validate(m)
}
func validate(m Manifest) error {
	if m.Version != 1 || (m.Role != "control" && m.Role != "gate") {
		return errors.New("unsupported installation manifest")
	}
	root := "/opt/migate-" + m.Role
	for _, v := range append([]string{m.Current, m.Previous}, m.Releases...) {
		if v != "" && (filepath.Base(v) != v || v == "." || v == "..") {
			return errors.New("invalid release name")
		}
	}
	allowed := map[string]bool{root: true, "/etc/migate-" + m.Role: true, "/var/lib/migate-" + m.Role: true}
	for _, r := range m.Resources {
		p := filepath.Clean(r.Path)
		if p != r.Path || !filepath.IsAbs(p) {
			return errors.New("noncanonical resource path")
		}
		switch r.Kind {
		case "transport-key":
			if m.Role != "control" || p != "/var/lib/migate-tunnel/.ssh/authorized_keys" {
				return errors.New("unowned transport key")
			}
		case "program":
			if p != root+"/releases" && p != root+"/current" && p != root+"/previous" && p != root+"/migate-manage" {
				return errors.New("program outside owned paths")
			}
		case "core":
			if m.Role != "gate" || (p != "/opt/migate-gate/cores" && p != "/etc/migate-gate/cores") {
				return errors.New("core outside owned paths")
			}
		case "config", "data":
			if !allowed[p] || p == root {
				return errors.New("data outside owned paths")
			}
		case "service", "core-service":
			names := map[string]bool{"migate-control.service": m.Role == "control", "migate-gate.service": m.Role == "gate", "migate-gate-helper.service": m.Role == "gate", "migate-gate-tunnel.service": m.Role == "gate", "migate-gate-xray.service": m.Role == "gate", "migate-gate-sing-box.service": m.Role == "gate"}
			if filepath.Dir(p) != "/etc/systemd/system" || !names[filepath.Base(p)] {
				return errors.New("unowned service")
			}
			if (r.Kind == "core-service") != (filepath.Base(p) == "migate-gate-xray.service" || filepath.Base(p) == "migate-gate-sing-box.service") {
				return errors.New("incorrect service classification")
			}
		default:
			return errors.New("unknown resource kind")
		}
	}
	return nil
}

func Plan(m Manifest, o Options) ([]Action, error) {
	if err := validate(m); err != nil {
		return nil, err
	}
	if o.Command == "restore" {
		if !filepath.IsAbs(o.Snapshot) {
			return nil, errors.New("absolute snapshot manifest required")
		}
		snapshot, err := readSnapshot(o.Snapshot)
		if err != nil {
			return nil, err
		}
		if snapshot.Manifest.Role != m.Role {
			return nil, errors.New("snapshot role mismatch")
		}
		return []Action{{"verify-snapshot", o.Snapshot}, {"stop-service", "migate-" + m.Role + ".service"}, {"restore-data", "owned configuration, data and namespace"}}, nil
	}
	if o.Command != "clean" && o.Command != "uninstall" && o.Command != "backup" {
		return nil, errors.New("expected clean, backup, restore or uninstall")
	}
	if o.Command == "backup" {
		if !filepath.IsAbs(o.BackupDir) {
			return nil, errors.New("absolute external backup directory required")
		}
		for _, r := range m.Resources {
			if within(r.Path, o.BackupDir) {
				return nil, errors.New("backup inside owned scope")
			}
		}
		actions := []Action{}
		for _, r := range m.Resources {
			if r.Kind == "service" || r.Kind == "core-service" {
				actions = append(actions, Action{"stop-service", filepath.Base(r.Path)})
			}
		}
		return append(actions, Action{"backup", o.BackupDir}), nil
	}
	if o.Command == "clean" && (o.Purge || o.WithCores) {
		return nil, errors.New("clean does not accept removal scope flags")
	}
	var actions []Action
	root := "/opt/migate-" + m.Role
	if o.Command == "clean" {
		for _, r := range m.Resources {
			if r.Kind != "program" || r.Path != root+"/releases" {
				continue
			}
			for _, version := range m.Releases {
				if version == m.Current || version == m.Previous {
					continue
				}
				p := filepath.Join(r.Path, version)
				info, err := os.Lstat(p)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					return nil, err
				}
				if info.Mode()&os.ModeSymlink != 0 {
					return nil, errors.New("unexpected release symlink")
				}
				actions = append(actions, Action{"remove", p})
			}
		}
		return actions, nil
	}
	if m.Role == "gate" && !o.Purge {
		actions = append(actions, Action{"revoke-device", "center (report failure if unreachable)"})
	}
	// Quiesce writers before backup and data removal.
	for _, r := range m.Resources {
		if r.Kind == "service" || r.Kind == "core-service" && (o.WithCores || o.Purge) {
			actions = append(actions, Action{"stop-service", filepath.Base(r.Path)})
		}
	}
	if o.Purge {
		if !filepath.IsAbs(o.BackupDir) {
			return nil, errors.New("purge requires absolute backup directory")
		}
		for _, r := range m.Resources {
			if within(r.Path, o.BackupDir) {
				return nil, errors.New("backup must be outside removal scope")
			}
		}
		actions = append(actions, Action{"backup", o.BackupDir})
		if m.Role == "gate" {
			actions = append(actions, Action{"revoke-device", "center (report failure if unreachable)"})
		}
		if m.Role == "control" {
			actions = append(actions, Action{"drop-schema", "migate"}, Action{"delete-namespace", "migate:*"})
		}
	}
	for _, r := range m.Resources {
		remove := r.Kind == "program" || r.Kind == "service" || r.Kind == "transport-key" || (r.Kind == "core" || r.Kind == "core-service") && (o.WithCores || o.Purge) || (r.Kind == "config" || r.Kind == "data") && o.Purge
		if remove {
			actions = append(actions, Action{"remove", r.Path})
		}
	}
	return actions, nil
}
func within(base, p string) bool {
	rel, e := filepath.Rel(base, p)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func checkParents(p string) error {
	for q := filepath.Dir(p); q != filepath.Dir(q); q = filepath.Dir(q) {
		info, e := os.Lstat(q)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink ancestor rejected: %s", q)
		}
	}
	return nil
}
