package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

type certificateVersion struct {
	ID      int64     `json:"certificate_id"`
	Created time.Time `json:"created_at"`
	PEMHash string    `json:"pem_hash"`
	KeyHash string    `json:"key_hash"`
}
type certificateCleanJob struct {
	Digest   string   `json:"digest"`
	Versions []string `json:"versions"`
	Done     bool     `json:"done"`
}
type certificateRegistry struct {
	Versions map[string]certificateVersion `json:"versions"`
	Jobs     map[int]certificateCleanJob   `json:"jobs"`
	LastTask int                           `json:"last_task"`
}
type certificateHistoryManager struct{ root, config string }

func (m certificateHistoryManager) recover() error {
	r, err := m.load()
	if err != nil {
		return err
	}
	ids := []int{}
	for id, job := range r.Jobs {
		if !job.Done {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		if _, err = m.clean(id, r.Jobs[id].Digest); err != nil {
			return err
		}
	}
	return nil
}

var certificateVersionName = regexp.MustCompile(`^[1-9][0-9]*-[a-f0-9]{32}$`)

func newCertificateHistoryManager() certificateHistoryManager {
	return certificateHistoryManager{"/etc/migate-gate/certs", "/etc/migate-gate/cores"}
}
func (m certificateHistoryManager) load() (certificateRegistry, error) {
	r := certificateRegistry{Versions: map[string]certificateVersion{}, Jobs: map[int]certificateCleanJob{}}
	info, err := os.Lstat(m.root)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return r, errors.New("unsafe_certificate_directory")
	}
	name := filepath.Join(m.root, "versions.json")
	info, err = os.Lstat(name)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return r, errors.New("unsafe_certificate_registry")
	}
	b, err := os.ReadFile(name)
	if err != nil || json.Unmarshal(b, &r) != nil || r.Versions == nil || r.Jobs == nil {
		return r, errors.New("invalid_certificate_registry")
	}
	for name, v := range r.Versions {
		if !certificateVersionName.MatchString(name) || v.ID <= 0 || !coreDigest.MatchString(v.PEMHash) || !coreDigest.MatchString(v.KeyHash) || v.Created.IsZero() {
			return r, errors.New("invalid_certificate_registry")
		}
	}
	for _, job := range r.Jobs {
		for _, name := range job.Versions {
			if !certificateVersionName.MatchString(name) {
				return r, errors.New("invalid_certificate_registry")
			}
		}
	}
	return r, nil
}
func (m certificateHistoryManager) save(r certificateRegistry) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return coreAtomic(filepath.Join(m.root, "versions.json"), b, 0640)
}
func (m certificateHistoryManager) forgetMissing() error {
	r, err := m.load()
	if err != nil {
		return err
	}
	changed := false
	for name := range r.Versions {
		_, a := os.Lstat(filepath.Join(m.root, name+".pem"))
		_, b := os.Lstat(filepath.Join(m.root, name+".key"))
		if os.IsNotExist(a) && os.IsNotExist(b) {
			delete(r.Versions, name)
			changed = true
		}
	}
	if changed {
		return m.save(r)
	}
	return nil
}
func (m certificateHistoryManager) register(c ManagedCertificate, base string) error {
	r, err := m.load()
	if err != nil {
		return err
	}
	name := filepath.Base(base)
	if !certificateVersionName.MatchString(name) || filepath.Dir(base) != m.root {
		return errors.New("unsafe_certificate_version")
	}
	v := certificateVersion{ID: c.ID, Created: time.Now().UTC(), PEMHash: coreHash([]byte(c.Certificate)), KeyHash: coreHash([]byte(c.Key))}
	if old, ok := r.Versions[name]; ok {
		if old.ID != v.ID || old.PEMHash != v.PEMHash || old.KeyHash != v.KeyHash {
			return errors.New("certificate_registry_conflict")
		}
		return nil
	}
	r.Versions[name] = v
	return m.save(r)
}

