package control

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectCLIMode(t *testing.T) {
	original := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = original })
	stdinIsTerminal = func() bool { return true }
	t.Setenv("INVOCATION_ID", "")
	if SelectCLIMode() != "menu" {
		t.Fatal("interactive terminal should open the menu")
	}
	t.Setenv("INVOCATION_ID", "systemd-invocation")
	if SelectCLIMode() != "serve" {
		t.Fatal("systemd invocation entered the menu")
	}
	stdinIsTerminal = func() bool { return false }
	t.Setenv("INVOCATION_ID", "")
	if SelectCLIMode() != "no-terminal" {
		t.Fatal("non-interactive run waited for input")
	}
}

func TestOperatorMenuExitBackAndInvalid(t *testing.T) {
	output := runMenu(t, filepath.Join(t.TempDir(), "config.json"), "9\n7\n0\n0\n", func(m *operatorMenu) {
		m.systemctl = failSystemctl(t)
		m.runSetup = failSetup(t)
	})
	if strings.Count(output, "MiGate 控制菜单") != 3 || !strings.Contains(output, "  1) 查看凭证状态") {
		t.Fatalf("menu navigation:\n%s", output)
	}
	if !strings.Contains(output, "请输入菜单中的数字") {
		t.Fatal("invalid choice was accepted")
	}
}

func TestOperatorMenuSkipsStartWhenActive(t *testing.T) {
	var calls []string
	output := runMenu(t, filepath.Join(t.TempDir(), "config.json"), "2\n\n0\n", func(m *operatorMenu) {
		m.systemctl = func(args ...string) (string, error) {
			calls = append(calls, strings.Join(args, " "))
			if args[0] == "is-active" {
				return "active", nil
			}
			t.Fatalf("start was issued: %v", args)
			return "", nil
		}
	})
	if !strings.Contains(output, "[跳过] 服务已在运行，未再次启动") {
		t.Fatalf("output:\n%s", output)
	}
	if len(calls) != 1 || calls[0] != "is-active migate-control" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestOperatorMenuStopRequiresRootAndConfirm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	denied := runMenu(t, path, "3\n\n0\n", func(m *operatorMenu) {
		m.isRoot = func() bool { return false }
		m.systemctl = failSystemctl(t)
	})
	if !strings.Contains(denied, "请用 root 运行：sudo migate") {
		t.Fatalf("output:\n%s", denied)
	}
	var calls []string
	cancelled := runMenu(t, path, "3\nn\n\n0\n", func(m *operatorMenu) {
		m.isRoot = func() bool { return true }
		m.systemctl = func(args ...string) (string, error) {
			calls = append(calls, strings.Join(args, " "))
			return "", nil
		}
	})
	if !strings.Contains(cancelled, "[跳过] 已取消") || len(calls) != 0 {
		t.Fatalf("calls=%v output:\n%s", calls, cancelled)
	}
	stopped := runMenu(t, path, "3\ny\n\n0\n", func(m *operatorMenu) {
		m.isRoot = func() bool { return true }
		m.systemctl = func(args ...string) (string, error) {
			calls = append(calls, strings.Join(args, " "))
			if args[0] == "is-active" {
				return "inactive", nil
			}
			return "stopped", nil
		}
	})
	if !strings.Contains(stopped, "[成功] migate-control：已停止") || !containsCall(calls, "stop migate-control") {
		t.Fatalf("calls=%v output:\n%s", calls, stopped)
	}
}

func TestOperatorMenuRejectsReinstallAndNewToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := saveConfig(path, Config{Installed: true, Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)}); err != nil {
		t.Fatal(err)
	}
	output := runMenu(t, path, "5\n\n7\n2\n\n0\n0\n", func(m *operatorMenu) {
		m.isRoot = func() bool { return true }
		m.runSetup = failSetup(t)
		m.rotate = func(string, Config) (string, error) {
			t.Fatal("rotated a token after installation")
			return "", nil
		}
	})
	if !strings.Contains(output, "已经完成初始化，不能再次安装") || !strings.Contains(output, "不能再发放初始化凭证") {
		t.Fatalf("output:\n%s", output)
	}
}

func TestOperatorMenuShowsTokenOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	token, err := Initialize(path, Config{Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)})
	if err != nil {
		t.Fatal(err)
	}
	output := runMenu(t, path, "7\n1\n\n2\n\n0\n0\n", func(m *operatorMenu) {
		m.isRoot = func() bool { return true }
		m.systemctl = failSystemctl(t)
	})
	if strings.Count(output, token) != 1 || !strings.Contains(output, "[成功] 已显示当前初始化凭证") {
		t.Fatalf("token count=%d output:\n%s", strings.Count(output, token), output)
	}
	if strings.Contains(output, "当前初始化凭证") && strings.Contains(strings.Split(output, token)[1], token) {
		t.Fatal("token repeated after the success line")
	}
}

func TestOperatorMenuChangesEmailWithoutOldPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := saveConfig(path, Config{Installed: true, Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)}); err != nil {
		t.Fatal(err)
	}
	var gotID int
	var gotEmail string
	output := runMenu(t, path, "6\n2\n2\nnext-admin@example.com\ny\n\n0\n0\n", func(m *operatorMenu) {
		m.isRoot = func() bool { return true }
		m.listAdmins = func(context.Context, Config) ([]AdminAccount, error) {
			return []AdminAccount{{ID: 3, Email: "old@example.com", Enabled: true}, {ID: 8, Email: "other@example.com", Enabled: false}}, nil
		}
		m.changeEmail = func(_ context.Context, _ Config, id int, email string) (AdminChangeResult, error) {
			gotID, gotEmail = id, email
			return AdminChangeResult{AuditWritten: true, SessionsRevoked: true}, nil
		}
		m.changePassword = func(context.Context, Config, int, string) (AdminChangeResult, error) {
			t.Fatal("password change was called")
			return AdminChangeResult{}, nil
		}
	})
	if gotID != 8 || gotEmail != "next-admin@example.com" {
		t.Fatalf("changed id=%d email=%s", gotID, gotEmail)
	}
	if strings.Contains(output, "旧密码") || !strings.Contains(output, "授权和订阅令牌保持不变") {
		t.Fatalf("output:\n%s", output)
	}
}

func TestOperatorMenuPasswordMismatchDoesNotSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := saveConfig(path, Config{Installed: true, Listen: "127.0.0.1:19810", EncryptionKey: strings.Repeat("ab", 32)}); err != nil {
		t.Fatal(err)
	}
	output := runMenu(t, path, "6\n3\none-password-1\nother-password\n\n0\n0\n", func(m *operatorMenu) {
		m.isRoot = func() bool { return true }
		m.listAdmins = func(context.Context, Config) ([]AdminAccount, error) {
			return []AdminAccount{{ID: 3, Email: "old@example.com", Enabled: true}}, nil
		}
		m.readSecret = func(string) (string, error) { return readLine(m.in) }
		m.changePassword = func(context.Context, Config, int, string) (AdminChangeResult, error) {
			t.Fatal("mismatched password was saved")
			return AdminChangeResult{}, nil
		}
	})
	if !strings.Contains(output, "两次密码不一致") {
		t.Fatalf("output:\n%s", output)
	}
}

func TestForceAdminPasswordRejectsShortPassword(t *testing.T) {
	_, err := ForceAdminPassword(context.Background(), Config{}, 1, "short")
	if err == nil || err.Error() != accountPasswordHint {
		t.Fatal(err)
	}
}

func runMenu(t *testing.T, path, script string, prepare func(*operatorMenu)) string {
	t.Helper()
	var output bytes.Buffer
	menu := newOperatorMenu(path, strings.NewReader(script), &output)
	menu.isRoot = func() bool { return false }
	menu.systemctl = failSystemctl(t)
	menu.runSetup = failSetup(t)
	menu.rotate = func(string, Config) (string, error) {
		t.Fatal("unexpected token rotation")
		return "", nil
	}
	menu.repair = func(string) error {
		t.Fatal("unexpected ownership repair")
		return errors.New("repair")
	}
	if prepare != nil {
		prepare(menu)
	}
	if err := menu.mainLoop(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	return output.String()
}

func failSystemctl(t *testing.T) func(...string) (string, error) {
	t.Helper()
	return func(args ...string) (string, error) {
		t.Fatalf("unexpected systemctl %v", args)
		return "", nil
	}
}

func failSetup(t *testing.T) func(string) error {
	t.Helper()
	return func(string) error {
		t.Fatal("setup wizard started")
		return errors.New("setup")
	}
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}
