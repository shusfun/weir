package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

var (
	errAlreadyInstalled      = errors.New("already installed")
	errInstallationCancelled = errors.New("installation cancelled")
	errInputClosed           = errors.New("input closed")
)

type SetupAnswers struct {
	Database DatabaseConfig
	Redis    RedisConfig
	Email    string
	Password string
	Listen   string
	Confirm  bool
}

type setupDefaults struct {
	Database   DatabaseConfig
	RedisHost  string
	RedisPort  int
	RedisUser  string
	RedisTLS   bool
	ListenHost string
	ListenPort int
	HasFile    bool
}

var (
	testDatabaseFn = TestDatabase
	testRedisFn    = TestRedis
	applySetupFn   = ApplySetup
	listenProbe    = portListening
	waitReady      = func(listen string) error { return waitLocalService(listen, 20*time.Second) }
	restartService = trySystemctlRestart
)

func ValidateSetupAccount(email, password string) error {
	if !validEmail(email) || !validAccountPassword(password) {
		return errors.New("需要有效邮箱，且" + accountPasswordHint)
	}
	return nil
}

func TestDatabase(cfg DatabaseConfig) error {
	if cfg.Port < 1 || cfg.Port > 65535 || !validSetupHost(cfg.Host) || !validSetupName(cfg.User) || !validSetupName(cfg.Database) || !validSSLMode(cfg.SSLMode) {
		return errors.New("database connection failed")
	}
	db, _, err := openDatabase(cfg)
	if err != nil {
		return errors.New("database connection failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return errors.New("database connection failed")
	}
	return nil
}

func TestRedis(cfg RedisConfig) error {
	host, port, err := net.SplitHostPort(cfg.Address)
	if err != nil || !validSetupHost(host) {
		return errors.New("redis connection failed")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return errors.New("redis connection failed")
	}
	redis := redisClient(cfg)
	defer redis.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if redis.Ping(ctx).Err() != nil {
		return errors.New("redis connection failed")
	}
	return nil
}

func ApplySetup(path string, answers SetupAnswers) error {
	if err := ValidateSetupAccount(answers.Email, answers.Password); err != nil {
		return err
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}
	if cfg.Installed {
		return errAlreadyInstalled
	}
	if answers.Listen != "" {
		if _, _, err = net.SplitHostPort(answers.Listen); err != nil {
			return errors.New("invalid listen address")
		}
		cfg.Listen = answers.Listen
	}
	if err = TestDatabase(answers.Database); err != nil {
		return err
	}
	if err = TestRedis(answers.Redis); err != nil {
		return err
	}
	if !answers.Confirm {
		return errInstallationCancelled
	}
	cfg.Database = answers.Database
	cfg.Redis = answers.Redis
	if cfg.EncryptionKey == "" {
		cfg.EncryptionKey = randomToken(32)
	}
	server := &Server{Config: cfg, Path: path}
	if err = server.connect(cfg); err != nil {
		return err
	}
	defer server.Close()
	installCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = server.finishInstall(installCtx, answers.Email, answers.Password)
	if err == nil {
		return nil
	}
	if err.Error() == errAlreadyInstalled.Error() || err.Error() == "existing_installation_restore_config" {
		loaded, loadErr := LoadConfig(path)
		if loadErr == nil && loaded.Installed {
			return nil
		}
	}
	return err
}

func RunSetupCLI(path string, in io.Reader, password func(string) (string, error)) error {
	cfg, err := LoadConfig(path)
	if err == nil && cfg.Installed {
		return errAlreadyInstalled
	}
	defaults, err := loadSetupDefaults(path)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(in)
	fmt.Fprintln(os.Stderr, "MiGate 终端初始化")
	fmt.Fprintf(os.Stderr, "配置：%s\n状态：未安装\n", path)
	currentListen := net.JoinHostPort(defaults.ListenHost, strconv.Itoa(defaults.ListenPort))
	if listenProbe(currentListen) {
		fmt.Fprintf(os.Stderr, "监听 %s：已有服务\n", currentListen)
	} else {
		fmt.Fprintf(os.Stderr, "监听 %s：当前没有服务\n", currentListen)
	}
	if defaults.HasFile {
		fmt.Fprintln(os.Stderr, "已有配置会作为默认值，直接回车保持不变。")
	}
	fmt.Fprintln(os.Stderr, "业务数据使用 PostgreSQL schema migate，不会新建数据库实例。")
	answers, err := collectSetupAnswers(reader, password, defaults)
	if err != nil {
		return err
	}
	previous := currentListen
	wasRunning := listenProbe(previous)
	if err = applySetupFn(path, answers); err != nil {
		return err
	}
	return reportSetupResult(previous, answers.Listen, wasRunning)
}

func collectSetupAnswers(reader *bufio.Reader, password func(string) (string, error), defaults setupDefaults) (SetupAnswers, error) {
	var answers SetupAnswers
	for {
		fmt.Fprintln(os.Stderr, "\n── PostgreSQL ──")
		var err error
		if answers.Database, err = promptDatabase(reader, password, defaults); err != nil {
			return answers, err
		}
		fmt.Fprint(os.Stderr, "正在测试 PostgreSQL 连接… ")
		if err = testDatabaseFn(answers.Database); err != nil {
			fmt.Fprintln(os.Stderr, "失败")
			if err = confirmRetry(reader); err != nil {
				return answers, err
			}
			continue
		}
		fmt.Fprintln(os.Stderr, "成功")
		break
	}
	for {
		fmt.Fprintln(os.Stderr, "\n── Redis ──")
		var err error
		if answers.Redis, err = promptRedis(reader, password, defaults); err != nil {
			return answers, err
		}
		fmt.Fprint(os.Stderr, "正在测试 Redis 连接… ")
		if err = testRedisFn(answers.Redis); err != nil {
			fmt.Fprintln(os.Stderr, "失败")
			if err = confirmRetry(reader); err != nil {
				return answers, err
			}
			continue
		}
		fmt.Fprintln(os.Stderr, "成功")
		break
	}
	for {
		fmt.Fprintln(os.Stderr, "\n── 管理员 ──")
		email, err := promptLine(reader, "管理员邮箱", "")
		if err != nil {
			return answers, err
		}
		secret, err := readPassword(password, "管理员密码")
		if err != nil {
			return answers, err
		}
		confirm, err := readPassword(password, "确认密码")
		if err != nil {
			return answers, err
		}
		if secret != confirm {
			fmt.Fprintln(os.Stderr, "两次输入的密码不一致")
			continue
		}
		if err = ValidateSetupAccount(email, secret); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			continue
		}
		answers.Email = email
		answers.Password = secret
		break
	}
	fmt.Fprintln(os.Stderr, "\n── 监听地址 ──")
	host, port, err := promptListen(reader, defaults)
	if err != nil {
		return answers, err
	}
	answers.Listen = net.JoinHostPort(host, strconv.Itoa(port))
	if host == "0.0.0.0" || host == "::" {
		fmt.Fprintln(os.Stderr, "警告：该监听地址提供明文 HTTP。建议保持 127.0.0.1，由本机反代提供 HTTPS。")
	}
	fmt.Fprintln(os.Stderr, "\n── 确认 ──")
	fmt.Fprintf(os.Stderr, "PostgreSQL：%s@%s:%d/%s ssl=%s schema=migate\n", answers.Database.User, answers.Database.Host, answers.Database.Port, answers.Database.Database, answers.Database.SSLMode)
	fmt.Fprintf(os.Stderr, "Redis：%s@%s tls=%s\n", displayUser(answers.Redis.Username), answers.Redis.Address, enabledText(answers.Redis.TLS))
	fmt.Fprintf(os.Stderr, "管理员：%s\n监听：%s\n", answers.Email, answers.Listen)
	ok, err := promptYes(reader, "确认安装？", false)
	if err != nil {
		return answers, err
	}
	if !ok {
		return answers, errInstallationCancelled
	}
	answers.Confirm = true
	return answers, nil
}