// No directory discovery: only registered pairs may become cleanup candidates.
// Preserve the newest two registered versions per certificate and every file
// still referenced by a current or candidate core configuration.
func (m certificateHistoryManager) protected(r certificateRegistry) (map[string]bool, error) {
	protected := map[string]bool{}
	groups := map[int64][]string{}
	for name, v := range r.Versions {
		groups[v.ID] = append(groups[v.ID], name)
	}
	for _, names := range groups {
		sort.Slice(names, func(i, j int) bool {
			a, b := r.Versions[names[i]], r.Versions[names[j]]
			if a.Created.Equal(b.Created) {
				return names[i] > names[j]
			}
			return a.Created.After(b.Created)
		})
		for i, name := range names {
			if i < 2 {
				protected[name] = true
			}
		}
	}
	for _, core := range []string{"xray", "sing-box"} {
		for _, suffix := range []string{".json", ".candidate.json"} {
			name := filepath.Join(m.config, core+suffix)
			info, err := os.Lstat(name)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
				return nil, errors.New("unsafe_certificate_config_reference")
			}
			b, err := os.ReadFile(name)
			if err != nil {
				return nil, err
			}
			var config any
			if json.Unmarshal(b, &config) != nil {
				return nil, errors.New("invalid_certificate_config_reference")
			}
			var walk func(any)
			walk = func(value any) {
				switch v := value.(type) {
				case string:
					if filepath.Dir(v) == m.root {
						base := filepath.Base(v)
						for _, ext := range []string{".pem", ".key"} {
							if filepath.Ext(base) == ext {
								protected[base[:len(base)-len(ext)]] = true
							}
						}
					}
				case []any:
					for _, x := range v {
						walk(x)
					}
				case map[string]any:
					for _, x := range v {
						walk(x)
					}
				}
			}
			walk(config)
		}
	}
	return protected, nil
}
func (m certificateHistoryManager) verify(name string, v certificateVersion, missingOK bool) error {
	for ext, want := range map[string]string{".pem": v.PEMHash, ".key": v.KeyHash} {
		p := filepath.Join(m.root, name+ext)
		info, err := os.Lstat(p)
		if os.IsNotExist(err) && missingOK {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
			return errors.New("unsafe_certificate_history_file")
		}
		body, err := os.ReadFile(p)
		if err != nil || coreHash(body) != want {
			return errors.New("certificate_history_checksum_mismatch")
		}
	}
	return nil
}
func (m certificateHistoryManager) inspect() (map[string]any, error) {
	r, err := m.load()
	if err != nil {
		return nil, err
	}
	protected, err := m.protected(r)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for name := range r.Versions {
		names = append(names, name)
	}
	sort.Strings(names)
	items := []map[string]any{}
	candidates := []string{}
	for _, name := range names {
		v := r.Versions[name]
		if err = m.verify(name, v, false); err != nil {
			return nil, err
		}
		keep := protected[name]
		if len(items) < 128 {
			items = append(items, map[string]any{"version": name, "certificate_id": v.ID, "created_at": v.Created, "protected": keep})
		}
		if !keep {
			candidates = append(candidates, name)
		}
	}
	b, _ := json.Marshal(candidates)
	return map[string]any{"validity": "fresh", "observed_at": time.Now().UTC(), "source": "gate-helper-registry", "items": items, "total": len(names), "removable": len(candidates), "preview_digest": coreHash(b)}, nil
}
func (m certificateHistoryManager) clean(task int, digest string) (map[string]any, error) {
	if task <= 0 || !coreDigest.MatchString(digest) {
		return nil, errors.New("invalid_certificate_cleanup")
	}
	r, err := m.load()
	if err != nil {
		return nil, err
	}
	job, replay := r.Jobs[task]
	if replay && job.Digest != digest {
		return nil, errors.New("certificate_cleanup_replay_conflict")
	}
	if !replay {
		if task <= r.LastTask {
			return nil, errors.New("stale_certificate_cleanup")
		}
		view, err := m.inspect()
		if err != nil {
			return nil, err
		}
		if view["preview_digest"] != digest {
			return nil, errors.New("certificate_history_changed")
		}
		job = certificateCleanJob{Digest: digest, Versions: []string{}}
		protected, err := m.protected(r)
		if err != nil {
			return nil, err
		}
		for name := range r.Versions {
			if !protected[name] {
				job.Versions = append(job.Versions, name)
			}
		}
		sort.Strings(job.Versions)
		r.Jobs[task] = job
		r.LastTask = task
		if err = m.save(r); err != nil {
			return nil, err
		}
	}
	if !job.Done {
		protected, err := m.protected(r)
		if err != nil {
			return nil, err
		}
		// Preflight every registered pair before deleting even the first file.
		for _, name := range job.Versions {
			v, ok := r.Versions[name]
			if !ok || protected[name] {
				return nil, errors.New("certificate_cleanup_protected")
			}
			if err = m.verify(name, v, replay); err != nil {
				return nil, err
			}
		}
		for _, name := range job.Versions {
			for _, ext := range []string{".pem", ".key"} {
				if err = os.Remove(filepath.Join(m.root, name+ext)); err != nil && !os.IsNotExist(err) {
					return nil, fmt.Errorf("certificate_cleanup_failed")
				}
			}
			delete(r.Versions, name)
		}
		job.Done = true
		r.Jobs[task] = job
		if len(r.Jobs) > 256 {
			ids := []int{}
			for id, j := range r.Jobs {
				if j.Done && id != task {
					ids = append(ids, id)
				}
			}
			sort.Ints(ids)
			for _, id := range ids {
				if len(r.Jobs) <= 256 {
					break
				}
				delete(r.Jobs, id)
			}
		}
		if err = m.save(r); err != nil {
			return nil, err
		}
	}
	return map[string]any{"applied": true, "removed": len(job.Versions), "replayed": replay, "source": "gate-helper-registry"}, nil
}
