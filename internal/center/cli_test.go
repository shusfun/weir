package center

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imzyb/MiGate/internal/store"
	"github.com/imzyb/MiGate/internal/store/ent/user"
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
