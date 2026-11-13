package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/gate"
	"github.com/shusfun/weir/internal/layahost"
)

var UnitsDir = "/etc/systemd/system"
var LocalGateConfig = "/etc/weir/gate/agent.json"

func PreflightLocalGate() error {
	if _, err := installArgs(); err != nil {
		return err
	}
	if _, err := currentPackage(); err != nil {
		return errors.New("installed_package_verification_failed")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemd_required")
	}
	return nil
}

// Only the installed root-owned release is executable by this request channel.
func currentPackage() (string, error) {
	link, err := os.Readlink(filepath.Join(Base, "current"))
	if err != nil || filepath.Dir(link) != "releases" || !safeVersion.MatchString(filepath.Base(link)) {
		return "", errors.New("installed_package_invalid")
	}
	dir := filepath.Join(Base, link)
	if err = layahost.TrustedDirectory(dir); err != nil {
		return "", err
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return "", err
	}
	if err = layahost.TrustedFile(filepath.Join(dir, "SHA256SUMS")); err != nil {
		return "", err
	}
	found := map[string]bool{}
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 || len(fields[0]) != 64 {
			return "", errors.New("installed_manifest_invalid")
		}
		name := strings.TrimPrefix(fields[1], "*")
		if filepath.IsAbs(name) || filepath.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") {
			return "", errors.New("installed_manifest_invalid")
		}
		path := filepath.Join(dir, name)
		if err = layahost.TrustedFile(path); err != nil {
			return "", err
		}
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0022 != 0 {
			return "", errors.New("installed_file_invalid")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != strings.ToLower(fields[0]) {
			return "", errors.New("installed_checksum_mismatch")
		}
		found[name] = true
	}
	for _, name := range []string{"weir", "install.sh", "VERSION"} {
		if !found[name] {
			return "", errors.New("installed_manifest_incomplete")
		}
	}
	return dir, nil
}

func (w *writer) enableLocalGate(ctx context.Context) error {
	w.set(func(s *Status) { s.Stage = "local-gate.preflight" })
	args, err := installArgs()
	if err != nil {
		return err
	}
	if _, err := os.Lstat(LocalGateConfig); err == nil {
		cfg, err := gate.LoadConfig(LocalGateConfig)
		if err != nil || cfg.Center != "http://"+loopbackListen(args[1]) {
			return errors.New("local_gate_identity_conflict")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	dir, err := currentPackage()
	if err != nil {
		return err
	}
	w.set(func(s *Status) { s.State = "installing" })
	w.log("启用中心本机执行端；保留中心数据及远程设备身份，服务将短暂重启")
	names := []string{"weir-center.service", "weir-helper.service", "weir-gate.service", "weir-local-pair.service", "weir-dsh.service", "weir-laya-helper.service", "weir-update.path"}
	enabled, active := map[string]bool{}, map[string]bool{}
	for _, name := range names {
		enabled[name] = exec.Command("systemctl", "is-enabled", "--quiet", name).Run() == nil
		active[name] = exec.Command("systemctl", "is-active", "--quiet", name).Run() == nil
	}
	snapshot := map[string][]byte{}
	for _, name := range names {
		b, e := os.ReadFile(filepath.Join(UnitsDir, name))
		if e == nil {
			snapshot[name] = b
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	before, err := os.ReadFile(RoleFile)
	if err != nil {
		return err
	}
	w.set(func(s *Status) { s.Stage = "local-gate.install" })
	if err = runInstaller(ctx, dir, []string{"standalone", args[1]}, w.log); err != nil {
		w.set(func(s *Status) { s.Stage = "local-gate.restore" })
		// Recovery must outlive a cancelled installer, and restore service roles as
		// well as unit contents. Any paired identity is retained for a later retry.
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		var recoveryErrors []error
		restore := func(err error) {
			if err != nil {
				recoveryErrors = append(recoveryErrors, err)
			}
		}
		for _, name := range names {
			_, unitErr := os.Stat(filepath.Join(UnitsDir, name))
			if active[name] || unitErr == nil {
				restore(exec.CommandContext(recovery, "systemctl", "disable", "--now", name).Run())
			}
		}
		for _, name := range names {
			path := filepath.Join(UnitsDir, name)
			if b, ok := snapshot[name]; ok {
				restore(os.WriteFile(path, b, 0644))
			} else {
				if e := os.Remove(path); e != nil && !os.IsNotExist(e) {
					restore(e)
				}
			}
		}
		restore(os.WriteFile(RoleFile, before, 0644))
		restore(exec.CommandContext(recovery, "systemctl", "daemon-reload").Run())
		for _, name := range names {
			if enabled[name] {
				restore(exec.CommandContext(recovery, "systemctl", "enable", name).Run())
			}
			if active[name] {
				restore(exec.CommandContext(recovery, "systemctl", "restart", name).Run())
			}
		}
		if len(recoveryErrors) > 0 {
			return errors.New("local_gate_enable_failed_recovery_incomplete")
		}
		return errors.New("local_gate_enable_failed_restored")
	}
	w.set(func(s *Status) { s.Stage = "local-gate.complete" })
	return nil
}

func loopbackListen(listen string) string {
	i := strings.LastIndex(listen, ":")
	if i < 0 {
		return listen
	}
	return "127.0.0.1" + listen[i:]
}

func (w *writer) prepareLaya(ctx context.Context) error {
	if _, err := role(); err != nil {
		return err
	}
	w.set(func(s *Status) { s.State = "installing" })
	w.log("准备官方 Laya 固定部署档案")
	target := "center"
	if LocalRole() == "gate" {
		target = "gate"
	}
	_, err := layahost.Prepare(ctx, target, func(stage string) {
		w.set(func(s *Status) { s.Stage = stage })
		w.log("laya.prepare." + stage)
	})
	return err
}

// Public state contains no device credential.
func LocalRole() string { b, _ := os.ReadFile(RoleFile); return strings.TrimSpace(string(b)) }

func LocalIdentity() (gate.Config, error) { return gate.LoadConfig(LocalGateConfig) }

func LocalListen() string {
	var cfg struct {
		Listen string `json:"listen"`
	}
	b, _ := os.ReadFile(CenterCfg)
	_ = json.Unmarshal(b, &cfg)
	return "http://" + loopbackListen(cfg.Listen)
}
