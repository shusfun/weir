package dsh

// Agent 是 dsh 的管理进程，以独立系统用户 migate-dsh 运行（生产环境由 systemd 单元 migate-dsh 启动，
// 开发容器里由中心降权启动）。中心与它只通过两个目录交换文件，dsh 读不到中心的配置、数据库与密钥：
//   control/（中心写，dsh 只读）：desired.json 期望状态；jobs/<id>.json 后台分析任务。
//   status/（dsh 写，中心只读）：status.json 运行状态与回环端点；jobs/<id>.json 分析结果。
// runtime/ 是中心下载的 Node.js 与 dsh 各版本（dsh 只读），state/ 是 dsh 的家目录（只有 dsh 可写）。

import (
	"context"
	"encoding/json"
	"errors"
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
	Generation   int64  `json:"generation"`
	Version      string `json:"version"` // 为空表示停止
	TrustedHost  string `json:"trusted_host"`
	WebPatch     string `json:"web_patch"`     // Weir 基础预设（只有人设与 MCP 工具，插件不可用时也在）
	AnalystPatch string `json:"analyst_patch"` // 后台分析 profile
	Instructions string `json:"instructions"`  // 写入 $DSH_HOME/AGENTS.md 的 Weir 说明
	// 插件：PluginDir 是中心持有的插件目录（runtime/plugins/ 下，dsh 只读）；为空表示不加载插件。
	// 插件补丁里的 MCP 地址与令牌是占位符，由管理进程用 MCPURL / MCPToken 填入后写到 state/ 下。
	PluginDir   string `json:"plugin_dir,omitempty"`
	PluginLabel string `json:"plugin_label,omitempty"`
	MCPURL      string `json:"mcp_url"`
	MCPToken    string `json:"mcp_token"`
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
}

// JobRequest / JobResult 是交给管理进程的一次任务：analysis 后台分析；plugin-collect 读取工作区里
// 创造模式改好的插件副本；plugin-smoke 用全新的临时家目录带着插件试启动一次 dsh。
type JobRequest struct {
	ID        string `json:"id"`
	Kind      string `json:"kind,omitempty"` // 为空即 analysis
	Task      string `json:"task,omitempty"`
	PluginDir string `json:"plugin_dir,omitempty"`
}

type JobResult struct {
	ID string `json:"id"`
	Result
	Files map[string]string `json:"files,omitempty"` // plugin-collect
	Log   []string          `json:"log,omitempty"`   // plugin-smoke（令牌已隐藏）
}

var jobID = regexp.MustCompile(`^[0-9a-f]{16,64}$`)

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
func RunAgent(ctx context.Context, root string) error {
	if err := Supported(); err != nil {
		return err
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
	a := &agent{l: l}
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
	l        Layout
	runner   Runner
	desired  Desired
	applied  int64
	version  string
	err      string
	failures int
	nextTry  time.Time
	busy     atomic.Bool
	// plugin 是实际加载的插件；pluginErr 是插件没能加载的原因。
	plugin    string
	pluginErr string
}

func (a *agent) paths(version string) Paths {
	in := Installer{Dir: a.l.Runtime()}
	return Paths{Node: in.NodeBin(), Bin: in.Bin(version), Dir: a.l.State()}
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
	a.desired = d
	st := a.runner.Status()
	switch {
	case d.Version == "":
		if st.State == "running" || st.State == "starting" {
			a.runner.Stop()
		}
		a.applied = d.Generation
	case d.Generation != a.applied:
		a.failures = 0
		a.start(ctx, d)
	case st.State != "running" && !time.Now().Before(a.nextTry):
		a.start(ctx, d) // 进程退出或上次启动失败：退避后重试
	}
	a.writeStatus()
	a.jobs(ctx)
}

func (a *agent) fail(msg string) {
	a.err = msg
	a.failures++
	a.nextTry = time.Now().Add(time.Duration(min(a.failures, 20)) * 30 * time.Second)
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
			// 插件与这个 dsh 版本不兼容或写错：去掉插件、只用基础预设重启，Weir 的工具照常可用。
			a.pluginErr = "Weir 插件导致 dsh 启动失败，已只用基础预设启动：" + err.Error()
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
}

func (a *agent) writeStatus() {
	st := a.runner.Status()
	out := AgentStatus{Status: st, Generation: a.applied, Heartbeat: time.Now().UTC(), Plugin: a.plugin, PluginError: a.pluginErr}
	if st.State != "running" && a.err != "" {
		out.Error = a.err
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
			_ = WriteShared(out, a.dispatch(ctx, d, req))
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

// runJob 用后台分析 profile（只有 Weir 的工具，没有 Shell、文件与联网）跑一个任务。
func (a *agent) runJob(ctx context.Context, d Desired, req JobRequest) Result {
	if d.Version == "" || d.AnalystPatch == "" {
		return Result{Error: "dsh 未启用"}
	}
	if req.Task == "" || len(req.Task) > 8000 {
		return Result{Error: "任务无效"}
	}
	p := a.paths(d.Version)
	if p.Node == "" {
		return Result{Error: "Node.js 运行环境缺失"}
	}
	run, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := EnsureProfile(run, p, "migate-analyst", "headless", []byte(d.AnalystPatch)); err != nil {
		return Result{Error: err.Error()}
	}
	res, err := RunHeadless(run, p, "migate-analyst", req.Task)
	if err != nil {
		return Result{Error: err.Error()}
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
