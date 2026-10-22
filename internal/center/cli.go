package center

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	"github.com/imzyb/MiGate/internal/buildinfo"
	"github.com/imzyb/MiGate/internal/store"
	"github.com/imzyb/MiGate/internal/store/ent/user"
)

// ServiceUnit 是中心的 systemd 单元名。
const ServiceUnit = "migate-center.service"

// SelectCLIMode 决定无参数运行时做什么：systemd 下（INVOCATION_ID）必须后台服务。
func SelectCLIMode() string {
	if os.Getenv("INVOCATION_ID") != "" {
		return "serve"
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return "menu"
	}
	return "no-terminal"
}

func ReadSecret(prompt string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		value, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return string(value), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func probeAddress(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}

func postSetupVerify(listen, token string) (int, error) {
	address, err := probeAddress(listen)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+address+"/api/v1/setup/verify", strings.NewReader("{}"))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}

// RotateSetupToken 写入新凭证；服务正在运行时确认新凭证可用、旧凭证已失效。
func RotateSetupToken(path string, cfg Config) (string, error) {
	previous := readSetupToken(path)
	token, err := Initialize(path, cfg)
	if err != nil {
		return "", err
	}
	address, _ := probeAddress(cfg.Listen)
	if conn, dialErr := net.DialTimeout("tcp", address, 300*time.Millisecond); dialErr == nil {
		conn.Close()
		if status, err := postSetupVerify(cfg.Listen, token); err != nil || status != http.StatusOK {
			return "", errors.New("running service did not accept the new initialization token")
		}
		if previous != "" && subtle.ConstantTimeCompare([]byte(previous), []byte(token)) != 1 {
			if status, err := postSetupVerify(cfg.Listen, previous); err != nil || status != http.StatusUnauthorized {
				return "", errors.New("running service still accepts the previous initialization token")
			}
		}
	}
	return token, nil
}

type prompter struct {
	in         *bufio.Reader
	out        io.Writer
	readSecret func(string) (string, error)
}

func (p prompter) ask(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(p.out, "%s: ", label)
	}
	line, err := p.in.ReadString('\n')
	line = strings.TrimSpace(line)
	if err != nil && line == "" {
		return "", err
	}
	if line == "" {
		return def, nil
	}
	return line, nil
}

func (p prompter) newPassword() (string, error) {
	for {
		first, err := p.readSecret("管理员密码（6–72 位，含大写、小写字母和数字）")
		if err != nil {
			return "", err
		}
		if !validAccountPassword(first) {
			fmt.Fprintln(p.out, "密码不符合要求，请重新输入。")
			continue
		}
		second, err := p.readSecret("再次输入密码")
		if err != nil {
			return "", err
		}
		if first == second {
			return first, nil
		}
		fmt.Fprintln(p.out, "两次输入不一致，请重新输入。")
	}
}

// RunSetupCLI 是终端初始化向导：选择存储、创建管理员。与 Web 向导共用安装锁，不能同时提交。
func RunSetupCLI(path string, in io.Reader, out io.Writer, readSecret func(string) (string, error)) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}
	if cred, err := readSetupCredential(path); err != nil || cred.Installed || cfg.Installed {
		return errors.New("已经初始化，不能再次执行")
	}
	p := prompter{in: bufio.NewReader(in), out: out, readSecret: readSecret}
	if cfg.Listen, err = p.ask("监听地址", cfg.Listen); err != nil {
		return err
	}
	if _, _, err = net.SplitHostPort(cfg.Listen); err != nil {
		return fmt.Errorf("监听地址无效：%w", err)
	}
	choice, err := p.ask("存储：1) 只用 SQLite（推荐单机）  2) PostgreSQL + Redis", "1")
	if err != nil {
		return err
	}
	if choice == "2" {
		pg := store.PostgresConfig{SSLMode: "disable"}
		var port string
		for _, f := range []struct {
			label string
			dst   *string
			def   string
		}{{"PostgreSQL 主机", &pg.Host, "127.0.0.1"}, {"PostgreSQL 端口", &port, "5432"}, {"数据库名", &pg.Database, "gate"}, {"数据库用户", &pg.User, ""}} {
			if *f.dst, err = p.ask(f.label, f.def); err != nil {
				return err
			}
		}
		if pg.Port, err = strconv.Atoi(port); err != nil || pg.Port < 1 || pg.Port > 65535 {
			return errors.New("PostgreSQL 端口无效")
		}
		if pg.Password, err = readSecret("数据库密码"); err != nil {
			return err
		}
		rc := store.RedisConfig{}
		if rc.Address, err = p.ask("Redis 地址", "127.0.0.1:6379"); err != nil {
			return err
		}
		if rc.Username, err = p.ask("Redis 用户（无则留空）", ""); err != nil {
			return err
		}
		if rc.Password, err = readSecret("Redis 密码（无则留空）"); err != nil {
			return err
		}
		cfg.Store = store.Config{Driver: store.DriverPostgres, Postgres: pg, Redis: rc}
	} else {
		path, err := p.ask("SQLite 文件", DefaultSQLitePath())
		if err != nil {
			return err
		}
		cfg.Store = store.Config{Driver: store.DriverSQLite, SQLitePath: path}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = store.Check(ctx, cfg.Store); err != nil {
		return fmt.Errorf("存储连接失败：%w", err)
	}
	email, err := p.ask("管理员邮箱", "")
	if err != nil {
		return err
	}
	if !validEmail(email) {
		return errors.New("邮箱格式无效")
	}
	password, err := p.newPassword()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "存储：%s；监听：%s；管理员：%s\n", cfg.Store.Driver, cfg.Listen, strings.ToLower(email))
	if answer, err := p.ask("确认安装？ [y/N]", "N"); err != nil || !strings.EqualFold(answer, "y") {
		return errors.New("已取消")
	}
	st, _, err := completeInstall(ctx, path, cfg, email, password)
	if err != nil {
		return fmt.Errorf("安装失败：%w", err)
	}
	st.Close()
	fmt.Fprintln(out, "初始化完成。运行中的服务会自动切换；未运行时执行：systemctl enable --now "+ServiceUnit)
	return nil
}

