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

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent/user"
)

// ServiceUnit 是中心的 systemd 单元名。
const ServiceUnit = "weir-center.service"

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

// CurrentSetupToken 返回可用的初始化凭证及剩余有效期：现有凭证没过期就照旧返回（不打断进行中的向导），
// 过期、缺失或与配置里的摘要对不上时轮换一份新的。rotated 表示这次换了新凭证。
func CurrentSetupToken(path string, cfg Config) (token string, left time.Duration, rotated bool, err error) {
	cred, err := readSetupCredential(path)
	if err != nil {
		return "", 0, false, err
	}
	if cred.Installed || cfg.Installed {
		return "", 0, false, errors.New("already installed")
	}
	token = readSetupToken(path)
	if left = time.Until(cred.Expires); token != "" && cred.Hash != "" && digest(token) == cred.Hash && left > 0 {
		return token, left, false, nil
	}
	if token, err = RotateSetupToken(path, cfg); err != nil {
		return "", 0, false, err
	}
	return token, 30 * time.Minute, true, nil
}

// DescribeSetupToken 是凭证上方那行说明：剩余分钟数，换新时注明原凭证已过期。
func DescribeSetupToken(left time.Duration, rotated bool) string {
	minutes := int((left + time.Minute - 1) / time.Minute)
	if rotated {
		return fmt.Sprintf("原凭证已过期，已换新（%d 分钟有效）：", minutes)
	}
	return fmt.Sprintf("初始化凭证（还有 %d 分钟有效）：", minutes)
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
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		found := detectStore(ctx)
		cancel()
		pgHost, pgPort, pgDB, pgUser, pgSecret := "127.0.0.1", "5432", "gate", "", "数据库密码"
		if d := found.Postgres; d != nil {
			pgHost, pgPort = d.Host, strconv.Itoa(d.Port)
			if d.User != "" {
				pgUser = d.User
			}
			if d.Recommended != "" {
				pgDB = d.Recommended
			}
			if d.HasPassword {
				pgSecret = "数据库密码（留空使用 1Panel 里的密码）"
			}
			fmt.Fprintln(out, describePostgres(d))
		}
		redisAddr, redisSecret := "127.0.0.1:6379", "Redis 密码（无则留空）"
		if d := found.Redis; d != nil {
			redisAddr = d.Address
			if d.HasPassword {
				redisSecret = "Redis 密码（留空使用 1Panel 里的密码）"
			}
			fmt.Fprintln(out, describeRedis(d))
		}
		pg := store.PostgresConfig{SSLMode: "disable"}
		var port string
		for _, f := range []struct {
			label string
			dst   *string
			def   string
		}{{"PostgreSQL 主机", &pg.Host, pgHost}, {"PostgreSQL 端口", &port, pgPort}, {"数据库名", &pg.Database, pgDB}, {"数据库用户", &pg.User, pgUser}} {
			if *f.dst, err = p.ask(f.label, f.def); err != nil {
				return err
			}
		}
		if pg.Port, err = strconv.Atoi(port); err != nil || pg.Port < 1 || pg.Port > 65535 {
			return errors.New("PostgreSQL 端口无效")
		}
		if pg.Password, err = readSecret(pgSecret); err != nil {
			return err
		}
		rc := store.RedisConfig{}
		if rc.Address, err = p.ask("Redis 地址", redisAddr); err != nil {
			return err
		}
		if rc.Username, err = p.ask("Redis ACL 用户（只设了密码的 Redis 留空）", ""); err != nil {
			return err
		}
		if rc.Password, err = readSecret(redisSecret); err != nil {
			return err
		}
		cfg.Store = fillDetectedSecrets(store.Config{Driver: store.DriverPostgres, Postgres: pg, Redis: rc})
	} else {
		path, err := p.ask("SQLite 文件", DefaultSQLitePath())
		if err != nil {
			return err
		}
		cfg.Store = store.Config{Driver: store.DriverSQLite, SQLitePath: path}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = checkInstallTarget(ctx, cfg.Store); errors.Is(err, store.ErrExistingData) {
		return errors.New("这个存储里已有 Weir 账户：要继续用这些数据请恢复原配置文件，否则换一个空库")
	} else if err != nil {
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

// RunOperatorMenu 是 root 在终端运行 weir 时的中文菜单。
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
		fmt.Fprintf(out, "\nWeir 中心 %s · %s · 监听 %s\n", buildinfo.Version, state, cfg.Listen)
		fmt.Fprintln(out, "  1) 服务状态   2) 启动   3) 停止   4) 重启")
		if cfg.Installed {
			fmt.Fprintln(out, "  5) 重设管理员密码")
		} else {
			fmt.Fprintln(out, "  5) 终端初始化   6) 显示初始化凭证   7) 刷新初始化凭证")
		}
		fmt.Fprintln(out, "  8) 卸载   0) 退出")
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
			if token, left, rotated, err := CurrentSetupToken(path, cfg); err != nil {
				report(out, err, "")
			} else {
				PrintSetupToken(out, DescribeSetupToken(left, rotated), token)
			}
		case choice == "7" && !cfg.Installed:
			if token, err := RotateSetupToken(path, cfg); err != nil {
				report(out, err, "")
			} else {
				PrintSetupToken(out, "新凭证（30 分钟有效）：", token)
			}
		case choice == "8":
			if done, err := p.uninstall(); err != nil {
				report(out, err, "")
			} else if done {
				return nil
			}
		default:
			fmt.Fprintln(out, "无效选项。")
		}
	}
}

