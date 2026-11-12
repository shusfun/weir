package dsh

// Agent 由宿主以独立运行身份启动。宿主与它只通过两个目录交换文件：
//   control/（中心写，dsh 只读）：desired.json 期望状态；jobs/<id>.json 后台分析任务。
//   status/（dsh 写，中心只读）：status.json 运行状态与回环端点；jobs/<id>.json 分析结果。
// runtime/ 是中心下载的 Node.js 与 dsh 各版本（dsh 只读），state/ 是 dsh 的家目录（只有 dsh 可写）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// Layout 是托管目录的结构。
type Layout struct{ Root string }

func (l Layout) Runtime() string { return filepath.Join(l.Root, "runtime") }
func (l Layout) Control() string { return filepath.Join(l.Root, "control") }
func (l Layout) Status() string  { return filepath.Join(l.Root, "status") }
func (l Layout) State() string   { return filepath.Join(l.Root, "state") }

// Desired 是中心写给管理进程的期望状态；Generation 变化即重启 dsh（换版本、换子域名、换 MCP 令牌、换插件、手动重启）。
type Desired struct {
	Compatibility      *Compatibility    `json:"compatibility,omitempty"`
	Namespace          string            `json:"namespace,omitempty"`
	Environment        map[string]string `json:"environment,omitempty"`
	Generation         int64             `json:"generation"`
	Version            string            `json:"version"` // 为空表示停止
	TrustedHost        string            `json:"trusted_host"`
	WebPatch           string            `json:"web_patch"`     // 宿主基础预设，插件不可用时仍启用
	AnalystPatch       string            `json:"analyst_patch"` // 后台分析 profile
	BackgroundProfiles map[string]string `json:"background_profiles,omitempty"`
	Instructions       string            `json:"instructions"` // 写入 $DSH_HOME/AGENTS.md 的宿主说明
	// 插件：PluginDir 是中心持有的插件目录（runtime/plugins/ 下，dsh 只读）；为空表示不加载插件。
	// 插件补丁里的 MCP 地址与令牌是占位符，由管理进程用 MCPURL / MCPToken 填入后写到 state/ 下。
	PluginDir   string `json:"plugin_dir,omitempty"`
	PluginLabel string `json:"plugin_label,omitempty"`
	MCPURL      string `json:"mcp_url"`
	MCPToken    string `json:"mcp_token"`
	// SkillsDir 是宿主生成的有效 skill 目录，填进插件补丁的占位符。
	SkillsDir string `json:"skills_dir,omitempty"`
}

// AgentStatus 是管理进程写回的状态。Token 是 dsh 的启动令牌，只给中心网关用，不返回给页面。
type AgentStatus struct {
	Status
	Generation  int64     `json:"generation"`
	TrustedHost string    `json:"trusted_host,omitempty"`
	Port        int       `json:"port,omitempty"`
	Token       string    `json:"token,omitempty"`
	Heartbeat   time.Time `json:"heartbeat"`
	Plugin      string    `json:"plugin,omitempty"`       // 实际加载的插件；为空表示只有基础预设
	PluginError string    `json:"plugin_error,omitempty"` // 插件导致 dsh 启动失败时的原因（已去掉插件重启）
	Waiting     string    `json:"waiting,omitempty"`      // 还没按最新的期望状态启动 dsh 的原因（如在等中心）
}

// JobRequest / JobResult 是交给管理进程的一次任务（目前只有 analysis：后台安全分析）。
type JobRequest struct {
	TraceID string `json:"trace_id,omitempty"`
	ID      string `json:"id"`
	Kind    string `json:"kind,omitempty"` // 为空即 analysis
	Task    string `json:"task,omitempty"`
	Profile string `json:"profile,omitempty"`
	// ToolToken replaces BackgroundTokenPlaceholder only in the host-owned
	// restricted profile. It is never added to the model task or result.
	ToolToken string `json:"tool_token,omitempty"`
}

const BackgroundTokenPlaceholder = "__HOST_BACKGROUND_TOOL_TOKEN__"

type JobResult struct {
	ID string `json:"id"`
	Result
}

