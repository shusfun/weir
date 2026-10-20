package agent

// The helper only installs packages shipped in a checksum-verified application
// release. Neither the center nor an Agent request can supply a URL or path.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	runtimecmd "github.com/imzyb/MiGate/internal/runtime/command"
)

type corePackage struct {
	Core          string `json:"core"`
	Version       string `json:"version"`
	SHA256        string `json:"sha256"`
	Compatibility string `json:"compatibility,omitempty"`
	Recommended   bool   `json:"recommended,omitempty"`
}
type coreVersion struct {
	Version     string `json:"version"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installed_at"`
}
type coreHistory struct {
	Current  string                 `json:"current"`
	Previous string                 `json:"previous"`
	Versions map[string]coreVersion `json:"versions"`
}
type coreReceipt struct {
	Fingerprint string         `json:"fingerprint"`
	Result      map[string]any `json:"result"`
}
type coreRegistry struct {
	Engines    map[string]*coreHistory `json:"engines"`
	Receipts   map[int]coreReceipt     `json:"receipts"`
	LastTaskID int                     `json:"last_task_id"`
}
type coreVersionRequest struct {
	TaskID    int    `json:"task_id"`
	Revision  int64  `json:"revision"`
	Core      string `json:"core"`
	Operation string `json:"operation"`
	Version   string `json:"version"`
}
type coreJournal struct {
	Core     string       `json:"core"`
	Hash     string       `json:"hash"`
	Active   bool         `json:"active"`
	Registry coreRegistry `json:"registry"`
}
type coreVersionManager struct {
	root, packages, config string
	runner                 runtimecmd.CommandRunner
	ready                  func(context.Context, string) error
}