func describePostgres(d *detectedPostgres) string {
	from := "本机端口"
	if d.Source == "1panel" {
		from = "1Panel"
	}
	line := fmt.Sprintf("检测到 %s 的 PostgreSQL %s：%s:%d", from, d.Version, d.Host, d.Port)
	if !d.Connected {
		return line + "（未能登录，请手动填写账号和密码）"
	}
	var used []string
	for _, db := range d.Databases {
		if db.HasData {
			used = append(used, db.Name)
		}
	}
	if len(used) > 0 {
		line += "；已有 Weir 账户的库：" + strings.Join(used, "、")
	}
	return line
}

func describeRedis(d *detectedRedis) string {
	from := "本机端口"
	if d.Source == "1panel" {
		from = "1Panel"
	}
	line := fmt.Sprintf("检测到 %s 的 Redis %s：%s", from, d.Version, d.Address)
	switch d.Status {
	case store.RedisAuthRequired:
		line += "（需要密码）"
	case store.RedisWrongPassword:
		line += "（1Panel 里的密码登录失败，请手动填写）"
	}
	return line
}

// PrintSetupToken 让凭证独占一行：网页终端对中文行的列宽计算与显示不一致，同行有中文时鼠标复制会错位丢字。
func PrintSetupToken(out io.Writer, label, token string) {
	fmt.Fprintln(out, label)
	fmt.Fprintln(out, token)
}

// Installer 是每次安装都会复制到本机的安装脚本；卸载只交给它做，菜单不另写一套。
var Installer = "/usr/local/lib/weir/install.sh"

// uninstall 确认后调用安装脚本卸载；默认保留配置与数据。返回 done=true 表示已卸载，菜单随即退出。
func (p prompter) uninstall() (bool, error) {
	if _, err := os.Stat(Installer); err != nil {
		return false, fmt.Errorf("找不到安装脚本 %s，请用在线脚本卸载", Installer)
	}
	fmt.Fprintln(p.out, "将停止并移除 Weir 的程序和服务。PostgreSQL 的 weir schema 与 Redis 的 weir: 键不会删除。")
	if answer, err := p.ask("确认卸载？ [y/N]", "N"); err != nil || !strings.EqualFold(answer, "y") {
		return false, err
	}
	args := []string{Installer, "uninstall"}
	if answer, err := p.ask("同时删除配置和数据（/etc/weir、/var/lib/weir）？ [y/N]", "N"); err != nil {
		return false, err
	} else if strings.EqualFold(answer, "y") {
		args = append(args, "--purge")
	}
	cmd := exec.Command("sh", args...)
	cmd.Stdout, cmd.Stderr = p.out, p.out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("卸载失败：%w", err)
	}
	return true, nil
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
