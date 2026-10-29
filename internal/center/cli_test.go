package center

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	db := filepath.Join(dir, "weir.db")
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
	input := strings.Join([]string{"", "1", filepath.Join(dir, "weir.db"), "ops@example.test", "n"}, "\n") + "\n"
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

// 菜单 6 / --token：没过期的凭证照旧返回（不打断进行中的向导），过期或丢失时换新，旧的失效。
func TestCurrentSetupTokenRotatesOnlyWhenExpired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "center.json")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	// 不能用默认端口：本机可能正跑着开发中心，轮换时会去确认它是否接受新凭证。
	cfg.Listen = "127.0.0.1:1"
	first, err := Initialize(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, err = LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	got, left, rotated, err := CurrentSetupToken(path, cfg)
	if err != nil || got != first || rotated || left <= 29*time.Minute {
		t.Fatalf("fresh token: %q %v %v %v", got, left, rotated, err)
	}

	cfg.SetupExpires = time.Now().Add(-time.Minute)
	if err = saveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	second, left, rotated, err := CurrentSetupToken(path, cfg)
	if err != nil || second == first || !rotated || left != 30*time.Minute {
		t.Fatalf("expired token: %q %v %v %v", second, left, rotated, err)
	}
	if cred, _ := readSetupCredential(path); cred.Hash != digest(second) || !cred.Expires.After(time.Now().Add(29*time.Minute)) {
		t.Fatalf("rotated credential not saved: %+v", cred)
	}

	os.Remove(setupTokenPath(path))
	third, _, rotated, err := CurrentSetupToken(path, cfg)
	if err != nil || third == second || !rotated {
		t.Fatalf("missing token file: %q %v %v", third, rotated, err)
	}

	var out bytes.Buffer
	if err = RunOperatorMenu(path, strings.NewReader("6\n0\n"), &out); err != nil || !strings.Contains(out.String(), "还有 30 分钟有效") || !strings.Contains(out.String(), "\n"+third+"\n") {
		t.Fatalf("menu 6: %v\n%s", err, out.String())
	}

	cfg.Installed = true
	if _, _, _, err = CurrentSetupToken(path, cfg); err == nil {
		t.Fatal("installed center must not issue a token")
	}
}
