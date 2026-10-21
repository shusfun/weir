package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imzyb/MiGate/internal/buildinfo"
	"golang.org/x/term"
)

// SelectCLIMode 决定无参数时做什么。systemd 设置 INVOCATION_ID，这时必须后台服务，不能读菜单。
func SelectCLIMode() string {
	if os.Getenv("INVOCATION_ID") != "" {
		return "serve"
	}
	if stdinIsTerminal() {
		return "menu"
	}
	return "no-terminal"
}

var stdinIsTerminal = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

type operatorMenu struct {
	path           string
	in             io.Reader
	out            io.Writer
	isRoot         func() bool
	systemctl      func(args ...string) (string, error)
	runSetup       func(path string) error
	readSecret     func(prompt string) (string, error)
	rotate         func(path string, cfg Config) (string, error)
	repair         func(path string) error
	listAdmins     func(ctx context.Context, cfg Config) ([]AdminAccount, error)
	changeEmail    func(ctx context.Context, cfg Config, id int, email string) (AdminChangeResult, error)
	changePassword func(ctx context.Context, cfg Config, id int, password string) (AdminChangeResult, error)
}

func RunOperatorMenu(path string, in io.Reader, out io.Writer) error {
	err := newOperatorMenu(path, in, out).mainLoop()
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func newOperatorMenu(path string, in io.Reader, out io.Writer) *operatorMenu {
	return &operatorMenu{
		path:           path,
		in:             in,
		out:            out,
		isRoot:         defaultIsRoot,
		systemctl:      defaultSystemctl,
		runSetup:       func(p string) error { return RunSetupCLI(p, in, ReadSecret) },
		readSecret:     ReadSecret,
		rotate:         RotateSetupToken,
		repair:         repairConfigOwnership,
		listAdmins:     ListAdmins,
		changeEmail:    ForceAdminEmail,
		changePassword: ForceAdminPassword,
	}
}

func defaultSystemctl(args ...string) (string, error) {
	command := exec.Command("systemctl", args...)
	out, err := command.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (m *operatorMenu) mainLoop() error {
	for {
		printTitle(m.out, "MiGate 控制菜单")
		fmt.Fprint(m.out, `  1) 查看服务状态
  2) 启动服务
  3) 停止服务
  4) 重启服务
  5) 初始化
  6) 管理员账号
  7) 初始化凭证
  8) 查看版本
  0) 退出
请输入数字：`)
		choice, err := readLine(m.in)
		if err != nil {
			return err
		}
		switch choice {
		case "0":
			return nil
		case "1":
			err = m.finish(m.serviceStatus())
		case "2":
			err = m.finish(m.startService())
		case "3":
			err = m.finishPair(m.stopService())
		case "4":
			err = m.finishPair(m.restartService())
		case "5":
			err = m.finishPair(m.initialize())
		case "6":
			err = m.adminMenu()
		case "7":
			err = m.credentialMenu()
		case "8":
			err = m.finish(m.showVersion())
		default:
			fmt.Fprintln(m.out, "请输入菜单中的数字")
			continue
		}
		if err != nil {
			return err
		}
	}
}

func (m *operatorMenu) adminMenu() error {
	for {
		printTitle(m.out, "管理员账号")
		fmt.Fprint(m.out, `  1) 查看管理员
  2) 强制修改登录邮箱
  3) 强制修改密码
  0) 返回
请输入数字：`)
		choice, err := readLine(m.in)
		if err != nil {
			return err
		}
		switch choice {
		case "0":
			return nil
		case "1":
			err = m.finishPair(m.viewAdmins())
		case "2":
			err = m.finishPair(m.changeAdminEmail())
		case "3":
			err = m.finishPair(m.changeAdminPassword())
		default:
			fmt.Fprintln(m.out, "请输入菜单中的数字")
			continue
		}
		if err != nil {
			return err
		}
	}
}

func (m *operatorMenu) credentialMenu() error {
	for {
		printTitle(m.out, "初始化凭证")
		fmt.Fprint(m.out, `  1) 查看凭证状态
  2) 显示当前凭证
  3) 刷新凭证
  4) 校正文件归属
  0) 返回
请输入数字：`)
		choice, err := readLine(m.in)
		if err != nil {
			return err
		}
		switch choice {
		case "0":
			return nil
		case "1":
			err = m.finish(m.describeCredential())
		case "2":
			err = m.finishPair(m.showToken())
		case "3":
			err = m.finishPair(m.refreshToken())
		case "4":
			err = m.finishPair(m.repairFiles())
		default:
			fmt.Fprintln(m.out, "请输入菜单中的数字")
			continue
		}
		if err != nil {
			return err
		}
	}
}

func (m *operatorMenu) finish(text string) error {
	fmt.Fprintln(m.out, text)
	fmt.Fprintln(m.out, "按回车继续")
	_, err := readLine(m.in)
	return err
}

func (m *operatorMenu) finishPair(text string, err error) error {
	if err != nil {
		return err
	}
	return m.finish(text)
}

func (m *operatorMenu) serviceStatus() string {
	state, err := m.serviceState()
	if err != nil {
		return "[失败] 无法查询服务状态：" + err.Error()
	}
	switch state {
	case "active":
		return "[成功] migate-control：正在运行"
	case "inactive", "failed":
		return "[失败] migate-control：已停止"
	default:
		return "[失败] migate-control：状态未知（" + state + "）"
	}
}

func (m *operatorMenu) startService() string {
	state, err := m.serviceState()
	if err != nil {
		return "[失败] 无法查询服务状态：" + err.Error()
	}
	if state == "active" {
		return "[跳过] 服务已在运行，未再次启动"
	}
	if denied := m.requireRoot(); denied != "" {
		return denied
	}
	out, err := m.systemctl("start", "migate-control")
	if err != nil {
		return "[失败] 启动失败：" + oneLine(out)
	}
	state, err = m.serviceState()
	if err != nil || state != "active" {
		return "[失败] 启动后服务没有进入运行状态"
	}
	return "[成功] migate-control：已启动"
}

func (m *operatorMenu) stopService() (string, error) {
	if denied := m.requireRoot(); denied != "" {
		return denied, nil
	}
	ok, err := m.confirm("停止 migate-control。继续？")
	if err != nil || !ok {
		if err != nil {
			return "", err
		}
		return "[跳过] 已取消", nil
	}
	out, err := m.systemctl("stop", "migate-control")
	if err != nil {
		return "[失败] 停止失败：" + oneLine(out), nil
	}
	state, stateErr := m.serviceState()
	if stateErr != nil || (state != "inactive" && state != "failed") {
		return "[失败] 停止后服务仍未退出", nil
	}
	return "[成功] migate-control：已停止", nil
}

func (m *operatorMenu) restartService() (string, error) {
	if denied := m.requireRoot(); denied != "" {
		return denied, nil
	}
	ok, err := m.confirm("重启 migate-control。继续？")
	if err != nil || !ok {
		if err != nil {
			return "", err
		}
		return "[跳过] 已取消", nil
	}
	out, err := m.systemctl("restart", "migate-control")
	if err != nil {
		return "[失败] 重启失败：" + oneLine(out), nil
	}
	state, stateErr := m.serviceState()
	if stateErr != nil || state != "active" {
		return "[失败] 重启后服务没有进入运行状态", nil
	}
	return "[成功] migate-control：已重启", nil
}

func (m *operatorMenu) initialize() (string, error) {
	installed, text := m.installationState()
	if text != "" {
		return text, nil
	}
	if installed {
		return "[失败] 已经完成初始化，不能再次安装", nil
	}
	if err := m.runSetup(m.path); err != nil {
		if errors.Is(err, errInstallationCancelled) || errors.Is(err, errInputClosed) {
			return "[跳过] 已取消初始化", nil
		}
		return "[失败] 初始化未完成：" + oneLine(err.Error()), nil
	}
	return "[成功] 初始化向导已完成", nil
}

func (m *operatorMenu) showVersion() string {
	fmt.Fprintln(m.out, buildinfo.ControlVersionLine())
	return "[成功] 已显示版本"
}

func (m *operatorMenu) describeCredential() string {
	installed, text := m.installationState()
	if text != "" {
		return text
	}
	cred, err := readSetupCredential(m.path)
	if err != nil {
		return "[失败] 无法读取初始化状态"
	}
	tokenPath := filepath.Join(filepath.Dir(m.path), "setup-token")
	var builder strings.Builder
	fmt.Fprintf(&builder, "配置文件：%s\n", m.path)
	fmt.Fprintf(&builder, "凭证文件：%s\n", tokenPath)
	if installed {
		fmt.Fprintf(&builder, "初始化：已完成\n")
	} else {
		fmt.Fprintf(&builder, "初始化：未完成\n")
	}
	switch {
	case cred.Hash == "":
		fmt.Fprintf(&builder, "凭证：没有有效凭证\n")
	case !cred.Expires.After(time.Now()):
		fmt.Fprintf(&builder, "凭证：已过期（%s）\n", cred.Expires.Format(time.RFC3339))
	default:
		fmt.Fprintf(&builder, "凭证：有效，到期 %s\n", cred.Expires.Format(time.RFC3339))
	}
	fmt.Fprintf(&builder, "服务账号可读：配置 %s，凭证 %s\n", serviceCanRead(m.path), serviceCanRead(tokenPath))
	fmt.Fprintf(&builder, "[成功] 已显示凭证状态")
	return builder.String()
}

func (m *operatorMenu) showToken() (string, error) {
	if denied := m.rejectTokenIssue(); denied != "" {
		return denied, nil
	}
	token := readSetupToken(m.path)
	if token == "" {
		return "[失败] 没有可显示的初始化凭证", nil
	}
	fmt.Fprintln(m.out, "当前初始化凭证（30 分钟内有效）：")
	fmt.Fprintln(m.out, token)
	return "[成功] 已显示当前初始化凭证", nil
}

func (m *operatorMenu) refreshToken() (string, error) {
	if denied := m.rejectTokenIssue(); denied != "" {
		return denied, nil
	}
	cfg, err := LoadConfig(m.path)
	if err != nil {
		return "[失败] 无法读取配置", nil
	}
	token, err := m.rotate(m.path, cfg)
	if err != nil {
		return "[失败] 刷新初始化凭证失败", nil
	}
	fmt.Fprintln(m.out, "新的初始化凭证（30 分钟内有效）：")
	fmt.Fprintln(m.out, token)
	if portListening(cfg.Listen) {
		return "[成功] 运行中的服务已接受新凭证，并拒绝旧凭证", nil
	}
	return "[成功] 已写入新凭证。当前没有服务在监听，启动后会直接读取", nil
}

func (m *operatorMenu) repairFiles() (string, error) {
	if denied := m.requireRoot(); denied != "" {
		return denied, nil
	}
	before := map[string]string{}
	for _, path := range []string{m.path, filepath.Join(filepath.Dir(m.path), "setup-token")} {
		body, err := os.ReadFile(path)
		if err == nil {
			before[path] = string(body)
		}
	}
	if err := m.repair(m.path); err != nil {
		return "[失败] 校正归属失败：" + oneLine(err.Error()), nil
	}
	for path, body := range before {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != body {
			return "[失败] 校正时文件内容发生变化", nil
		}
	}
	return "[成功] 已按配置目录所有者校正文件，内容未改", nil
}

func (m *operatorMenu) viewAdmins() (string, error) {
	rows, text, err := m.admins()
	if text != "" || err != nil {
		return text, err
	}
	fmt.Fprintln(m.out, formatAdmins(rows))
	return "[成功] 已列出现有管理员", nil
}

func (m *operatorMenu) changeAdminEmail() (string, error) {
	if denied := m.requireRoot(); denied != "" {
		return denied, nil
	}
	rows, text, err := m.admins()
	if text != "" || err != nil {
		return text, err
	}
	chosen, ok, err := m.pickAdmin(rows)
	if err != nil || !ok {
		if err != nil {
			return "", err
		}
		return "[跳过] 已取消", nil
	}
	fmt.Fprint(m.out, "新的登录邮箱：")
	email, err := readLine(m.in)
	if err != nil {
		return "", err
	}
	if !validEmail(strings.ToLower(strings.TrimSpace(email))) {
		return "[失败] 登录邮箱格式无效", nil
	}
	ok, err = m.confirm(fmt.Sprintf("将修改管理员 id=%d（%s）的登录邮箱，授权和订阅令牌保持不变。继续？", chosen.ID, chosen.Email))
	if err != nil || !ok {
		if err != nil {
			return "", err
		}
		return "[跳过] 已取消", nil
	}
	cfg, err := LoadConfig(m.path)
	if err != nil {
		return "[失败] 无法读取配置", nil
	}
	cfg.Installed = true
	email = strings.ToLower(strings.TrimSpace(email))
	result, err := m.changeEmail(context.Background(), cfg, chosen.ID, email)
	if err != nil {
		return "[失败] " + oneLine(err.Error()), nil
	}
	return result.Summary("已修改管理员登录邮箱为 " + email + "，授权和订阅令牌保持不变"), nil
}

func (m *operatorMenu) changeAdminPassword() (string, error) {
	if denied := m.requireRoot(); denied != "" {
		return denied, nil
	}
	rows, text, err := m.admins()
	if text != "" || err != nil {
		return text, err
	}
	chosen, ok, err := m.pickAdmin(rows)
	if err != nil || !ok {
		if err != nil {
			return "", err
		}
		return "[跳过] 已取消", nil
	}
	first, err := m.readSecret("新密码")
	if err != nil {
		return "", err
	}
	second, err := m.readSecret("再次输入新密码")
	if err != nil {
		return "", err
	}
	if first != second {
		return "[失败] 两次密码不一致", nil
	}
	if !validAccountPassword(first) {
		return "[失败] " + accountPasswordHint, nil
	}
	ok, err = m.confirm(fmt.Sprintf("将修改管理员 id=%d（%s）的密码，授权和订阅令牌保持不变。继续？", chosen.ID, chosen.Email))
	if err != nil || !ok {
		if err != nil {
			return "", err
		}
		return "[跳过] 已取消", nil
	}
	cfg, err := LoadConfig(m.path)
	if err != nil {
		return "[失败] 无法读取配置", nil
	}
	cfg.Installed = true
	result, err := m.changePassword(context.Background(), cfg, chosen.ID, first)
	if err != nil {
		return "[失败] " + oneLine(err.Error()), nil
	}
	return result.Summary("已修改管理员密码，授权和订阅令牌保持不变"), nil
}

func (m *operatorMenu) admins() ([]AdminAccount, string, error) {
	installed, text := m.installationState()
	if text != "" {
		return nil, text, nil
	}
	if !installed {
		return nil, "[失败] 尚未完成初始化，没有管理员账号", nil
	}
	cfg, err := LoadConfig(m.path)
	if err != nil {
		return nil, "[失败] 无法读取配置", nil
	}
	cfg.Installed = true
	rows, err := m.listAdmins(context.Background(), cfg)
	if err != nil {
		return nil, "[失败] 无法读取管理员", nil
	}
	if len(rows) == 0 {
		return nil, "[失败] 没有管理员账号", nil
	}
	return rows, "", nil
}

func (m *operatorMenu) pickAdmin(rows []AdminAccount) (AdminAccount, bool, error) {
	if len(rows) == 1 {
		return rows[0], true, nil
	}
	fmt.Fprintln(m.out, formatAdmins(rows))
	fmt.Fprint(m.out, "请选择管理员编号，0 取消：")
	choice, err := readLine(m.in)
	if err != nil {
		return AdminAccount{}, false, err
	}
	if choice == "0" {
		return AdminAccount{}, false, nil
	}
	number, err := strconv.Atoi(choice)
	if err != nil || number < 1 || number > len(rows) {
		fmt.Fprintln(m.out, "请输入菜单中的数字")
		return m.pickAdmin(rows)
	}
	return rows[number-1], true, nil
}

func formatAdmins(rows []AdminAccount) string {
	var builder strings.Builder
	for i, row := range rows {
		state := "启用"
		if !row.Enabled {
			state = "停用"
		}
		fmt.Fprintf(&builder, "%d) id=%d  %s  %s\n", i+1, row.ID, row.Email, state)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func (m *operatorMenu) installationState() (bool, string) {
	cfg, err := LoadConfig(m.path)
	if err != nil {
		return false, "[失败] 无法读取配置"
	}
	cred, err := readSetupCredential(m.path)
	if err != nil {
		return false, "[失败] 无法读取初始化状态"
	}
	return cfg.Installed || cred.Installed, ""
}

func (m *operatorMenu) rejectTokenIssue() string {
	installed, text := m.installationState()
	if text != "" {
		return text
	}
	if installed {
		return "[失败] 已经完成初始化，不能再发放初始化凭证"
	}
	return m.requireRoot()
}

func (m *operatorMenu) requireRoot() string {
	if m.isRoot() {
		return ""
	}
	return "[失败] 请用 root 运行：sudo migate"
}

func (m *operatorMenu) confirm(text string) (bool, error) {
	for {
		fmt.Fprintf(m.out, "%s [y/n]：", text)
		answer, err := readLine(m.in)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(m.out, "请输入 y 或 n")
		}
	}
}

func (m *operatorMenu) serviceState() (string, error) {
	out, err := m.systemctl("is-active", "migate-control")
	state := strings.TrimSpace(out)
	switch state {
	case "active", "inactive", "failed", "activating", "deactivating":
		return state, nil
	}
	if err != nil {
		var missing *exec.Error
		if errors.As(err, &missing) && errors.Is(missing.Err, exec.ErrNotFound) {
			return "", errors.New("未找到 systemctl")
		}
		if state == "" {
			return "", errors.New("未找到 systemctl")
		}
	}
	if state == "" {
		return "", errors.New("无法识别服务状态")
	}
	return state, nil
}

func serviceCanRead(path string) string {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "文件不存在"
	}
	if err != nil || !info.Mode().IsRegular() {
		return "未知"
	}
	account, err := user.Lookup("migate-control")
	if err != nil {
		return "未知"
	}
	uid, gid, ok := ownerOf(info)
	if !ok {
		return "未知"
	}
	accountUID, err := strconv.Atoi(account.Uid)
	if err != nil {
		return "未知"
	}
	perm := info.Mode().Perm()
	if uid == accountUID && perm&0o400 != 0 {
		return "是"
	}
	accountGID, err := strconv.Atoi(account.Gid)
	if err == nil && gid == accountGID && perm&0o040 != 0 {
		return "是"
	}
	if perm&0o004 != 0 {
		return "是"
	}
	return "否"
}

func readLine(r io.Reader) (string, error) {
	var builder strings.Builder
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		if n == 1 {
			if one[0] == '\n' {
				return strings.TrimSpace(strings.TrimRight(builder.String(), "\r")), nil
			}
			builder.WriteByte(one[0])
			if builder.Len() > 1024 {
				return "", errors.New("输入过长")
			}
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) && builder.Len() > 0 {
				return strings.TrimSpace(strings.TrimRight(builder.String(), "\r")), nil
			}
			return "", err
		}
	}
}

func oneLine(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 180 {
		return text[:180]
	}
	return text
}

func printTitle(out io.Writer, title string) {
	const width = 60
	bar := strings.Repeat("=", width)
	fmt.Fprintln(out, bar)
	pad := (width - displayWidth(title)) / 2
	if pad < 0 {
		pad = 0
	}
	fmt.Fprintf(out, "%s%s\n", strings.Repeat(" ", pad), title)
	fmt.Fprintln(out, bar)
}

func displayWidth(text string) int {
	width := 0
	for _, r := range text {
		if r >= 0x1100 {
			width += 2
		} else {
			width++
		}
	}
	return width
}