var jobID = regexp.MustCompile(`^[0-9a-f]{16,64}$`)
var backgroundToken = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ValidJobID 判断任务编号是否合法（防止路径注入）。
func ValidJobID(id string) bool { return jobID.MatchString(id) }

// WriteShared 原子写入一个对方可读的文件（0640，组由目录的 setgid 继承）。
func WriteShared(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	_ = os.Chmod(tmp, 0o640) // 进程 umask 可能更严
	return os.Rename(tmp, path)
}

// WriteSharedBytes 原子写入一个对方可读的文件（内容原样）。
func WriteSharedBytes(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	_ = os.Chmod(tmp, 0o640)
	return os.Rename(tmp, path)
}

// ReadJSON 读取一个 JSON 文件。
func ReadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// SharedDir 创建对方可读的目录（2750：setgid，新文件继承目录的组）。
func SharedDir(path string) error {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return err
	}
	return os.Chmod(path, 0o750|os.ModeSetgid)
}

// RunAgent 运行管理进程，直到 ctx 结束（结束时停止 dsh）。
type AgentOptions struct {
	Observe        Observer
	Compatibility  *Compatibility
	ValidatePlugin PluginValidator
}

func RunAgent(ctx context.Context, root string, options ...AgentOptions) error {
	if err := Supported(); err != nil {
		return err
	}
	if !filepath.IsAbs(root) {
		return errors.New("agent root must be absolute")
	}
	l := Layout{Root: root}
	for _, d := range []string{l.Status(), filepath.Join(l.Status(), "jobs")} {
		if err := SharedDir(d); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(l.State(), 0o700); err != nil {
		return err
	}
	unlock, err := lockJobProfile(ctx, l.State(), "supervisor")
	if err != nil {
		return err
	}
	defer unlock()
	if err = RecoverBackgroundProfiles(ctx, Paths{Dir: l.State()}); err != nil {
		return err
	}
	a := &agent{l: l}
	if len(options) > 0 {
		a.observe = options[0].Observe
		a.compatibility = options[0].Compatibility
		a.validatePlugin = options[0].ValidatePlugin
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		a.step(ctx)
		select {
		case <-ctx.Done():
			a.runner.Stop()
			a.writeStatus()
			return nil
		case <-tick.C:
		}
	}
}

type agent struct {
	validatePlugin PluginValidator
	observe        Observer
	l              Layout
	runner         Runner
	desired        Desired
	applied        int64
	version        string
	err            string
	failures       int
	nextTry        time.Time
	busy           atomic.Bool
	compatibility  *Compatibility
	// plugin 是实际加载的插件；pluginErr 是插件没能加载的原因。
	plugin              string
	pluginErr           string
	compatibilityReason string
	// waitSince 是开始等中心的时刻：期望状态里的 MCP 地址连不上（中心重启后还没写新的期望状态）。
	waitSince time.Time
	waitWhy   string
}

// mcpWaitLimit 之后仍连不上中心的 MCP，把等待当作错误显示：中心正常运行时几秒内就会写新的期望状态。
const mcpWaitLimit = 2 * time.Minute

// mcpReachable 确认期望状态里的 MCP 地址能连上。中心每次启动都会换 MCP 的端口与令牌：
// 管理进程与中心一起重启时，control/ 里还是上一次的期望状态，按它启动 dsh 只会连到已经不存在的地址，
// 还要等最多 2 分钟的启动超时，之后中心写来新的期望状态又得再启动一次。
func mcpReachable(raw string) error {
	u, err := loopbackEndpoint(raw)
	if err != nil {
		return err
	}
	c, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err != nil {
		return errors.New("MCP endpoint unavailable")
	}
	_ = c.Close()
	return nil
}

// ready 判断能否按这份期望状态启动 dsh；不能时记下原因（只在原因变化时写日志）。
func (a *agent) ready(d Desired) bool {
	err := mcpReachable(d.MCPURL)
	if err == nil {
		if !a.waitSince.IsZero() {
			slog.Info("dsh: center MCP reachable, starting", "generation", d.Generation, "waited", time.Since(a.waitSince).Round(time.Second).String())
		}
		a.waitSince, a.waitWhy = time.Time{}, ""
		return true
	}
	if a.waitSince.IsZero() {
		a.waitSince = time.Now()
		slog.Warn("dsh: waiting for host MCP endpoint", "generation", d.Generation, "err", err)
	}
	a.waitWhy = "等中心写入新的连接设置（中心刚重启时会这样，通常几秒）"
	if time.Since(a.waitSince) > mcpWaitLimit {
		a.waitWhy = fmt.Sprintf("已等 %s 仍连不上宿主的 MCP：%v", time.Since(a.waitSince).Round(time.Second), err)
	}
	return false
}

func loopbackEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Port() == "" {
		return nil, errors.New("invalid loopback MCP endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("MCP endpoint must use a loopback IP")
	}
	return u, nil
}

func (a *agent) paths(version string) Paths {
	in := Installer{Dir: a.l.Runtime()}
	return Paths{Node: in.NodeBin(), Bin: in.Bin(version), Dir: a.l.State(), Environment: a.desired.Environment, Observe: a.observe}
}

// step 读取期望状态并让 dsh 与之一致，写回状态，再处理分析任务。
func (a *agent) step(ctx context.Context) {
	var d Desired
	if err := ReadJSON(filepath.Join(a.l.Control(), "desired.json"), &d); err != nil {
		a.err = ""
		if !errors.Is(err, os.ErrNotExist) {
			a.err = "无法读取中心的期望状态：" + err.Error()
		}
		d = Desired{}
	}
	if d.Version != "" && d.Namespace != "" && !ValidNamespace(d.Namespace) {
		a.err = "期望状态命名空间无效"
		a.writeStatus()
		return
	}
	a.desired = d
	a.compatibilityReason = ""
	// A desired-state policy is supplied by the host and can be updated
	// independently of the runtime module.
	policy := a.compatibility
	if d.Compatibility != nil {
		policy = d.Compatibility
	}
	if d.Version != "" && policy != nil {
		decision, err := CheckCompatibility(Compatibility{InstalledVersion: d.Version, VerifiedRange: policy.VerifiedRange, OutOfRange: policy.OutOfRange})
		if err != nil || !decision.EnableBase {
			a.err = "dsh compatibility policy rejected the configured version"
			a.runner.Stop()
			a.writeStatus()
			return
		}
		if !decision.EnableBackground {
			d.AnalystPatch = ""
			d.BackgroundProfiles = nil
			d.PluginDir = ""
			a.desired = d
			a.compatibilityReason = decision.Reason
		}
	}
	st := a.runner.Status()
	switch {
	case d.Version == "":
		if st.State == "running" || st.State == "starting" {
			slog.Info("dsh: stopping, disabled by center", "generation", d.Generation)
			a.runner.Stop()
		}
		a.waitSince, a.waitWhy = time.Time{}, ""
		a.applied = d.Generation
	case d.Generation != a.applied:
		// 连不上中心时先不动：正在运行的旧 dsh 保持原样，applied 不变，下一轮再看。
		if a.ready(d) {
			a.failures = 0
			a.start(ctx, d)
		}
	case st.State != "running" && !time.Now().Before(a.nextTry):
		if a.ready(d) {
			a.start(ctx, d) // 进程退出或上次启动失败：退避后重试
		}
	}
	a.writeStatus()
	a.jobs(ctx)
}

func (a *agent) fail(msg string) {
	a.err = msg
	a.failures++
	a.nextTry = time.Now().Add(time.Duration(min(a.failures, 20)) * 30 * time.Second)
	slog.Warn("dsh: start failed", "generation", a.applied, "failures", a.failures, "retry_in", time.Until(a.nextTry).Round(time.Second).String(), "err", msg)
}

func (a *agent) start(ctx context.Context, d Desired) {
	a.applied = d.Generation
	a.runner.Stop()
	// 换版本前备份 dsh 的会话与设置（只保留最近 3 份），出问题可以回退。
	marker := filepath.Join(a.l.State(), ".version")
	if b, err := os.ReadFile(marker); err == nil {
		if last := strings.TrimSpace(string(b)); last != "" && last != d.Version {
			if err := backupHome(a.l.State(), last); err != nil {
				a.fail("切换版本前备份失败：" + err.Error())
				return
			}
		}
	}
	base, plugin, pluginErr, err := a.prepareWorkspace(d)
	if err != nil {
		a.fail("准备 dsh 工作区失败：" + err.Error())
		return
	}
	p := a.paths(d.Version)
	if p.Node == "" {
		a.fail("Node.js 运行环境缺失，请在中心重新下载 dsh")
		return
	}
	if _, err := os.Stat(p.Bin); err != nil {
		a.fail("dsh " + d.Version + " 尚未下载")
		return
	}
	port, err := FreePort()
	if err != nil {
		a.fail(err.Error())
		return
	}
	// 启动最多等 2 分钟：先写一次状态，让中心看到正在启动。
	_ = WriteShared(filepath.Join(a.l.Status(), "status.json"), AgentStatus{Status: Status{State: "starting", Version: d.Version, Logs: []string{}}, Generation: d.Generation, Heartbeat: time.Now().UTC()})
	opts := WebOptions{Paths: p, Version: d.Version, Port: port, TrustedHost: d.TrustedHost, Patches: base}
	a.plugin, a.pluginErr = "", pluginErr
	if plugin != "" {
		opts.Patches = append(append([]string{}, base...), plugin)
		if err = a.runner.Start(ctx, opts); err == nil {
			a.plugin = d.PluginLabel
		} else {
			// 插件无法启动时保留宿主基础预设。
			a.pluginErr = "插件导致 dsh 启动失败，已只用基础预设启动：" + err.Error()
			opts.Patches = base
			if opts.Port, err = FreePort(); err == nil {
				err = a.runner.Start(ctx, opts)
			}
		}
	} else {
		err = a.runner.Start(ctx, opts)
	}
	if err != nil {
		a.fail(err.Error())
		return
	}
	_ = os.WriteFile(marker, []byte(d.Version), 0o600)
	a.err, a.failures = "", 0
	slog.Info("dsh: running", "generation", d.Generation, "version", d.Version, "port", opts.Port, "plugin", a.plugin != "")
}

func (a *agent) writeStatus() {
	st := a.runner.Status()
	out := AgentStatus{Status: st, Generation: a.applied, Heartbeat: time.Now().UTC(), Plugin: a.plugin, PluginError: a.pluginErr}
	if a.compatibilityReason != "" {
		out.PluginError = a.compatibilityReason
	}
	if st.State != "running" && a.err != "" {
		out.Error = a.err
	}
	// 在等中心：还没按新的期望状态启动。没有在跑的 dsh 时显示「正在启动」与等待原因，等太久才算错误。
	if !a.waitSince.IsZero() {
		out.Waiting = a.waitWhy
		if st.State != "running" {
			out.State = "starting"
		}
		if time.Since(a.waitSince) > mcpWaitLimit {
			out.Error = a.waitWhy
		}
	}
	if _, token, ok := a.runner.Endpoint(); ok {
		o := a.runner.Options()
		out.Port, out.Token, out.TrustedHost = o.Port, token, o.TrustedHost
	}
	_ = WriteShared(filepath.Join(a.l.Status(), "status.json"), out)
}

// jobs 一次只跑一个分析任务；中心删除请求后，对应结果也一并清理。
func (a *agent) jobs(ctx context.Context) {
	if a.busy.Load() {
		return
	}
	reqDir, resDir := filepath.Join(a.l.Control(), "jobs"), filepath.Join(a.l.Status(), "jobs")
	entries, _ := os.ReadDir(reqDir)
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		out := filepath.Join(resDir, e.Name())
		if !ok || !ValidJobID(id) {
			continue
		}
		if _, err := os.Stat(out); err == nil {
			continue
		}
		var req JobRequest
		if ReadJSON(filepath.Join(reqDir, e.Name()), &req) != nil || req.ID != id {
			continue
		}
		d := a.desired
		a.busy.Store(true)
		go func() {
			defer a.busy.Store(false)
			result := runRequestedJob(ctx, filepath.Join(reqDir, e.Name()), func(run context.Context) JobResult { return a.dispatch(run, d, req) })
			if _, err := os.Stat(filepath.Join(reqDir, e.Name())); err == nil {
				_ = WriteShared(out, result)
			}
		}()
		return
	}
	results, _ := os.ReadDir(resDir)
	for _, e := range results {
		if strings.HasSuffix(e.Name(), ".json") {
			if _, err := os.Stat(filepath.Join(reqDir, e.Name())); errors.Is(err, os.ErrNotExist) {
				_ = os.Remove(filepath.Join(resDir, e.Name()))
			}
		}
	}
}