func reportSetupResult(previous, next string, wasUp bool) error {
	if !wasUp {
		fmt.Fprintln(os.Stderr, "\n配置和管理员已写入。")
		fmt.Fprintln(os.Stderr, "启动服务：systemctl enable --now migate-control")
		fmt.Fprintf(os.Stderr, "启动后打开 %s 登录。前面有反代时，打开反代网址。不要再次执行 --setup。\n", next)
		return nil
	}
	if previous != next {
		fmt.Fprintln(os.Stderr, "监听地址已变更，正在重启服务…")
		if err := restartService(); err != nil {
			return errors.New("installed but the new listen address is not active; run: systemctl restart migate-control")
		}
	}
	fmt.Fprintln(os.Stderr, "正在等待服务就绪…")
	if err := waitReady(next); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "\n安装已完成，服务已就绪。")
	fmt.Fprintf(os.Stderr, "监听：%s\n", next)
	fmt.Fprintln(os.Stderr, "若前面有反代，打开反代网址并用管理员邮箱登录。初始化入口已锁定。")
	return nil
}

func promptDatabase(reader *bufio.Reader, password func(string) (string, error), defaults setupDefaults) (DatabaseConfig, error) {
	var cfg DatabaseConfig
	var err error
	if cfg.Host, err = promptValidated(reader, "PostgreSQL 主机", defaults.Database.Host, validSetupHost, "主机格式无效"); err != nil {
		return cfg, err
	}
	if cfg.Port, err = promptPort(reader, "PostgreSQL 端口", defaults.Database.Port); err != nil {
		return cfg, err
	}
	if cfg.User, err = promptValidated(reader, "PostgreSQL 用户", defaults.Database.User, validSetupName, "用户名格式无效"); err != nil {
		return cfg, err
	}
	secret, err := readPassword(password, "PostgreSQL 密码")
	if err != nil {
		return cfg, err
	}
	cfg.Password = secret
	if cfg.Database, err = promptValidated(reader, "数据库名", defaults.Database.Database, validSetupName, "数据库名格式无效"); err != nil {
		return cfg, err
	}
	if cfg.SSLMode, err = promptValidated(reader, "SSL 模式", defaults.Database.SSLMode, validSSLMode, "SSL 模式只能是 disable、require、verify-ca 或 verify-full"); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func promptRedis(reader *bufio.Reader, password func(string) (string, error), defaults setupDefaults) (RedisConfig, error) {
	var cfg RedisConfig
	host, err := promptValidated(reader, "Redis 主机", defaults.RedisHost, validSetupHost, "主机格式无效")
	if err != nil {
		return cfg, err
	}
	port, err := promptPort(reader, "Redis 端口", defaults.RedisPort)
	if err != nil {
		return cfg, err
	}
	cfg.Address = net.JoinHostPort(host, strconv.Itoa(port))
	for {
		user, err := promptLine(reader, "Redis ACL 用户名", defaults.RedisUser)
		if err != nil {
			return cfg, err
		}
		if len(user) <= 128 {
			cfg.Username = user
			break
		}
		fmt.Fprintln(os.Stderr, "ACL 用户名过长")
	}
	secret, err := readPassword(password, "Redis 密码")
	if err != nil {
		return cfg, err
	}
	cfg.Password = secret
	tls, err := promptYes(reader, "启用 Redis TLS？", defaults.RedisTLS)
	if err != nil {
		return cfg, err
	}
	cfg.TLS = tls
	return cfg, nil
}

func promptListen(reader *bufio.Reader, defaults setupDefaults) (string, int, error) {
	host, err := promptValidated(reader, "监听主机", defaults.ListenHost, validSetupHost, "监听主机格式无效")
	if err != nil {
		return "", 0, err
	}
	port, err := promptPort(reader, "监听端口", defaults.ListenPort)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func confirmRetry(reader *bufio.Reader) error {
	ok, err := promptYes(reader, "修正后重试？", true)
	if err != nil {
		return err
	}
	if !ok {
		return errInstallationCancelled
	}
	return nil
}

func loadSetupDefaults(path string) (setupDefaults, error) {
	defaults := setupDefaults{
		Database:   DatabaseConfig{Host: "127.0.0.1", Port: 5432, User: "gate", Database: "gate", SSLMode: "disable"},
		RedisHost:  "127.0.0.1",
		RedisPort:  6379,
		RedisUser:  "migate",
		ListenHost: "127.0.0.1",
		ListenPort: 19810,
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return defaults, err
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(b, &root) != nil {
		return defaults, errors.New("invalid configuration")
	}
	defaults.HasFile = true
	if raw, ok := root["database"]; ok {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) == nil {
			applyString(fields, "host", &defaults.Database.Host)
			applyInt(fields, "port", &defaults.Database.Port)
			applyString(fields, "user", &defaults.Database.User)
			applyString(fields, "database", &defaults.Database.Database)
			applyString(fields, "ssl_mode", &defaults.Database.SSLMode)
		}
	}
	if raw, ok := root["redis"]; ok {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) == nil {
			if value, ok := fields["address"]; ok {
				var address string
				if json.Unmarshal(value, &address) == nil {
					if host, port, splitErr := net.SplitHostPort(address); splitErr == nil {
						if number, convErr := strconv.Atoi(port); convErr == nil {
							defaults.RedisHost = host
							defaults.RedisPort = number
						}
					}
				}
			}
			applyString(fields, "username", &defaults.RedisUser)
			applyBool(fields, "tls", &defaults.RedisTLS)
		}
	}
	if raw, ok := root["listen"]; ok {
		var listen string
		if json.Unmarshal(raw, &listen) == nil {
			if host, port, splitErr := net.SplitHostPort(listen); splitErr == nil {
				if number, convErr := strconv.Atoi(port); convErr == nil {
					defaults.ListenHost = host
					defaults.ListenPort = number
				}
			}
		}
	}
	return defaults, nil
}

func applyString(fields map[string]json.RawMessage, key string, dest *string) {
	value, ok := fields[key]
	if !ok {
		return
	}
	_ = json.Unmarshal(value, dest)
}

func applyInt(fields map[string]json.RawMessage, key string, dest *int) {
	value, ok := fields[key]
	if !ok {
		return
	}
	_ = json.Unmarshal(value, dest)
}

func applyBool(fields map[string]json.RawMessage, key string, dest *bool) {
	value, ok := fields[key]
	if !ok {
		return
	}
	_ = json.Unmarshal(value, dest)
}

func promptValidated(reader *bufio.Reader, label, fallback string, valid func(string) bool, problem string) (string, error) {
	for {
		value, err := promptLine(reader, label, fallback)
		if err != nil {
			return "", err
		}
		if valid(value) {
			return value, nil
		}
		fmt.Fprintln(os.Stderr, problem)
	}
}

func promptLine(reader *bufio.Reader, label, fallback string) (string, error) {
	if fallback != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, fallback)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", label)
	}
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", errInputClosed
	}
	line = strings.TrimSpace(strings.TrimRight(line, "\r"))
	if line == "" {
		return fallback, nil
	}
	return line, nil
}

