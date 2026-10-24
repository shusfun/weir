package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/publicsuffix"

	"github.com/imzyb/MiGate/internal/dsh"
	"github.com/imzyb/MiGate/internal/store/ent/setting"
)

// 托管的 dsh（DeepSeek Harness）：中心像内核一样下载、校验、切换版本，但不运行它。dsh 由独立的管理进程
// （系统用户 migate-dsh，生产环境是 systemd 单元 migate-dsh）启动，只监听回环，读不到中心的配置、数据库与密钥；
// 中心与它只通过 control/、status/ 两个目录交换文件（见 internal/dsh/agent.go）。
// 浏览器只能经 MiGate 的 dsh 子域名网关、从控制台抽屉里打开它（见 dsh_gateway.go）。
// DeepSeek 的 Key 只在 dsh 自己的设置里填写，MiGate 不保存也不读取。

const dshSettingKey = "dsh"

// DSHSettings 保存在设置键 dsh 下。
type DSHSettings struct {
	Enabled     bool   `json:"enabled"`
	Domain      string `json:"domain"`  // dsh 子域名（可带端口），反代把它转发到中心
	Version     string `json:"version"` // 当前使用的版本
	Previous    string `json:"previous,omitempty"`
	AutoAnalyze bool   `json:"auto_analyze"` // Laya 判定可疑的事件自动交给 dsh 后台分析
}

// dshHost 是中心这一侧的 dsh 状态：期望状态、管理进程（开发环境由中心启动）与状态缓存。
type dshHost struct {
	mu      sync.Mutex
	domain  string // 生效中的子域名（未启用时为空）
	desired dsh.Desired
	force   bool // 下次对账时换一个 Generation（手动重启）
	agent   *exec.Cmd
	started time.Time
	mcp     *mcpServer
	wake    chan struct{}
	cache   dsh.AgentStatus
	cacheAt time.Time
	// pluginNote 是插件没有加载的原因（版本不兼容等）。
	pluginNote string
	// endpoint / jobRunner 只在测试中替换：假 dsh 的地址与启动令牌、假的任务执行。
	endpoint  func() (string, string, bool)
	jobRunner func(context.Context, dsh.JobRequest) (dsh.JobResult, error)
}

// dshDir 是中心与 dsh 管理进程共用的托管目录；生产环境由安装脚本设置 MIGATE_DSH_DIR（/var/lib/migate/dsh）。
func (s *Server) dshDir() string {
	if v := os.Getenv("MIGATE_DSH_DIR"); v != "" {
		return v
	}
	return filepath.Join(s.dataDir(), "dsh")
}

func (s *Server) dshLayout() dsh.Layout { return dsh.Layout{Root: s.dshDir()} }

func (s *Server) dshInstaller() dsh.Installer { return dsh.DefaultInstaller(s.dshLayout().Runtime()) }

// dshExternal 表示管理进程由 systemd 单元 migate-dsh 托管，中心不自己启动它。
func dshExternal() bool { return os.Getenv("MIGATE_DSH_AGENT") == "systemd" }

func (s *Server) dshSettings(ctx context.Context) DSHSettings {
	var out DSHSettings
	if row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(dshSettingKey)).Only(ctx); err == nil {
		b, _ := json.Marshal(row.Value)
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func (s *Server) saveDSHSettings(ctx context.Context, v DSHSettings) error {
	var value map[string]any
	b, _ := json.Marshal(v)
	_ = json.Unmarshal(b, &value)
	err := s.Store.Ent.Setting.Create().SetKey(dshSettingKey).SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx)
	if err == nil {
		s.wakeDSH()
	}
	return err
}

// dshDomain 返回生效中的 dsh 子域名；网关按它分流，控制台 CSP 据此允许嵌入。
func (s *Server) dshDomain() string {
	s.dsh.mu.Lock()
	defer s.dsh.mu.Unlock()
	return s.dsh.domain
}

