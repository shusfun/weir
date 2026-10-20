package lifecycle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/imzyb/MiGate/internal/control"
)

type snapshotFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}
type snapshot struct {
	Version   int            `json:"version"`
	CreatedAt time.Time      `json:"created_at"`
	Manifest  Manifest       `json:"manifest"`
	Installed bool           `json:"installed"`
	Files     []snapshotFile `json:"files"`
}

type redisSnapshotEntry struct {
	Key  string `json:"key"`
	Dump []byte `json:"dump"`
	TTL  int64  `json:"ttl_ms"`
}

func ephemeralRedisKey(key string) bool {
	for _, prefix := range []string{"session:", "device-token:", "device-challenge:", "device-recovery:", "mfa:", "limit:", "passkey:", "totp-setup:"} {
		if strings.HasPrefix(key, "migate:"+prefix) {
			return true
		}
	}
	return false
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func writeSnapshot(prefix string, m Manifest, installed bool) error {
	s := snapshot{Version: 1, CreatedAt: time.Now().UTC(), Manifest: m, Installed: installed}
	suffixes := []string{".tar.gz"}
	if m.Role == "control" && installed {
		suffixes = append(suffixes, ".pgdump", ".redis.json")
	}
	for _, suffix := range suffixes {
		h, err := fileHash(prefix + suffix)
		if err != nil {
			return err
		}
		s.Files = append(s.Files, snapshotFile{filepath.Base(prefix + suffix), h})
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(prefix+".snapshot.json", b, 0600)
}
func readSnapshot(path string) (snapshot, error) {
	var s snapshot
	if err := checkParents(path); err != nil {
		return s, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return s, err
	}
	if !info.Mode().IsRegular() {
		return s, errors.New("snapshot must be a regular file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Version != 1 || s.CreatedAt.IsZero() {
		return s, errors.New("invalid snapshot version or timestamp")
	}
	if err = validate(s.Manifest); err != nil {
		return s, err
	}
	prefix := strings.TrimSuffix(filepath.Base(path), ".snapshot.json")
	allowed := map[string]bool{prefix + ".tar.gz": true}
	if s.Manifest.Role == "control" && s.Installed {
		allowed[prefix+".pgdump"] = true
		allowed[prefix+".redis.json"] = true
	}
	if len(s.Files) != len(allowed) {
		return s, errors.New("incomplete snapshot")
	}
	seen := map[string]bool{}
	for _, f := range s.Files {
		if !allowed[f.Name] || seen[f.Name] {
			return s, errors.New("unexpected snapshot artifact")
		}
		seen[f.Name] = true
		p := filepath.Join(filepath.Dir(path), f.Name)
		info, err = os.Lstat(p)
		if err != nil {
			return s, err
		}
		if !info.Mode().IsRegular() {
			return s, errors.New("snapshot artifact must be regular")
		}
		h, e := fileHash(p)
		if e != nil {
			return s, e
		}
		if h != f.SHA256 {
			return s, errors.New("snapshot checksum mismatch")
		}
	}
	return s, nil
}

// Validate the entire archive before touching the target. Links, special files
// and any paths outside the exact role's configuration/data roots are rejected.
func archiveEntries(path, role string, visit func(*tar.Header, io.Reader) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name {
			return errors.New("unsafe archive path")
		}
		p := "/" + name
		if !within("/etc/migate-"+role, p) && !within("/var/lib/migate-"+role, p) {
			return errors.New("archive path outside owned roots")
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return errors.New("archive links and special files rejected")
		}
		if err = checkParents(p); err != nil {
			return err
		}
		if info, e := os.Lstat(p); e == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("unsafe existing restore target")
		}
		if err = visit(h, tr); err != nil {
			return err
		}
	}
}

func restore(ctx context.Context, m Manifest, o Options, out io.Writer) error {
	s, err := readSnapshot(o.Snapshot)
	if err != nil {
		return err
	}
	prefix := strings.TrimSuffix(o.Snapshot, ".snapshot.json")
	var cfg control.Config
	if err = archiveEntries(prefix+".tar.gz", m.Role, func(h *tar.Header, r io.Reader) error {
		if h.Name == "etc/migate-control/config.json" {
			return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&cfg)
		}
		return nil
	}); err != nil {
		return err
	}
	if m.Current != s.Manifest.Current {
		return errors.New("install the snapshot's application version before restoring")
	}
	if s.Installed && m.Role == "control" && (!cfg.Installed || o.PGRestore == "") {
		return errors.New("installed configuration and --pg-restore required")
	}
	// Validate all Redis metadata before stopping services or changing PostgreSQL.
	var entries []redisSnapshotEntry
	if s.Installed && m.Role == "control" {
		b, e := os.ReadFile(prefix + ".redis.json")
		if e != nil {
			return e
		}
		if e = json.Unmarshal(b, &entries); e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Key, "migate:") || seen[entry.Key] || entry.TTL < -1 || len(entry.Dump) == 0 {
				return errors.New("invalid or foreign Redis snapshot entry")
			}
			seen[entry.Key] = true
		}
	}
	if !filepath.IsAbs(o.BackupDir) {
		return errors.New("--backup-dir required for pre-restore protection")
	}
	if _, err = Plan(m, Options{Command: "backup", BackupDir: o.BackupDir}); err != nil {
		return err
	}
	// Preserve the current target before replacement. After purge, an installed
	// version can be restored before its first setup; the archive supplies config.
	currentCfg := control.Config{}
	if m.Role == "control" {
		if _, e := os.Stat("/etc/migate-control/config.json"); e == nil {
			currentCfg, err = control.LoadConfig("/etc/migate-control/config.json")
			if err != nil {
				return err
			}
		}
	}
	for _, r := range m.Resources {
		if r.Kind == "service" || r.Kind == "core-service" {
			if err = exec.CommandContext(ctx, "systemctl", "stop", filepath.Base(r.Path)).Run(); err != nil {
				return err
			}
		}
	}
	if err = backup(ctx, m, currentCfg, o); err != nil {
		return fmt.Errorf("pre-restore backup failed; nothing replaced: %w", err)
	}
	if s.Installed && m.Role == "control" {
		cmd := exec.CommandContext(ctx, o.PGRestore, "--dbname", pgDSN(cfg.Database), "--clean", "--if-exists", "--single-transaction", "--no-owner", "--no-acl", prefix+".pgdump")
		cmd.Env = append(os.Environ(), "PGPASSWORD="+cfg.Database.Password)
		if err = cmd.Run(); err != nil {
			return fmt.Errorf("database restore failed (transaction rolled back): %w", err)
		}
		r := redisConnection(cfg.Redis)
		defer r.Close()
		// Never resurrect sessions, one-time credentials or rate-limit state.
		var cursor uint64
		for {
			keys, next, e := r.Scan(ctx, cursor, "migate:*", 100).Result()
			if e != nil {
				return e
			}
			for _, key := range keys {
				if !strings.HasPrefix(key, "migate:") {
					return errors.New("foreign key")
				}
				if e = r.Unlink(ctx, key).Err(); e != nil {
					return e
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		elapsed := time.Since(s.CreatedAt).Milliseconds()
		for _, entry := range entries {
			if ephemeralRedisKey(entry.Key) {
				continue
			}
			ttl := time.Duration(0)
			if entry.TTL >= 0 {
				remaining := entry.TTL - elapsed
				if remaining <= 0 {
					continue
				}
				ttl = time.Duration(remaining) * time.Millisecond
			}
			if entry.TTL < 0 && entry.TTL != -1 {
				continue
			}
			if err = r.Restore(ctx, entry.Key, ttl, string(entry.Dump)).Err(); err != nil {
				return err
			}
		}
	}
	if m.Role == "gate" {
		// The pre-restore backup above owns the prior SQLite state. Never leave
		// a newer WAL beside an older restored database; archive WALs, if any,
		// will be restored together with their matching database below.
		for _, suffix := range []string{"-wal", "-shm"} {
			p := "/var/lib/migate-gate/agent.db" + suffix
			if err = checkParents(p); err != nil {
				return err
			}
			if info, e := os.Lstat(p); e == nil {
				if !info.Mode().IsRegular() {
					return errors.New("unsafe SQLite sidecar")
				}
				if err = os.Remove(p); err != nil {
					return err
				}
			} else if !os.IsNotExist(e) {
				return e
			}
		}
	}
	err = archiveEntries(prefix+".tar.gz", m.Role, func(h *tar.Header, r io.Reader) error {
		p := "/" + strings.TrimSuffix(h.Name, "/")
		// Keep the actual installation inventory, not a stale list of releases.
		if p == "/etc/migate-"+m.Role+"/install-manifest.json" {
			return nil
		}
		if h.Typeflag == tar.TypeDir {
			if e := os.MkdirAll(p, 0700); e != nil {
				return e
			}
		} else {
			if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
				return e
			}
			f, e := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			_, e = io.Copy(f, r)
			syncErr := f.Sync()
			f.Close()
			if e != nil {
				return e
			}
			if syncErr != nil {
				return syncErr
			}
		}
		if e := os.Chown(p, h.Uid, h.Gid); e != nil {
			return e
		}
		return os.Chmod(p, os.FileMode(h.Mode)&0777)
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Owned data restored. Sessions invalidated. Start the installed services and verify identity, authorization and counters.")
	return nil
}
