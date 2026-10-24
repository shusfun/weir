package dsh

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Paths 是一次运行用到的目录与程序。
type Paths struct {
	Node string // node 程序
	Bin  string // dsh 的 lib/bin.js
	Dir  string // 托管根目录
}

func (p Paths) Home() string { return filepath.Join(p.Dir, "home") }
func (p Paths) Work() string { return filepath.Join(p.Dir, "work") }

// env 是 dsh 进程的完整环境：只给运行需要的变量，不继承中心的任何环境变量（数据库、加密密钥等）。
// MIGATE_DSH_DEEPSEEK_BASE_URL 只用于本地验收时把 dsh 指向假模型。
func (p Paths) env() []string {
	env := []string{
		"PATH=" + filepath.Dir(p.Node) + ":/usr/local/bin:/usr/bin:/bin",
		"HOME=" + filepath.Join(p.Dir, "user"),
		"DSH_HOME=" + p.Home(),
		"LANG=C.UTF-8",
	}
	if v := os.Getenv("MIGATE_DSH_DEEPSEEK_BASE_URL"); v != "" {
		env = append(env, "DEEPSEEK_BASE_URL="+v)
	}
	return env
}

// prepare 建好 dsh 的家目录与工作目录（管理进程本身已是 migate-dsh 用户，这里不再降权）。
func (p Paths) prepare() error {
	for _, d := range []string{p.Home(), p.Work(), filepath.Join(p.Dir, "user")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// FreePort 取一个本机回环上的空闲端口。
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// WebOptions 是 dsh Web 服务的启动参数。
type WebOptions struct {
	Paths
	Version     string
	Port        int
	TrustedHost string // 浏览器看到的 dsh 子域名（含端口时一并写上）
	PublicURL   string
	Patches     []string // 叠加的补丁层：工作区、Weir 基础预设、Weir 插件（按顺序，后者覆盖前者）
}

// Status 是 dsh Web 进程的状态。
type Status struct {
	State     string    `json:"state"` // stopped / starting / running / failed
	Version   string    `json:"version,omitempty"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	Logs      []string  `json:"logs"`
}

// Runner 管理一个 dsh Web 进程。启动令牌只保存在内存里，从不写进日志或返回给页面。
type Runner struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	opts    WebOptions
	status  Status
	token   string
	stopped bool
	done    chan struct{}
}

var launchLine = regexp.MustCompile(`dsh web: (\S+)`)
var tokenParam = regexp.MustCompile(`token=[A-Za-z0-9_-]+`)

// Start 启动 dsh Web 并等待它打印启动地址（最多 2 分钟）；已有进程时先停止。
func (r *Runner) Start(ctx context.Context, o WebOptions) error {
	r.Stop()
	if err := o.prepare(); err != nil {
		return err
	}
	args := []string{o.Bin, "--profile", "web"}
	for _, p := range o.Patches {
		args = append(args, "--patch", p)
	}
	args = append(args, "--no-open", "--port", fmt.Sprint(o.Port), "--trusted-host", o.TrustedHost)
	if o.PublicURL != "" {
		args = append(args, "--public-url", o.PublicURL)
	}
	cmd := exec.Command(o.Node, args...)
	cmd.Dir, cmd.Env = o.Work(), o.env()
	setProcAttr(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err = cmd.Start(); err != nil {
		return err
	}
	ready := make(chan string, 1)
	done := make(chan struct{})
	r.mu.Lock()
	r.cmd, r.opts, r.token, r.stopped, r.done = cmd, o, "", false, done
	r.status = Status{State: "starting", Version: o.Version, StartedAt: time.Now().UTC()}
	r.mu.Unlock()
	go r.read(stdout, ready)
	go func() {
		err := cmd.Wait()
		r.mu.Lock()
		if r.cmd == cmd {
			r.token = ""
			if r.stopped {
				r.status.State = "stopped"
			} else {
				r.status.State = "failed"
				r.status.Error = "dsh 进程已退出"
				if err != nil {
					r.status.Error += "：" + err.Error()
				}
			}
		}
		r.mu.Unlock()
		close(done)
	}()
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	select {
	case token := <-ready:
		r.mu.Lock()
		r.token, r.status.State = token, "running"
		r.mu.Unlock()
		return nil
	case <-done:
		return errors.New(r.Status().Error)
	case <-timer.C:
	case <-ctx.Done():
	}
	r.Stop()
	r.mu.Lock()
	r.status.State, r.status.Error = "failed", "dsh 启动超时"
	r.mu.Unlock()
	return errors.New("dsh 启动超时")
}

// read 收集输出：识别启动地址取出令牌；日志里的令牌一律打码。
func (r *Runner) read(out io.Reader, ready chan<- string) {
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	sent := false
	for sc.Scan() {
		line := sc.Text()
		if m := launchLine.FindStringSubmatch(line); m != nil && !sent {
			if u, err := url.Parse(m[1]); err == nil && u.Query().Get("token") != "" {
				ready <- u.Query().Get("token")
				sent = true
			}
		}
		line = tokenParam.ReplaceAllString(line, "token=<已隐藏>")
		r.mu.Lock()
		r.status.Logs = append(r.status.Logs, line)
		if len(r.status.Logs) > 80 {
			r.status.Logs = r.status.Logs[len(r.status.Logs)-80:]
		}
		r.mu.Unlock()
	}
}

// Stop 结束进程（连同它启动的子进程）。
func (r *Runner) Stop() {
	r.mu.Lock()
	cmd, done := r.cmd, r.done
	r.stopped = true
	r.mu.Unlock()
	if cmd == nil || cmd.Process == nil || done == nil {
		return
	}
	select {
	case <-done:
		return
	default:
	}
	terminate(cmd)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		kill(cmd)
		<-done
	}
}

// Status 返回当前状态（日志已打码）。
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.status
	if s.State == "" {
		s.State = "stopped"
	}
	s.Logs = append([]string(nil), r.status.Logs...)
	return s
}

// Endpoint 返回正在运行的 dsh 的回环地址与启动令牌。
func (r *Runner) Endpoint() (addr, token string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.State != "running" || r.token == "" {
		return "", "", false
	}
	return fmt.Sprintf("127.0.0.1:%d", r.opts.Port), r.token, true
}

// Options 返回当前进程的启动参数（判断是否需要重启）。
func (r *Runner) Options() WebOptions {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.opts
}

// EnsureProfile 确保 DSH_HOME 下有名为 name 的 profile（从官方模板复制），并写入中心维护的补丁层。
func EnsureProfile(ctx context.Context, p Paths, name, template string, patch []byte) error {
	if err := p.prepare(); err != nil {
		return err
	}
	dir := filepath.Join(p.Home(), "profiles", name)
	if _, err := os.Stat(filepath.Join(dir, "cordis.yml")); err != nil {
		cmd := exec.CommandContext(ctx, p.Node, p.Bin, "--profile", name, "--from-default-profile", template, "--help")
		cmd.Dir, cmd.Env = p.Work(), p.env()
		setProcAttr(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("创建 dsh profile 失败：%s", tail(string(out), 300))
		}
	}
	return os.WriteFile(filepath.Join(dir, "cordis.patch.yml"), patch, 0o600)
}

// Result 是一次 headless 运行的结果（来自 --json 事件流）。
type Result struct {
	ToolCalls []string `json:"tool_calls"`
	Final     string   `json:"final"`
	Completed bool     `json:"completed"`
	Error     string   `json:"error,omitempty"`
}

// RunHeadless 用指定 profile 跑一个任务。dsh 在工具连不上时仍可能以 0 退出，所以结果以事件流为准。
func RunHeadless(ctx context.Context, p Paths, profile, task string) (Result, error) {
	cmd := exec.CommandContext(ctx, p.Node, p.Bin, "--profile", profile, "--json", task)
	cmd.Dir, cmd.Env = p.Work(), p.env()
	setProcAttr(cmd)
	cmd.Cancel = func() error { kill(cmd); return nil }
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	var stderr strings.Builder
	cmd.Stderr = &limited{w: &stderr, n: 4096}
	if err = cmd.Start(); err != nil {
		return Result{}, err
	}
	res := parseEvents(out)
	if err = cmd.Wait(); err != nil && res.Error == "" {
		res.Error = strings.TrimSpace(tail(stderr.String(), 300))
		if res.Error == "" {
			res.Error = err.Error()
		}
	}
	return res, nil
}

func parseEvents(r io.Reader) Result {
	var res Result
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var e struct {
			Type    string `json:"type"`
			Tool    string `json:"tool"`
			Text    string `json:"text"`
			Phase   string `json:"phase"`
			Message string `json:"message"`
			Reason  struct {
				Kind string `json:"kind"`
			} `json:"reason"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e.Type {
		case "tool_call":
			res.ToolCalls = append(res.ToolCalls, e.Tool)
		case "final":
			res.Final = e.Text
		case "status":
			if e.Phase == "turn_end" {
				res.Completed = e.Reason.Kind == "completed"
			}
		case "error":
			res.Error = e.Message
		}
	}
	return res
}

type limited struct {
	w io.Writer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		l.n -= k
		_, _ = l.w.Write(p[:k])
	}
	return len(p), nil
}