// agentStatus 读取管理进程写回的状态（缓存 1 秒，网关每个请求都会用到）。
func (s *Server) agentStatus() dsh.AgentStatus {
	s.dsh.mu.Lock()
	defer s.dsh.mu.Unlock()
	if time.Since(s.dsh.cacheAt) < time.Second {
		return s.dsh.cache
	}
	var st dsh.AgentStatus
	if err := dsh.ReadJSON(filepath.Join(s.dshLayout().Status(), "status.json"), &st); err != nil {
		st = dsh.AgentStatus{}
	}
	s.dsh.cache, s.dsh.cacheAt = st, time.Now()
	return st
}

// dshEndpoint 返回 dsh 的回环地址与启动令牌：只认心跳新鲜、与当前期望状态一致的运行中进程。
func (s *Server) dshEndpoint() (string, string, bool) {
	if s.dsh.endpoint != nil {
		return s.dsh.endpoint()
	}
	st := s.agentStatus()
	s.dsh.mu.Lock()
	want, domain := s.dsh.desired, s.dsh.domain
	s.dsh.mu.Unlock()
	if st.State != "running" || st.Port <= 0 || st.Port > 65535 || st.Token == "" || time.Since(st.Heartbeat) > 30*time.Second ||
		want.Generation == 0 || st.Generation != want.Generation || st.TrustedHost != domain {
		return "", "", false
	}
	return fmt.Sprintf("127.0.0.1:%d", st.Port), st.Token, true
}

func (s *Server) wakeDSH() {
	s.dsh.mu.Lock()
	if s.dsh.wake == nil {
		s.dsh.wake = make(chan struct{}, 1)
	}
	w := s.dsh.wake
	s.dsh.mu.Unlock()
	select {
	case w <- struct{}{}:
	default:
	}
}

// superviseDSH 每 15 秒（或设置变化时）让 dsh 进程与设置一致：启用且版本已安装就运行，否则停止。
func (s *Server) superviseDSH(ctx context.Context) {
	s.wakeDSH()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	defer s.stopAgent()
	for {
		s.dsh.mu.Lock()
		wake := s.dsh.wake
		s.dsh.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-wake:
		}
		s.state.RLock()
		ready := s.Config.Installed && s.Store != nil
		s.state.RUnlock()
		if ready {
			s.reconcileDSH(ctx)
		}
	}
}

// reconcileDSH 把期望状态写给管理进程：启用、有子域名且版本已下载时运行 dsh，否则停止。
// 期望状态变化（或手动重启）时换一个 Generation，管理进程据此重启 dsh；失败重试由管理进程负责。
func (s *Server) reconcileDSH(ctx context.Context) {
	set := s.dshSettings(ctx)
	domain := ""
	if set.Enabled {
		domain = set.Domain
	}
	s.dsh.mu.Lock()
	s.dsh.domain = domain
	s.dsh.mu.Unlock()
	if s.dsh.endpoint != nil || dsh.Supported() != nil {
		return // 测试替身，或本机不能托管
	}
	want, note := dsh.Desired{}, ""
	if domain != "" && set.Version != "" && s.dshInstaller().Has(set.Version) {
		m, err := s.ensureMCP()
		if err != nil {
			return
		}
		url := "http://" + m.addr + "/mcp"
		want = dsh.Desired{Version: set.Version, TrustedHost: domain, WebPatch: basePatch(url, m.webToken), AnalystPatch: analystPatch(m.addr, m.analystToken),
			Instructions: dshInstructions, MCPURL: url, MCPToken: m.webToken}
		want.PluginDir, want.PluginLabel, note = s.activePlugin(ctx, set.Version)
	}
	if err := s.prepareDSHDirs(); err != nil {
		return
	}
	if want.Version != "" {
		s.ensureAgent()
	}
	s.dsh.mu.Lock()
	cur, force := s.dsh.desired, s.dsh.force
	s.dsh.pluginNote = note
	s.dsh.mu.Unlock()
	prev := cur
	prev.Generation = 0
	if cur.Generation != 0 && prev == want && !force {
		return
	}
	// 用时间作 Generation：中心重启后不会与上一次撞号（MCP 的端口与令牌每次启动都会换，dsh 必须跟着重启）。
	want.Generation = time.Now().UnixNano()
	if err := dsh.WriteShared(filepath.Join(s.dshLayout().Control(), "desired.json"), want); err != nil {
		return
	}
	s.dsh.mu.Lock()
	s.dsh.desired, s.dsh.force = want, false
	s.dsh.mu.Unlock()
}