// Removing the host-owned request revokes execution, including an already
// running child process. A vanished request must not leave a late result file.
func runRequestedJob(ctx context.Context, path string, run func(context.Context) JobResult) JobResult {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, err := os.Stat(path); err != nil {
		return JobResult{Result: Result{State: "cancelled", Error: "job request no longer exists"}}
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(path); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return run(ctx)
}

// runJob 用宿主提供的受限后台 profile 跑一个任务。
func (a *agent) runJob(ctx context.Context, d Desired, req JobRequest) Result {
	patch, ns := d.AnalystPatch, namespace(d.Namespace)
	if req.Profile != "" {
		if !ValidNamespace(req.Profile) || len(req.Profile) > 20 {
			return Result{State: "failed", Error: "invalid background profile"}
		}
		patch, ns = d.BackgroundProfiles[req.Profile], ns+"-"+req.Profile
	}
	if d.Version == "" || patch == "" {
		return Result{Error: "dsh 未启用"}
	}
	if strings.Contains(patch, BackgroundTokenPlaceholder) {
		if !backgroundToken.MatchString(req.ToolToken) {
			return Result{State: "failed", Error: "background tool token required"}
		}
		patch = strings.ReplaceAll(patch, BackgroundTokenPlaceholder, req.ToolToken)
	}
	if req.Task == "" || len(req.Task) > 8000 {
		return Result{Error: "任务无效"}
	}
	p := a.paths(d.Version)
	if p.Node == "" {
		return Result{Error: "Node.js 运行环境缺失"}
	}
	policy := a.compatibility
	if d.Compatibility != nil {
		policy = d.Compatibility
	}
	if policy == nil {
		return Result{State: "unknown", Error: "background compatibility policy is required"}
	}
	job := Job{Paths: p, Namespace: ns, Patch: []byte(patch), Compatibility: Compatibility{InstalledVersion: d.Version, VerifiedRange: policy.VerifiedRange, OutOfRange: policy.OutOfRange}}
	res, err := job.Run(ctx, req.Task)
	if err != nil {
		res.Error = err.Error()
	}
	if req.ToolToken != "" {
		res.Error = strings.ReplaceAll(res.Error, req.ToolToken, "<redacted>")
		res.Final = strings.ReplaceAll(res.Final, req.ToolToken, "<redacted>")
	}
	return res
}

// backupHome 把 dsh 的家目录复制到 state/backups/，保留最近 3 份。
func backupHome(state, version string) error {
	src := filepath.Join(state, "home")
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	root := filepath.Join(state, "backups")
	dst := filepath.Join(root, "home-"+version+"-"+time.Now().UTC().Format("20060102-150405"))
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, 0o700)
		case info.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, b, 0o600)
		}
		return nil
	})
	if err != nil {
		return err
	}
	entries, _ := os.ReadDir(root)
	type backup struct {
		name string
		at   time.Time
	}
	var list []backup
	for _, e := range entries {
		if info, err := e.Info(); err == nil && strings.HasPrefix(e.Name(), "home-") {
			list = append(list, backup{e.Name(), info.ModTime()})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].at.After(list[j].at) })
	for i := 3; i < len(list); i++ {
		_ = os.RemoveAll(filepath.Join(root, list[i].name))
	}
	return nil
}
