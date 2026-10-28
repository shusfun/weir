package center

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent/user"
)

func scriptedSecrets(values ...string) func(string) (string, error) {
	return func(string) (string, error) {
		if len(values) == 0 {
			return "", errors.New("no more input")
		}
		v := values[0]
		values = values[1:]
		return v, nil
	}
}

func TestSetupCLISQLiteThenResetPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	db := filepath.Join(dir, "migate.db")
	// 监听（默认）→ 存储 1 → SQLite 路径 → 邮箱 → 确认
	input := strings.Join([]string{"", "1", db, "Ops@Example.test", "y"}, "\n") + "\n"
	var out bytes.Buffer
	if err := RunSetupCLI(path, strings.NewReader(input), &out, scriptedSecrets("weak", "Passw0rd", "Passw0rd")); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "密码不符合要求") {
		t.Fatalf("weak password must be re-prompted:\n%s", out.String())
	}
	cfg, err := LoadConfig(path)
	if err != nil || !cfg.Installed || cfg.Store.Driver != store.DriverSQLite || cfg.Store.SQLitePath != db {
		t.Fatalf("config = %+v %v", cfg, err)
	}
	if err = RunSetupCLI(path, strings.NewReader(input), &out, scriptedSecrets()); err == nil {
		t.Fatal("second setup must be refused")
	}
	ctx := context.Background()
	if err = ResetAdminPassword(ctx, cfg, "ops@example.test", "weak"); err == nil {
		t.Fatal("weak reset password accepted")
	}
	if err = ResetAdminPassword(ctx, cfg, "nobody@example.test", "N3wPassword"); err == nil {
		t.Fatal("unknown admin accepted")
	}
	if err = ResetAdminPassword(ctx, cfg, "ops@example.test", "N3wPassword"); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, cfg.Store)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, err := st.Ent.User.Query().Where(user.EmailEQ("ops@example.test")).Only(ctx)
	if err != nil || admin.Role != user.RoleAdmin {
		t.Fatalf("admin = %+v %v", admin, err)
	}
}

func TestSetupCLICancelWritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	input := strings.Join([]string{"", "1", filepath.Join(dir, "migate.db"), "ops@example.test", "n"}, "\n") + "\n"
	var out bytes.Buffer
	if err := RunSetupCLI(path, strings.NewReader(input), &out, scriptedSecrets("Passw0rd", "Passw0rd")); err == nil {
		t.Fatal("cancel must return an error")
	}
	if cfg, _ := LoadConfig(path); cfg.Installed {
		t.Fatal("cancelled setup must not install")
	}
}

// 菜单的卸载只调用本机安装脚本：取消不调用，确认后按选择传 uninstall / uninstall --purge，卸载后菜单退出。
func TestOperatorMenuUninstallDelegatesToInstaller(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "args")
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte("echo \"$@\" > '"+record+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := Installer
	Installer = script
	t.Cleanup(func() { Installer = old })
	path := filepath.Join(dir, "center.json")
	for _, tc := range []struct{ input, want string }{
		{"8\nn\n0\n", ""},
		{"8\ny\n\n", "uninstall"},
		{"8\ny\ny\n", "uninstall --purge"},
	} {
		os.Remove(record)
		var out bytes.Buffer
		if err := RunOperatorMenu(path, strings.NewReader(tc.input), &out); err != nil {
			t.Fatalf("%q: %v", tc.input, err)
		}
		got, _ := os.ReadFile(record)
		if strings.TrimSpace(string(got)) != tc.want {
			t.Fatalf("%q: installer args = %q, want %q\n%s", tc.input, got, tc.want, out.String())
		}
	}
	Installer = filepath.Join(dir, "missing.sh")
	var out bytes.Buffer
	if err := RunOperatorMenu(path, strings.NewReader("8\n0\n"), &out); err != nil || !strings.Contains(out.String(), "找不到安装脚本") {
		t.Fatalf("missing installer: %v\n%s", err, out.String())
	}
}