// ResetAdminPassword 供运维菜单强制重设管理员密码（不需要旧密码），并撤销其全部会话。
func ResetAdminPassword(ctx context.Context, cfg Config, email, password string) error {
	if !validAccountPassword(password) {
		return errors.New("密码不符合要求")
	}
	st, err := store.Open(ctx, cfg.Store)
	if err != nil {
		return err
	}
	defer st.Close()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u, err := st.Ent.User.Query().Where(user.EmailEQ(strings.ToLower(strings.TrimSpace(email))), user.RoleEQ(user.RoleAdmin)).Only(ctx)
	if err != nil {
		return errors.New("找不到这个管理员")
	}
	if _, err = st.Ent.User.UpdateOne(u).SetPasswordHash(string(hash)).Save(ctx); err != nil {
		return err
	}
	keys, err := st.KV.Keys(ctx, sessionPrefix(u.ID), 5000)
	if err == nil {
		err = st.KV.Del(ctx, keys...)
	}
	return err
}

// RunOperatorMenu 是 root 在终端运行 migate 时的中文菜单。
func RunOperatorMenu(path string, in io.Reader, out io.Writer) error {
	p := prompter{in: bufio.NewReader(in), out: out, readSecret: ReadSecret}
	for {
		cfg, err := LoadConfig(path)
		if err != nil {
			return err
		}
		state := "未初始化"
		if cfg.Installed {
			state = "已初始化（" + cfg.Store.Driver + "）"
		}
		fmt.Fprintf(out, "\nMiGate 中心 %s · %s · 监听 %s\n", buildinfo.Version, state, cfg.Listen)
		fmt.Fprintln(out, "  1) 服务状态   2) 启动   3) 停止   4) 重启")
		if cfg.Installed {
			fmt.Fprintln(out, "  5) 重设管理员密码")
		} else {
			fmt.Fprintln(out, "  5) 终端初始化   6) 显示初始化凭证   7) 刷新初始化凭证")
		}
		fmt.Fprintln(out, "  0) 退出")
		choice, err := p.ask("请选择", "")
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case choice == "0":
			return nil
		case choice == "1":
			systemctl(out, "status", "--no-pager", ServiceUnit)
		case choice == "2", choice == "3", choice == "4":
			action := map[string]string{"2": "start", "3": "stop", "4": "restart"}[choice]
			if action != "start" {
				if answer, _ := p.ask("确认"+action+"？ [y/N]", "N"); !strings.EqualFold(answer, "y") {
					continue
				}
			}
			systemctl(out, action, ServiceUnit)
		case choice == "5" && cfg.Installed:
			email, _ := p.ask("管理员邮箱", "")
			password, err := p.newPassword()
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				err = ResetAdminPassword(ctx, cfg, email, password)
				cancel()
			}
			report(out, err, "密码已重设，原有登录已全部撤销。")
		case choice == "5":
			report(out, RunSetupCLI(path, p.in, out, ReadSecret), "")
		case choice == "6" && !cfg.Installed:
			if token := readSetupToken(path); token != "" {
				fmt.Fprintln(out, "初始化凭证（30 分钟有效）：", token)
			} else {
				fmt.Fprintln(out, "没有有效凭证，请选择 7 刷新。")
			}
		case choice == "7" && !cfg.Installed:
			token, err := RotateSetupToken(path, cfg)
			report(out, err, "新凭证（30 分钟有效）："+token)
		default:
			fmt.Fprintln(out, "无效选项。")
		}
	}
}

func systemctl(out io.Writer, args ...string) {
	b, err := exec.Command("systemctl", args...).CombinedOutput()
	fmt.Fprint(out, string(b))
	if err != nil {
		fmt.Fprintln(out, "systemctl:", err)
	}
}

func report(out io.Writer, err error, ok string) {
	if err != nil {
		fmt.Fprintln(out, "失败：", err)
	} else if ok != "" {
		fmt.Fprintln(out, ok)
	}
}
