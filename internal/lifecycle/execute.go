package lifecycle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/imzyb/MiGate/internal/agent"
	"github.com/imzyb/MiGate/internal/control"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

func Execute(ctx context.Context, m Manifest, o Options, out io.Writer) (resultErr error) {
	actions, e := Plan(m, o)
	if e != nil {
		return e
	}
	b, _ := json.MarshalIndent(actions, "", "  ")
	fmt.Fprintln(out, string(b))
	if o.DryRun {
		return nil
	}
	expected := m.Role + ":" + o.Command
	if o.Purge {
		expected += ":purge"
	} else if o.WithCores {
		expected += ":with-cores"
	}
	if o.Confirm != expected {
		return fmt.Errorf("explicit --confirm %s required", expected)
	}
	if o.Command == "restore" {
		return restore(ctx, m, o, out)
	}
	// Backups pause writers but must not disable units or leave a working
	// installation stopped on either success or failure.
	destructive := false
	if o.Command == "backup" || o.Purge {
		active := []string{}
		enabled := []string{}
		for _, action := range actions {
			if action.Operation == "stop-service" {
				if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", action.Target).Run() == nil {
					active = append(active, action.Target)
				}
				if exec.CommandContext(ctx, "systemctl", "is-enabled", "--quiet", action.Target).Run() == nil {
					enabled = append(enabled, action.Target)
				}
			}
		}
		defer func() {
			if o.Purge && destructive {
				return
			}
			resumeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if o.Purge {
				for _, unit := range enabled {
					if err := exec.CommandContext(resumeCtx, "systemctl", "enable", unit).Run(); err != nil {
						resultErr = errors.Join(resultErr, fmt.Errorf("failed to re-enable %s: %w", unit, err))
					}
				}
			}
			for i := len(active) - 1; i >= 0; i-- {
				if err := exec.CommandContext(resumeCtx, "systemctl", "start", active[i]).Run(); err != nil {
					fmt.Fprintf(out, "WARNING: failed to resume %s\n", active[i])
					resultErr = errors.Join(resultErr, fmt.Errorf("failed to resume %s: %w", active[i], err))
				}
			}
		}()
	}
	for _, a := range actions {
		if a.Operation == "remove" {
			if e = checkParents(a.Target); e != nil {
				return e
			}
		}
	}
	var cfg control.Config
	if (o.Purge || o.Command == "backup") && m.Role == "control" {
		cfg, e = control.LoadConfig("/etc/migate-control/config.json")
		if e != nil {
			return e
		}
		if o.Purge && !cfg.Installed {
			return errors.New("installed config required for data purge")
		}
	}
	for _, a := range actions {
		switch a.Operation {
		case "revoke-device":
			destructive = true
			cfg, err := agent.Load("/etc/migate-gate/agent.json")
			if os.IsNotExist(err) {
				fmt.Fprintln(out, "device configuration absent; revocation not verified")
				continue
			}
			if err != nil {
				return err
			}
			a, err := agent.New(cfg)
			if err != nil {
				return err
			}
			err = a.Revoke(ctx)
			a.DB.Close()
			if err != nil {
				fmt.Fprintln(out, "WARNING: device revocation incomplete; revoke manually at center")
			}
		case "backup":
			if e = backup(ctx, m, cfg, o); e != nil {
				return fmt.Errorf("backup failed; purge stopped: %w", e)
			}
			if o.Purge {
				// Pause only until the backup commits. Failed preparation must not
				// disable autostart or revoke the device identity.
				for _, action := range actions {
					if action.Operation == "stop-service" {
						if e = exec.CommandContext(ctx, "systemctl", "disable", action.Target).Run(); e != nil {
							return e
						}
					}
				}
			}
		case "drop-schema":
			destructive = true
			// The schema is a fixed identifier, never a supplied name.
			pc, err := pgx.ParseConfig(pgDSN(cfg.Database))
			if err != nil {
				return err
			}
			pc.Password = cfg.Database.Password
			conn, err := pgx.ConnectConfig(ctx, pc)
			if err != nil {
				return err
			}
			_, err = conn.Exec(ctx, "DROP SCHEMA IF EXISTS migate CASCADE")
			conn.Close(ctx)
			if err != nil {
				return err
			}
		case "delete-namespace":
			destructive = true
			r := redisConnection(cfg.Redis)
			defer r.Close()
			// Require all writers stopped. SCAN/UNLINK only application-prefixed keys.
			var cursor uint64
			for {
				keys, next, err := r.Scan(ctx, cursor, "migate:*", 100).Result()
				if err != nil {
					return err
				}
				for _, k := range keys {
					if !strings.HasPrefix(k, "migate:") {
						return errors.New("unexpected key")
					}
					if err = r.Unlink(ctx, k).Err(); err != nil {
						return err
					}
				}
				cursor = next
				if cursor == 0 {
					break
				}
			}
		case "stop-service":
			args := []string{"stop", a.Target}
			if o.Command == "uninstall" && !o.Purge {
				args = []string{"disable", "--now", a.Target}
			}
			if e = exec.CommandContext(ctx, "systemctl", args...).Run(); e != nil { // An already removed unit is harmless; an active failure is not.
				if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", a.Target).Run() == nil {
					return fmt.Errorf("cannot stop %s", a.Target)
				}
			}
		case "remove":
			destructive = true
			if e = checkParents(a.Target); e != nil {
				return e
			}
			if e = os.RemoveAll(a.Target); e != nil {
				return e
			}
		}
	}
	if o.Command == "uninstall" {
		if e = exec.CommandContext(ctx, "systemctl", "daemon-reload").Run(); e != nil {
			return e
		}
		// Only remove an empty, fixed installation root. Leaving it after purge
		// would make the installer correctly reject it as an unmanaged directory.
		root := "/opt/migate-" + m.Role
		if entries, err := os.ReadDir(root); err == nil && len(entries) == 0 {
			if err = os.Remove(root); err != nil {
				return err
			}
		}
	}
	return nil
}
func pgDSN(c control.DatabaseConfig) string {
	if c.Port == 0 {
		c.Port = 5432
	}
	if c.SSLMode == "" {
		c.SSLMode = "disable"
	}
	// pgx uses URL encoding through ParseConfig below; avoid embedding secrets in child args.
	return fmt.Sprintf("host=%s port=%d user=%s dbname=%s sslmode=%s", quote(c.Host), c.Port, quote(c.User), quote(c.Database), quote(c.SSLMode))
}
func quote(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "'", "\\'") + "'"
}
func redisConnection(c control.RedisConfig) *redis.Client {
	opts := &redis.Options{Addr: c.Address, Username: c.Username, Password: c.Password}
	if c.TLS {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return redis.NewClient(opts)
}
func backup(ctx context.Context, m Manifest, cfg control.Config, o Options) error {
	if e := checkParents(filepath.Join(o.BackupDir, "placeholder")); e != nil {
		return e
	}
	if e := os.MkdirAll(o.BackupDir, 0700); e != nil {
		return e
	}
	prefix := filepath.Join(o.BackupDir, "migate-"+m.Role+"-"+time.Now().UTC().Format("20060102T150405.000000000"))
	if m.Role == "control" && cfg.Installed {
		if o.PGDump == "" {
			return errors.New("--pg-dump executable required for complete PostgreSQL backup")
		}
		f, e := os.OpenFile(prefix+".pgdump", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		cmd := exec.CommandContext(ctx, o.PGDump, "--dbname", pgDSN(cfg.Database), "--schema=migate", "--format=custom", "--no-owner", "--no-acl")
		cmd.Env = append(os.Environ(), "PGPASSWORD="+cfg.Database.Password)
		cmd.Stdout = f
		e = cmd.Run()
		syncErr := f.Sync()
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		r := redisConnection(cfg.Redis)
		defer r.Close()
		type entry struct {
			Key       string `json:"key"`
			Dump      []byte `json:"dump"`
			TTLMillis int64  `json:"ttl_ms"`
		}
		entries := []entry{}
		var cursor uint64
		for {
			keys, next, err := r.Scan(ctx, cursor, "migate:*", 100).Result()
			if err != nil {
				return err
			}
			for _, key := range keys {
				if !strings.HasPrefix(key, "migate:") {
					return errors.New("unexpected key")
				}
				dump, err := r.Dump(ctx, key).Bytes()
				if err == redis.Nil {
					continue
				}
				if err != nil {
					return err
				}
				ttl, err := r.PTTL(ctx, key).Result()
				if err != nil {
					return err
				}
				if ttl == -2 {
					continue
				}
				ttlMillis := ttl.Milliseconds()
				if ttl == -1 {
					ttlMillis = -1
				}
				entries = append(entries, entry{key, dump, ttlMillis})
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		rb, err := json.Marshal(entries)
		if err != nil {
			return err
		}
		if err = os.WriteFile(prefix+".redis.json", rb, 0600); err != nil {
			return err
		}
	}
	f, e := os.OpenFile(prefix+".tar.gz", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, r := range m.Resources {
		if r.Kind != "config" && r.Kind != "data" {
			continue
		}
		if e = checkParents(filepath.Join(r.Path, "placeholder")); e != nil {
			return e
		}
		if _, err := os.Stat(r.Path); os.IsNotExist(err) {
			continue
		}
		e = filepath.Walk(r.Path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("backup refuses symlinks")
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = strings.TrimPrefix(filepath.ToSlash(path), "/")
			if err = tw.WriteHeader(header); err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, in)
			in.Close()
			return err
		})
		if e != nil {
			return e
		}
	}
	if e = tw.Close(); e != nil {
		return e
	}
	if e = gz.Close(); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return writeSnapshot(prefix, m, cfg.Installed)
}