// prepareDSHDirs 建好托管目录：runtime/、control/ 归中心（control/ 对 dsh 组只读），status/、state/ 归管理进程。
// 生产环境由安装脚本建好并设置属主；开发容器里中心以 root 运行，这里把 status/、state/ 交给 migate-dsh。
func (s *Server) prepareDSHDirs() error {
	l := s.dshLayout()
	for _, d := range []string{l.Root, l.Runtime()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
		_ = os.Chmod(d, 0o755) // 生产环境 Root 归 root 所有，这里失败无妨
	}
	control := []string{l.Control(), filepath.Join(l.Control(), "jobs")}
	for _, d := range control {
		if err := dsh.SharedDir(d); err != nil {
			return err
		}
	}
	uid, gid, drop := dsh.AgentUser()
	for d, mode := range map[string]os.FileMode{l.Status(): 0o750, l.State(): 0o700} {
		if err := os.MkdirAll(d, mode); err != nil {
			return err
		}
		if drop {
			_ = os.Chown(d, uid, gid)
		}
	}
	if drop {
		for _, d := range control {
			_ = os.Chown(d, os.Getuid(), gid)
		}
	}
	return nil
}

// ensureAgent 在没有 systemd 单元时由中心启动管理进程（以 root 运行且有 migate-dsh 用户时切换到该用户）。
// 管理进程退出后 10 秒内不重复拉起；它自己负责 dsh 的失败退避。
func (s *Server) ensureAgent() {
	if dshExternal() {
		return
	}
	s.dsh.mu.Lock()
	defer s.dsh.mu.Unlock()
	if s.dsh.agent != nil || time.Since(s.dsh.started) < 10*time.Second {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "dsh", "agent", "--dir", s.dshDir())
	cmd.Dir = s.dshDir()
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8"}
	if v := os.Getenv("MIGATE_DSH_DEEPSEEK_BASE_URL"); v != "" {
		cmd.Env = append(cmd.Env, "MIGATE_DSH_DEEPSEEK_BASE_URL="+v)
	}
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	cmd.SysProcAttr = dsh.AgentProcAttr()
	s.dsh.started = time.Now()
	if err = cmd.Start(); err != nil {
		return
	}
	s.dsh.agent = cmd
	go func() {
		_ = cmd.Wait()
		s.dsh.mu.Lock()
		if s.dsh.agent == cmd {
			s.dsh.agent = nil
		}
		s.dsh.mu.Unlock()
	}()
}

// stopAgent 结束中心启动的管理进程（它会先停掉 dsh），最多等 15 秒。
func (s *Server) stopAgent() {
	s.dsh.mu.Lock()
	cmd := s.dsh.agent
	s.dsh.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	dsh.Terminate(cmd)
	for i := 0; i < 75; i++ {
		time.Sleep(200 * time.Millisecond)
		s.dsh.mu.Lock()
		gone := s.dsh.agent != cmd
		s.dsh.mu.Unlock()
		if gone {
			return
		}
	}
	dsh.Kill(cmd)
}

// runDSHJob 把一次后台分析交给管理进程。
func (s *Server) runDSHJob(ctx context.Context, task string) (dsh.Result, error) {
	res, err := s.dshJob(ctx, dsh.JobRequest{Kind: "analysis", Task: task})
	return res.Result, err
}