var coreVersionName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
var coreDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func newCoreVersionManager() coreVersionManager {
	return coreVersionManager{root: "/opt/migate-gate/cores", packages: "/opt/migate-gate/current/core-packages", config: "/etc/migate-gate/cores", runner: runtimecmd.NewRealCommandRunner(20 * time.Second), ready: verifyStatsReady}
}
func managedCore(core string) bool { return core == "xray" || core == "sing-box" }
func regularCoreFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 160<<20 || info.Size() < 4 {
		return nil, errors.New("unsafe_core_file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if string(b[:4]) != "\x7fELF" {
		return nil, errors.New("invalid_core_binary")
	}
	return b, nil
}
func coreHash(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func coreAtomic(path string, b []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("unsafe_core_path")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".migate-core-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func (m coreVersionManager) prepare() error {
	for _, dir := range []string{m.root, filepath.Join(m.root, "versions")} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return err
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe_core_directory")
		}
	}
	return nil
}
func (m coreVersionManager) load() (coreRegistry, error) {
	out := coreRegistry{Engines: map[string]*coreHistory{}, Receipts: map[int]coreReceipt{}}
	b, err := os.ReadFile(filepath.Join(m.root, "versions.json"))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if len(b) > 2<<20 || json.Unmarshal(b, &out) != nil || out.Engines == nil || out.Receipts == nil {
		return out, errors.New("invalid_core_registry")
	}
	for core, h := range out.Engines {
		if !managedCore(core) || h == nil || h.Versions == nil {
			return out, errors.New("invalid_core_registry")
		}
		for hash, v := range h.Versions {
			if !coreDigest.MatchString(hash) || hash != v.SHA256 {
				return out, errors.New("invalid_core_registry")
			}
		}
		for _, hash := range []string{h.Current, h.Previous} {
			if hash != "" {
				if _, ok := h.Versions[hash]; !ok {
					return out, errors.New("invalid_core_registry")
				}
			}
		}
	}
	return out, nil
}
func (m coreVersionManager) save(r coreRegistry) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	return coreAtomic(filepath.Join(m.root, "versions.json"), b, 0640)
}
func (m coreVersionManager) versionPath(core, hash string) string {
	return filepath.Join(m.root, "versions", core+"-"+hash)
}
func (m coreVersionManager) storeVersion(core string, b []byte) error {
	path := m.versionPath(core, coreHash(b))
	if existing, e := regularCoreFile(path); e == nil {
		if coreHash(existing) != coreHash(b) {
			return errors.New("core_archive_checksum_mismatch")
		}
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	return coreAtomic(path, b, 0750)
}
func (m coreVersionManager) catalog() ([]corePackage, error) {
	f, e := os.Open(filepath.Join(m.packages, "catalog.json"))
	if e != nil {
		return nil, errors.New("core_catalog_unavailable")
	}
	defer f.Close()
	var items []corePackage
	if json.NewDecoder(io.LimitReader(f, 65537)).Decode(&items) != nil || len(items) > 32 {
		return nil, errors.New("invalid_core_catalog")
	}
	seen := map[string]bool{}
	for _, p := range items {
		key := p.Core + ":" + p.Version
		if !managedCore(p.Core) || !coreVersionName.MatchString(p.Version) || !coreDigest.MatchString(p.SHA256) || len(p.Compatibility) > 1024 || seen[key] {
			return nil, errors.New("invalid_core_catalog")
		}
		seen[key] = true
	}
	return items, nil
}
func (m coreVersionManager) inspect() map[string]any {
	r, e := m.load()
	items, ce := m.catalog()
	if e != nil || ce != nil {
		return map[string]any{"validity": "unknown", "message": "core_version_inventory_unavailable"}
	}
	return map[string]any{"validity": "fresh", "catalog": items, "engines": r.Engines}
}

// Recover a process interruption before accepting new helper operations. The
// journal contains the prior registry and exact content-addressed binary.
func (m coreVersionManager) recover(ctx context.Context) error {
	path := filepath.Join(m.root, "version-transaction.json")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var j coreJournal
	if len(b) > 2<<20 || json.Unmarshal(b, &j) != nil || !managedCore(j.Core) || (j.Hash != "" && !coreDigest.MatchString(j.Hash)) {
		return errors.New("invalid_core_recovery_journal")
	}
	unit := "migate-gate-" + j.Core + ".service"
	if _, err = m.runner.RunOutput(ctx, "systemctl", "stop", unit); err != nil {
		return errors.New("core_recovery_stop_failed")
	}
	target := filepath.Join(m.root, j.Core)
	if j.Hash == "" {
		err = os.Remove(target)
		if os.IsNotExist(err) {
			err = nil
		}
	} else {
		var old []byte
		old, err = regularCoreFile(m.versionPath(j.Core, j.Hash))
		if err == nil && coreHash(old) != j.Hash {
			err = errors.New("core_recovery_checksum_mismatch")
		}
		if err == nil {
			err = coreAtomic(target, old, 0755)
		}
	}
	if err != nil {
		return err
	}
	if err = m.save(j.Registry); err != nil {
		return err
	}
	if j.Active {
		if _, err = m.runner.RunOutput(ctx, "systemctl", "restart", unit); err != nil {
			return errors.New("core_recovery_restart_failed")
		}
		if err = m.ready(ctx, j.Core); err != nil {
			return errors.New("core_recovery_readiness_failed")
		}
	}
	return os.Remove(path)
}
func (m coreVersionManager) execute(ctx context.Context, req coreVersionRequest) (result map[string]any, resultErr error) {
	if req.TaskID <= 0 || req.Revision <= 0 || !managedCore(req.Core) {
		return nil, errors.New("invalid_core_version_request")
	}
	if req.Operation != "core.install" && req.Operation != "core.rollback" && req.Operation != "core.clean" {
		return nil, errors.New("unsupported_core_version_operation")
	}
	if req.Operation == "core.install" && !coreVersionName.MatchString(req.Version) {
		return nil, errors.New("invalid_core_version")
	}
	if err := m.prepare(); err != nil {
		return nil, err
	}
	if err := m.recover(ctx); err != nil {
		return nil, err
	}
	registry, err := m.load()
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(req)
	fingerprint := coreHash(raw)
	if receipt, ok := registry.Receipts[req.TaskID]; ok {
		if receipt.Fingerprint != fingerprint {
			return nil, errors.New("core_task_identity_conflict")
		}
		result := map[string]any{}
		for key, value := range receipt.Result {
			result[key] = value
		}
		result["replayed"] = true
		return result, nil
	}
	if req.TaskID <= registry.LastTaskID {
		return nil, errors.New("core_task_receipt_retired")
	}
	h := registry.Engines[req.Core]
	if h == nil {
		h = &coreHistory{Versions: map[string]coreVersion{}}
		registry.Engines[req.Core] = h
	}
	binary := filepath.Join(m.root, req.Core)
	existing, readErr := regularCoreFile(binary)
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, readErr
	}
	if readErr == nil {
		hash := coreHash(existing)
		if h.Current != "" && h.Current != hash {
			return nil, errors.New("managed_core_drift_detected")
		}
		if h.Current == "" {
			if err = m.storeVersion(req.Core, existing); err != nil {
				return nil, err
			}
			h.Current = hash
			h.Versions[hash] = coreVersion{Version: "imported-" + hash[:12], SHA256: hash, InstalledAt: time.Now().UTC().Format(time.RFC3339)}
		}
	} else if h.Current != "" {
		return nil, errors.New("managed_core_missing")
	}
	var candidate []byte
	targetHash := ""
	if req.Operation == "core.install" {
		catalog, e := m.catalog()
		if e != nil {
			return nil, e
		}
		var selected *corePackage
		for i := range catalog {
			if catalog[i].Core == req.Core && catalog[i].Version == req.Version {
				selected = &catalog[i]
				break
			}
		}
		if selected == nil {
			return nil, errors.New("core_version_not_in_catalog")
		}
		candidate, err = regularCoreFile(filepath.Join(m.packages, req.Core+"-"+selected.Version))
		if err != nil {
			return nil, err
		}
		targetHash = coreHash(candidate)
		if targetHash != selected.SHA256 {
			return nil, errors.New("core_package_checksum_mismatch")
		}
		if len(h.Versions) >= 32 {
			if _, ok := h.Versions[targetHash]; !ok {
				return nil, errors.New("core_history_limit_clean_required")
			}
		}
		if err = m.storeVersion(req.Core, candidate); err != nil {
			return nil, err
		}
		h.Versions[targetHash] = coreVersion{Version: selected.Version, SHA256: targetHash, InstalledAt: time.Now().UTC().Format(time.RFC3339)}
	} else if req.Operation == "core.rollback" {
		targetHash = h.Previous
		if targetHash == "" {
			return nil, errors.New("core_rollback_unavailable")
		}
		candidate, err = regularCoreFile(m.versionPath(req.Core, targetHash))
		if err != nil || coreHash(candidate) != targetHash {
			return nil, errors.New("core_rollback_checksum_mismatch")
		}
	}
	// Stage only registered, verified artifacts. This state is also the exact
	// recovery baseline if validation or service readiness subsequently fails.
	if err = m.save(registry); err != nil {
		return nil, err
	}
	if req.Operation == "core.clean" {
		removed := []string{}
		for hash := range h.Versions {
			if hash == h.Current || hash == h.Previous {
				continue
			}
			path := m.versionPath(req.Core, hash)
			info, e := os.Lstat(path)
			if e == nil && !info.Mode().IsRegular() {
				return nil, errors.New("unsafe_core_archive")
			}
			if e != nil && !os.IsNotExist(e) {
				return nil, e
			}
			if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
				return nil, e
			}
			delete(h.Versions, hash)
			removed = append(removed, hash)
		}
		result = map[string]any{"applied": true, "core": req.Core, "removed": removed, "current": h.Current, "previous": h.Previous}
	} else {
		config := filepath.Join(m.config, req.Core+".json")
		candidatePath := m.versionPath(req.Core, targetHash)
		args := []string{"run", "-test", "-config", config}
		if req.Core == "sing-box" {
			args = []string{"check", "-c", config}
		}
		if _, e := os.Stat(config); e == nil {
			if _, e = m.runner.RunOutput(ctx, candidatePath, args...); e != nil {
				return nil, errors.New("core_candidate_config_rejected")
			}
		} else if !os.IsNotExist(e) {
			return nil, e
		}
		if _, e := m.runner.RunOutput(ctx, candidatePath, "version"); e != nil {
			return nil, errors.New("core_candidate_version_rejected")
		}
		unit := "migate-gate-" + req.Core + ".service"
		_, activeErr := m.runner.RunOutput(ctx, "systemctl", "is-active", "--quiet", unit)
		active := activeErr == nil
		journal := coreJournal{Core: req.Core, Hash: h.Current, Active: active, Registry: registry}
		b, _ := json.Marshal(journal)
		if err = coreAtomic(filepath.Join(m.root, "version-transaction.json"), b, 0640); err != nil {
			return nil, err
		}
		defer func() {
			if resultErr != nil {
				recoverCtx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
				defer cancel()
				resultErr = errors.Join(resultErr, m.recover(recoverCtx))
			}
		}()
		if active {
			if _, err = m.runner.RunOutput(ctx, "systemctl", "stop", unit); err != nil {
				return nil, errors.New("core_stop_before_switch_failed")
			}
		}
		if err = coreAtomic(binary, candidate, 0755); err != nil {
			return nil, err
		}
		if active {
			if _, err = m.runner.RunOutput(ctx, "systemctl", "restart", unit); err != nil {
				return nil, errors.New("core_candidate_restart_failed")
			}
			if err = m.ready(ctx, req.Core); err != nil {
				return nil, errors.New("core_candidate_readiness_failed")
			}
		}
		if h.Current != targetHash {
			h.Previous = h.Current
			h.Current = targetHash
		}
		result = map[string]any{"applied": true, "core": req.Core, "current": h.Current, "previous": h.Previous, "version": h.Versions[targetHash].Version, "service_restarted": active}
	}
	registry.LastTaskID = req.TaskID
	for id := range registry.Receipts {
		if id < req.TaskID-256 {
			delete(registry.Receipts, id)
		}
	}
	registry.Receipts[req.TaskID] = coreReceipt{Fingerprint: fingerprint, Result: result}
	if err = m.save(registry); err != nil {
		return nil, err
	}
	if req.Operation != "core.clean" {
		if err = os.Remove(filepath.Join(m.root, "version-transaction.json")); err != nil {
			return nil, fmt.Errorf("core_journal_commit_failed: %w", err)
		}
	}
	return result, nil
}