func promptPort(reader *bufio.Reader, label string, fallback int) (int, error) {
	for {
		text, err := promptLine(reader, label, strconv.Itoa(fallback))
		if err != nil {
			return 0, err
		}
		port, convErr := strconv.Atoi(text)
		if convErr == nil && port >= 1 && port <= 65535 {
			return port, nil
		}
		fmt.Fprintln(os.Stderr, "端口必须是 1–65535")
	}
}

func promptYes(reader *bufio.Reader, label string, fallback bool) (bool, error) {
	hint := "y/N"
	if fallback {
		hint = "Y/n"
	}
	fmt.Fprintf(os.Stderr, "%s [%s]: ", label, hint)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return false, errInputClosed
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return fallback, nil
	}
	if line == "y" || line == "yes" {
		return true, nil
	}
	if line == "n" || line == "no" {
		return false, nil
	}
	fmt.Fprintln(os.Stderr, "请输入 y 或 n")
	return promptYes(reader, label, fallback)
}

func readPassword(password func(string) (string, error), label string) (string, error) {
	secret, err := password(label)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", errInputClosed
		}
		return "", err
	}
	return secret, nil
}

func ReadSecret(prompt string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		value, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(value), nil
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func validSetupHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, ch := range host {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case strings.ContainsRune(".-:[]", ch):
		default:
			return false
		}
	}
	return true
}

func validSetupName(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	for i, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z':
		case i > 0 && (ch >= '0' && ch <= '9' || ch == '_'):
		default:
			return false
		}
	}
	return true
}

func validSSLMode(mode string) bool {
	switch mode {
	case "disable", "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}

func displayUser(user string) string {
	if user == "" {
		return "(默认用户)"
	}
	return user
}

func enabledText(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

func trySystemctlRestart() error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("systemctl unavailable")
	}
	if exec.Command("systemctl", "is-active", "--quiet", "migate-control.service").Run() != nil {
		return errors.New("service inactive")
	}
	return exec.Command("systemctl", "restart", "migate-control.service").Run()
}