// dshJob 把一个任务交给管理进程：写 control/jobs/<id>.json，等 status/jobs/<id>.json（最多 6 分钟或 ctx 结束）。
func (s *Server) dshJob(ctx context.Context, req dsh.JobRequest) (dsh.JobResult, error) {
	if s.dsh.jobRunner != nil {
		return s.dsh.jobRunner(ctx, req)
	}
	l := s.dshLayout()
	req.ID = randomToken(16)
	path := filepath.Join(l.Control(), "jobs", req.ID+".json")
	if err := dsh.WriteShared(path, req); err != nil {
		return dsh.JobResult{}, err
	}
	defer os.Remove(path)
	out := filepath.Join(l.Status(), "jobs", req.ID+".json")
	deadline := time.Now().Add(6 * time.Minute)
	for time.Now().Before(deadline) {
		var res dsh.JobResult
		if dsh.ReadJSON(out, &res) == nil && res.ID == req.ID {
			return res, nil
		}
		select {
		case <-ctx.Done():
			return dsh.JobResult{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return dsh.JobResult{}, errors.New("dsh 任务超时")
}

// mcpPatch 是接入 MiGate 工具的 dsh 补丁层（Web 抽屉与后台分析共用这一行）。
func mcpPatch(addr, token string) string {
	return fmt.Sprintf(`# 由 MiGate 生成，每次启动覆盖。
- insert:
    - id: mcp-migate
      name: '@deepseek-ai/dsh-mcp-client'
      config:
        serverName: migate
        transport: streamable-http
        url: http://%s/mcp
        headers:
          Authorization: 'Bearer %s'
        toolCallTimeoutMs: 180000
`, addr, token)
}

// basePatch 是 MiGate 的基础预设：只有人设与 MiGate 工具，只用 dsh 最稳定的几个组合包，插件不可用时也在。
// dsh 的 MCP 工具只作用于所在的预设，所以 MiGate 工具必须放在预设里；插件加载时它的「MiGate 助手」会取代这里的默认。
func basePatch(url, token string) string {
	return fmt.Sprintf(`# 由 MiGate 生成，每次启动覆盖。
- insert:
    - id: preset-migate-basic
      name: '@deepseek-ai/dsh-agent-preset'
      config:
        id: migate-basic
        order: 0
        plugins:
          - id: persona
            name: '@deepseek-ai/dsh-persona'
            config:
              prefix: 你是 MiGate 的运维助手。只能通过 MiGate 的工具读取脱敏后的状态、提交草稿；草稿要经过实测与管理员确认才会生效。请用中文回答。
          - id: mcp-migate
            name: '@deepseek-ai/dsh-mcp-client'
            config:
              serverName: migate
              transport: streamable-http
              url: %q
              headers:
                Authorization: %q
              toolCallTimeoutMs: 180000
- id: agent-preset-registry
  config:
    default: migate-basic
`, url, "Bearer "+token)
}

// dshInstructions 写入 $DSH_HOME/AGENTS.md（每次启动 dsh 时覆盖）。
const dshInstructions = `# MiGate

本文件由 MiGate 生成，每次启动 dsh 时覆盖。这个 dsh 由 MiGate 托管，用来帮管理员调整代理节点、研判安全事件。

## MiGate 工具

- mcp__migate__get_console_context：管理员正在看的节点或页面，对话开始先调用。
- mcp__migate__list_nodes / get_node_state：节点列表与当前参数（密码、UUID、密钥显示为 <redacted>，不要猜测或修改）。
- mcp__migate__submit_node_draft：提交节点参数草稿；MiGate 在节点所在 VPS 上用真实内核实测，通过后弹窗请管理员确认，未通过会返回原因，可以修改后重新提交。
- mcp__migate__list_security_events / get_security_event / submit_defense_draft：安全事件与防御草稿，同样由管理员确认后才执行。

你无法直接改动 MiGate 的任何配置，所有改动都是草稿。

## MiGate 插件

工作区里的 {{MIGATE_PLUGIN_COPY}} 是 MiGate 维护的 dsh 插件副本。dsh 升级后插件不兼容时，可以在创造模式里按其中的 ADAPT.md 修改，
改好后请管理员在 MiGate 控制台「内核 → dsh」点「提交适配草稿」；不要用插件管理直接安装。
`

// analystPatch 是后台分析 profile：复用 Web 里保存的 Key，去掉一切能执行命令、写文件、联网的工具。
func analystPatch(addr, token string) string {
	disabled := []string{"tool-bash", "tool-pwsh", "tool-jobs", "tool-fs", "tool-fs-search", "tool-plugin-manager", "tool-working-directory", "tool-skill", "tool-subagent", "tool-subagent-fork", "tool-subagent-control", "tool-subagent-list-agents", "tool-workflow", "tool-web"}
	var b strings.Builder
	b.WriteString("# 由 MiGate 生成：后台分析只用 MiGate 的工具。\n")
	for _, id := range []string{"credentials", "authorization", "deepseek-account"} {
		fmt.Fprintf(&b, "- id: %s\n  disabled: false\n", id)
	}
	for _, id := range disabled {
		fmt.Fprintf(&b, "- id: %s\n  disabled: true\n", id)
	}
	b.WriteString(strings.Replace(mcpPatch(addr, token), "# 由 MiGate 生成，每次启动覆盖。\n", "", 1))
	return b.String()
}

func (s *Server) dshRoutes(admin *gin.RouterGroup) {
	admin.GET("/dsh", s.dshStatus)
	admin.PUT("/dsh/settings", s.putDSHSettings)
	admin.GET("/dsh/releases", s.dshReleases)
	admin.POST("/dsh/fetch", s.fetchDSH)
	admin.GET("/dsh/fetch", s.dshFetchStatus)
	admin.PUT("/dsh/version", s.setDSHVersion)
	admin.DELETE("/dsh/versions/:version", s.removeDSHVersion)
	admin.POST("/dsh/restart", s.restartDSH)
	admin.POST("/dsh/ticket", s.dshTicket)
	admin.POST("/dsh/plugin/adapt", s.adaptPlugin)
	admin.DELETE("/dsh/plugin", s.restorePlugin)
}

// dshStatus 返回设置、管理进程报告的状态（不含启动令牌）与已下载的版本。
func (s *Server) dshStatus(c *gin.Context) {
	set := s.dshSettings(c.Request.Context())
	_, _, running := s.dshEndpoint()
	agent := s.agentStatus()
	st := agent.Status
	if st.State == "" {
		st.State = "stopped"
	}
	if st.Logs == nil {
		st.Logs = []string{}
	}
	switch {
	case running:
		st.State = "running"
	case s.dsh.endpoint == nil && set.Enabled && set.Version != "" && time.Since(agent.Heartbeat) > 3*time.Minute:
		st.State, st.Error = "failed", "dsh 管理进程没有在运行"
		if dshExternal() {
			st.Error += "，请检查 systemd 单元 migate-dsh"
		}
	case st.State == "running":
		st.State = "starting" // 进程在跑，但还是上一份期望状态
	}
	supported := ""
	if err := dsh.Supported(); err != nil {
		supported = err.Error()
	}
	installed := s.dshInstaller().List()
	if installed == nil {
		installed = []dsh.Installed{}
	}
	s.dsh.mu.Lock()
	want, note := s.dsh.desired, s.dsh.pluginNote
	s.dsh.mu.Unlock()
	_, adaptErr := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(dshPluginSettingKey)).Only(c.Request.Context())
	plugin := gin.H{"label": want.PluginLabel, "note": note, "loaded": agent.Plugin, "error": agent.PluginError, "adapted": adaptErr == nil}
	c.JSON(200, gin.H{"settings": set, "status": st, "installed": installed, "unsupported": supported, "ready": running && s.dshDomain() != "", "plugin": plugin})
}

// putDSHSettings 修改启用状态与子域名需要重新验证身份；子域名必须与当前控制台同站、不同源。
func (s *Server) putDSHSettings(c *gin.Context) {
	var req struct {
		Enabled     bool   `json:"enabled"`
		Domain      string `json:"domain"`
		AutoAnalyze bool   `json:"auto_analyze"`
		Password    string `json:"password"`
		Code        string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if domain != "" {
		if err := checkDSHDomain(domain, requestHost(c.Request)); err != nil {
			failDetail(c, 400, "invalid_dsh_domain", err.Error(), nil)
			return
		}
	}
	if req.Enabled && domain == "" {
		failDetail(c, 400, "invalid_dsh_domain", "启用前请填写 dsh 子域名", nil)
		return
	}
	ctx := c.Request.Context()
	set := s.dshSettings(ctx)
	set.Enabled, set.Domain, set.AutoAnalyze = req.Enabled, domain, req.AutoAnalyze
	if err := s.saveDSHSettings(ctx, set); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "dsh.settings", domain, "ok")
	c.JSON(200, gin.H{"settings": set})
}

// checkDSHDomain：合法主机名（可带端口），与控制台不同源但同站（同一注册域，或同一 IP / localhost 的不同端口）。
func checkDSHDomain(domain, console string) error {
	if !validHost(domain) || strings.ContainsAny(domain, "[]") {
		return errors.New("子域名格式无效")
	}
	if domain == console {
		return errors.New("dsh 子域名不能与控制台地址相同")
	}
	hostOnly := func(h string) string {
		if host, _, err := net.SplitHostPort(h); err == nil {
			return host
		}
		return h
	}
	a, b := hostOnly(domain), hostOnly(console)
	site := func(h string) string {
		if net.ParseIP(h) != nil || !strings.Contains(h, ".") {
			return h
		}
		if s, err := publicsuffix.EffectiveTLDPlusOne(h); err == nil {
			return s
		}
		return h
	}
	if site(a) != site(b) {
		return fmt.Errorf("dsh 子域名必须与控制台 %s 属于同一站点（例如 dsh.%s）", b, site(b))
	}
	return nil
}

func (s *Server) dshReleases(c *gin.Context) {
	list, err := s.dshInstaller().Releases(c.Request.Context())
	if err != nil {
		c.AbortWithStatusJSON(502, gin.H{"error": gin.H{"code": "upstream_unavailable", "message": "无法获取 dsh 版本：" + err.Error()}})
		return
	}
	c.JSON(200, gin.H{"releases": list})
}

// fetchDSH 后台下载并校验一个版本（与内核下载相同：页面轮询状态）。
func (s *Server) fetchDSH(c *gin.Context) {
	var req struct {
		Version string `json:"version"`
	}
	if !bind(c, &req) {
		return
	}
	if !dsh.ValidVersion(req.Version) {
		failure(c, 400, "invalid_version")
		return
	}
	if err := dsh.Supported(); err != nil {
		failDetail(c, 409, "dsh_unsupported", err.Error(), nil)
		return
	}
	ctx := c.Request.Context()
	key := "dsh-fetch:" + req.Version
	if ok, err := s.Store.KV.SetNX(ctx, key, `{"status":"running"}`, 40*time.Minute); err != nil {
		failure(c, 503, "dsh_fetch_unavailable")
		return
	} else if !ok {
		raw, _, _ := s.Store.KV.Get(ctx, key)
		if strings.Contains(raw, `"running"`) {
			c.JSON(202, gin.H{"status": "running"})
			return
		}
		_ = s.Store.KV.Set(ctx, key, `{"status":"running"}`, 40*time.Minute)
	}
	actor, ip := c.GetInt("user_id"), c.ClientIP()
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
		defer cancel()
		meta, err := s.dshInstaller().Install(bg, req.Version)
		result, res := map[string]any{"status": "done", "integrity": meta.Integrity, "registry": meta.Registry}, "ok"
		if err != nil {
			result, res = map[string]any{"status": "failed", "error": err.Error()}, "failed"
		}
		b, _ := json.Marshal(result)
		_ = s.Store.KV.Set(bg, key, string(b), time.Hour)
		_, _ = s.Store.Ent.Audit.Create().SetActor(actor).SetAction("dsh.fetch").SetTarget(req.Version).SetResult(res).SetIP(ip).Save(bg)
	}()
	c.JSON(202, gin.H{"status": "running"})
}

func (s *Server) dshFetchStatus(c *gin.Context) {
	raw, ok, err := s.Store.KV.Get(c.Request.Context(), "dsh-fetch:"+c.Query("version"))
	if err != nil {
		failure(c, 503, "dsh_fetch_unavailable")
		return
	}
	if !ok {
		c.JSON(200, gin.H{"status": "none"})
		return
	}
	c.Data(200, "application/json", []byte(raw))
}

// setDSHVersion 切换版本：记下上一版本以便回退；管理进程据新的期望状态停掉旧版本、备份 dsh 的会话与设置，再按新版本启动。
func (s *Server) setDSHVersion(c *gin.Context) {
	var req struct {
		Version string `json:"version"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	if !dsh.ValidVersion(req.Version) || !s.dshInstaller().Has(req.Version) {
		failure(c, 409, "dsh_not_installed")
		return
	}
	set := s.dshSettings(ctx)
	if set.Version != req.Version {
		if set.Version != "" {
			set.Previous = set.Version
		}
		set.Version = req.Version
		if err := s.saveDSHSettings(ctx, set); err != nil {
			failure(c, 500, "save_failed")
			return
		}
	}
	s.audit(c, "dsh.version", req.Version, "ok")
	c.JSON(200, gin.H{"settings": set})
}

func (s *Server) removeDSHVersion(c *gin.Context) {
	v := c.Param("version")
	if set := s.dshSettings(c.Request.Context()); v == set.Version {
		failure(c, 409, "dsh_version_in_use")
		return
	}
	if err := s.dshInstaller().Remove(v); err != nil {
		failure(c, 400, "invalid_version")
		return
	}
	s.audit(c, "dsh.remove", v, "ok")
	c.Status(204)
}

// restartDSH 换一个 Generation，管理进程据此重启 dsh。
func (s *Server) restartDSH(c *gin.Context) {
	s.dsh.mu.Lock()
	s.dsh.force = true
	s.dsh.mu.Unlock()
	s.wakeDSH()
	s.audit(c, "dsh.restart", "", "ok")
	c.Status(202)
}

// dshTicket 给抽屉签发一次性票据（60 秒），绑定当前管理员会话与控制台来源。
func (s *Server) dshTicket(c *gin.Context) {
	domain := s.dshDomain()
	if domain == "" {
		failure(c, 409, "dsh_not_enabled")
		return
	}
	if _, _, ok := s.dshEndpoint(); !ok {
		failure(c, 503, "dsh_not_running")
		return
	}
	t := randomToken(32)
	b, _ := json.Marshal(dshGrant{UserID: c.GetInt("user_id"), SessionKey: c.GetString("session_key"), Origin: requestOrigin(c.Request)})
	if err := s.Store.KV.Set(c.Request.Context(), "dsh-ticket:"+digest(t), string(b), time.Minute); err != nil {
		failure(c, 503, "dsh_ticket_unavailable")
		return
	}
	// 控制台上下文（可选）：管理员从哪个节点或页面打开了抽屉；dsh 用 get_console_context 读取。
	var hint struct {
		NodeID int    `json:"node_id"`
		Path   string `json:"path"`
	}
	if raw, _ := io.ReadAll(io.LimitReader(c.Request.Body, 4096)); len(raw) > 0 && json.Unmarshal(raw, &hint) == nil {
		if len(hint.Path) > 200 || !strings.HasPrefix(hint.Path, "/") {
			hint.Path = ""
		}
		v, _ := json.Marshal(map[string]any{"user_id": c.GetInt("user_id"), "node_id": hint.NodeID, "path": hint.Path, "at": time.Now().UTC()})
		_ = s.Store.KV.Set(c.Request.Context(), "dsh-context", string(v), 2*time.Hour)
	}
	s.audit(c, "dsh.open", "", "ok")
	c.JSON(200, gin.H{"url": requestScheme(c.Request) + "://" + domain + dshOpenPath + "?t=" + t})
}
