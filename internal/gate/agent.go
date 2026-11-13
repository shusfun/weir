package gate

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/hoststats"
	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/netbudget"
	"github.com/shusfun/weir/internal/xray"
	"github.com/shusfun/weir/packages/contracts"
)

// Agent 以 weir-gate 身份运行：与中心通信、计量、本地执行额度与到期，经辅助程序应用配置。
type Agent struct {
	P      Paths
	cfg    Config
	center *centerClient
	helper *helperClient
	mu     sync.Mutex // 保护 st 与应用过程
	st     State
	pids   map[string]int
	forced map[string]bool
	// 计量与心跳周期；ReportEvery 为每隔几次计量上报一次中心。测试中可调短。
	MeterEvery         time.Duration
	HeartbeatEvery     time.Duration
	HeartbeatFullEvery time.Duration
	HostSampleEvery    time.Duration
	ReportEvery        int64
	// switchTried 是上一次尝试切换中心地址的时间（由 mu 保护）。
	switchTried time.Time
	// 实时上报（live.go）：期限与是否在跑，由 liveMu 保护；runCtx 是 Run 的上下文。
	liveMu      sync.Mutex
	liveUntil   time.Time
	liveRunning bool
	runCtx      context.Context
	// up 是自更新的进度与失败冷却（selfupdate.go），随心跳上报。
	up updater
	// 测试替换：replaceFn 代替辅助程序替换程序；extHTTP 是访问 GitHub 的客户端；ghHost 是允许的 GitHub 主机；
	// uploadBackoff 是上传重试的起始间隔。
	replaceFn         func(ctx context.Context, version, sum string, data []byte) error
	extHTTP           *http.Client
	ghHost            string
	uploadBackoff     time.Duration
	meterProtocol     atomic.Int32
	heartbeatProtocol atomic.Int32
	heartbeats        heartbeatState
	reportedEventAt   time.Time
	workerBudgetMu    sync.RWMutex
	workerBudget      contracts.WorkerBudget
	workerPacer       netbudget.Pacer
	workerObserved    workerObservations
	downloads         downloadCoordinator
}

func NewAgent(p Paths) (*Agent, error) {
	cfg, err := LoadConfig(p.Config)
	if err != nil {
		return nil, err
	}
	cc, err := newCenterClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Agent{P: p, cfg: cfg, center: cc, helper: newHelperClient(p.Socket), st: loadState(filepath.Join(p.Data, "state.json")), pids: map[string]int{},
		MeterEvery: 10 * time.Second, HeartbeatEvery: 30 * time.Second, HeartbeatFullEvery: contracts.HeartbeatFullEvery, HostSampleEvery: contracts.HostSampleEvery, ReportEvery: 3}, nil
}

func (a *Agent) statePath() string { return filepath.Join(a.P.Data, "state.json") }

// Run 启动时先按本地已生效配置重建（中心不可达也能继续服务），然后并行运行各循环。
func (a *Agent) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	var loops sync.WaitGroup
	defer func() { cancel(); loops.Wait() }()
	hostSampler.CertDir = a.P.CertsDir
	hostSampler.DNS = hoststats.CompareDNS
	if err := cleanupAbandonedAssetDownloads(filepath.Join(a.P.Data, "assets")); err != nil {
		return fmt.Errorf("abandoned download cleanup: %w", err)
	}
	a.runCtx = ctx
	a.mu.Lock()
	if a.st.Config != nil {
		if _, err := a.applyLocked(ctx, *a.st.Config); err != nil {
			log.Printf("reapply last config: %v", err)
		}
	}
	_ = a.st.save(a.statePath())
	a.mu.Unlock()
	loops.Go(func() { a.loop(ctx, a.HeartbeatEvery, a.heartbeat) })
	loops.Go(func() { a.loop(ctx, a.MeterEvery, a.meter) })
	loops.Go(func() { a.observeWorkerComponents(ctx) })
	a.workerLoop(ctx)
	return ctx.Err()
}

func (a *Agent) loop(ctx context.Context, every time.Duration, fn func(context.Context) error) {
	for {
		if err := fn(ctx); err != nil && ctx.Err() == nil {
			log.Printf("%v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

func (a *Agent) configLoop(ctx context.Context) {
	for ctx.Err() == nil {
		a.mu.Lock()
		since := a.st.Applied
		execSince := int64(0)
		if a.st.Config != nil {
			execSince = a.st.Config.ExecutionSeq
		}
		a.mu.Unlock()
		var cfg contracts.GateConfig
		code, err := a.center.call(ctx, "GET", "/agent/config?since="+strconv.FormatInt(since, 10)+"&exec_since="+strconv.FormatInt(execSince, 10), nil, &cfg)
		switch {
		case err != nil:
			// 409：中心暂时无法编译（例如内核尚未下载），保持当前配置稍后重试。
			wait := 5 * time.Second
			if code == 409 {
				wait = 30 * time.Second
			}
			log.Printf("config: %v", err)
			sleep(ctx, wait)
			continue
		case code == 204:
			continue
		case cfg.ExecutionOnly:
			a.mu.Lock()
			state := a.applyExecution(ctx, cfg)
			a.mu.Unlock()
			if _, err = a.center.call(ctx, "POST", "/agent/state", state, nil); err != nil {
				log.Printf("report execution: %v", err)
			}
			continue
		case cfg.CertOnly:
			if err = a.ensureCerts(ctx, cfg); err != nil {
				log.Printf("cert sync: %v", err)
				sleep(ctx, 10*time.Second)
				continue
			}
			log.Printf("cert sync: revision %d refs %d", cfg.Revision, len(cfg.Certs))
			if _, err = a.center.call(ctx, "POST", "/agent/state", contracts.GateState{AppliedRevision: a.appliedRevision(), CertSynced: true}, nil); err != nil {
				log.Printf("report cert sync: %v", err)
			}
			continue
		}
		// 跟随中心版本只看心跳回复（heartbeat → followCenter），这里不再检查 cfg.Version。
		a.mu.Lock()
		configCtx := configurationContext(ctx, cfg)
		state, err := a.applySyncedConfig(configCtx, cfg)
		a.mu.Unlock()
		if err != nil {
			log.Printf("apply revision %d: %v", cfg.Revision, err)
			sleep(ctx, 10*time.Second)
			continue
		}
		// 中心的配置版本号，两边的日志按它对上。
		if state.FailedRevision == cfg.Revision {
			log.Printf("revision %d rejected, keeping %d: %s", cfg.Revision, state.AppliedRevision, state.Error)
		} else {
			log.Printf("revision %d applied", state.AppliedRevision)
		}
		if _, err = a.center.call(configCtx, "POST", "/agent/state", state, nil); err != nil {
			log.Printf("report state: %v", err)
		}
		// 配置被拒绝时等一会再取，避免反复应用同一份坏配置；端口冲突时中心会马上换端口发新版本，直接去取（长轮询，不会空转）。
		if state.FailedRevision != 0 && state.ConflictPort == 0 {
			sleep(ctx, 30*time.Second)
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// ensureKernels 下载并安装配置要求、本机还没有的内核版本（摘要必须与中心下发的一致）。
func (a *Agent) ensureKernels(ctx context.Context, cfg contracts.GateConfig) error {
	_, background := ctx.Value(taskLeaseKey{}).(taskLease)
	release, err := a.downloads.acquire(ctx, !background)
	if err != nil {
		return err
	}
	defer release()
	st, err := a.helper.Status(ctx)
	if err != nil {
		return err
	}
	for name, k := range cfg.Kernels {
		if !installable(name) || !safeVer.MatchString(k.Version) {
			return errors.New("invalid kernel reference")
		}
		have := false
		for _, v := range st.Installed[name] {
			have = have || v == k.Version
		}
		if have && a.installedKernelMatches(name, k) {
			continue
		}
		data, err := a.downloadKernel(ctx, name, k)
		if err != nil {
			return fmt.Errorf("download %s %s: %w", name, k.Version, err)
		}
		sum := sumOf(data)
		if err = a.checkExecutionLease(ctx); err != nil {
			return err
		}
		installed := beginTaskStage(ctx, "kernel.install", "gate.ensureKernels")
		if err = a.helper.InstallKernel(ctx, name, k.Version, sum, data); err != nil {
			installed(err)
			return err
		}
		installed(nil)
		if k.Artifact != nil {
			if err = writeJournal(a.kernelReceiptPath(name, k.Version), kernelReceipt{ArchiveSHA256: k.Artifact.SHA256, BinarySHA256: sum}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ensureCerts 下载配置引用、本机缺失或摘要不符的证书。
func (a *Agent) ensureCerts(ctx context.Context, cfg contracts.GateConfig) error {
	if len(cfg.Certs) == 0 {
		return nil
	}
	st, err := a.helper.Status(ctx)
	if err != nil {
		return err
	}
	for _, ref := range cfg.Certs {
		if ref.Panel != "" {
			// 1Panel 管理的本机证书：私钥只在本机，缺文件时经 1Panel 取得；1Panel 续签后摘要会变，不核对。
			if _, ok := st.Certs[strconv.FormatInt(ref.ID, 10)]; !ok {
				if _, err = a.helper.PanelCert(ctx, ref.ID, ref.Panel); err != nil {
					return fmt.Errorf("certificate %d via 1Panel: %w", ref.ID, err)
				}
			}
			continue
		}
		if st.Certs[strconv.FormatInt(ref.ID, 10)] == ref.SHA256 {
			continue
		}
		var body struct {
			Cert string `json:"cert_pem"`
			Key  string `json:"key_pem"`
		}
		if _, err = a.center.call(ctx, "GET", fmt.Sprintf("/agent/certs/%d", ref.ID), nil, &body); err != nil {
			return fmt.Errorf("download certificate %d: %w", ref.ID, err)
		}
		if contracts.CertSum(body.Cert, body.Key) != ref.SHA256 {
			return fmt.Errorf("certificate %d checksum mismatch", ref.ID)
		}
		if err = a.helper.InstallCert(ctx, ref.ID, body.Cert, body.Key); err != nil {
			return err
		}
	}
	return nil
}

// applyLocked 生成内核配置并交给辅助程序应用；调用方持有 a.mu。
// 返回 err 表示暂时性问题（稍后重试）；配置本身被拒绝时 err 为空、FailedRevision 非零。
func (a *Agent) applyLocked(ctx context.Context, cfg contracts.GateConfig) (contracts.GateState, error) {
	if err := a.ensureKernels(ctx, cfg); err != nil {
		return contracts.GateState{}, err
	}
	if err := a.ensureCerts(ctx, cfg); err != nil {
		return contracts.GateState{}, err
	}
	fresh := a.st.Config == nil || a.st.Config.Revision != cfg.Revision
	baseline := a.st.Baseline
	if fresh {
		a.st.resetBaseline(cfg)
	}
	exhausted := a.st.exhausted(cfg)
	out, err := kernelconf.Build(cfg, kernelconf.Options{CertDir: a.P.CertsDir, Now: time.Now(), Exhausted: exhausted})
	if err != nil {
		a.st.Baseline = baseline
		return contracts.GateState{AppliedRevision: a.st.Applied, FailedRevision: cfg.Revision, Error: driverLabel(cfg) + err.Error()}, nil
	}
	req := ApplyRequest{Kernels: map[string]string{}, Configs: map[string][]byte{"xray": out.Xray, "sing-box": out.Singbox}, StatsPorts: map[string]int{"xray": cfg.Ports.XrayStats, "sing-box": cfg.Ports.SingboxStats}, Args: out.Args, HotUsers: hotUsers(cfg), ForceRestart: a.forceRestart(cfg, exhausted), TraceID: cfg.TraceID}
	for name, k := range cfg.Kernels {
		req.Kernels[name] = k.Version
	}
	for _, in := range cfg.Inbounds {
		req.Ports = append(req.Ports, in.Port)
	}
	req.Ports = append(req.Ports, cfg.Ports.XrayStats, cfg.Ports.SingboxStats)
	if err := a.checkExecutionLease(ctx); err != nil {
		return contracts.GateState{}, err
	}
	applied := beginTaskStage(ctx, "kernel.apply", "gate.applyLocked")
	res, err := a.helper.Apply(ctx, req)
	stageErr := err
	if stageErr == nil && !res.OK {
		stageErr = errors.New("kernel_apply_rejected")
	}
	applied(stageErr)
	if err != nil {
		a.st.Baseline = baseline
		return contracts.GateState{}, err
	}
	if !res.OK {
		a.st.Baseline = baseline
		msg := res.Error
		if res.ConflictPort == 0 {
			msg = driverLabel(cfg) + msg
		}
		return contracts.GateState{AppliedRevision: a.st.Applied, FailedRevision: cfg.Revision, Error: msg, ConflictPort: res.ConflictPort}, nil
	}
	copy := cfg
	a.st.Config, a.st.Applied = &copy, cfg.Revision
	a.st.Signature = signature(cfg, time.Now(), exhausted)
	_ = a.st.save(a.statePath())
	report := &contracts.ApplyReport{Mode: res.Mode, Reason: res.Reason, PID: res.PID, Disconnects: res.Disconnects, Events: res.Events, Revision: cfg.Revision, TraceID: cfg.TraceID}
	return contracts.GateState{AppliedRevision: cfg.Revision, Apply: report}, nil
}

func (a *Agent) applyExecution(ctx context.Context, notice contracts.GateConfig) contracts.GateState {
	if a.st.Config == nil {
		return contracts.GateState{AppliedRevision: a.st.Applied, ExecutionSeq: notice.ExecutionSeq}
	}
	next := *a.st.Config
	next.Budgets = notice.Budgets
	next.ExecutionSeq = notice.ExecutionSeq
	next.ForceDisconnect = notice.ForceDisconnect
	next.ExecutionOnly = false
	baseline := a.st.Baseline
	a.st.advanceBaseline(next)
	exhausted := a.st.exhausted(next)
	if signature(next, time.Now(), exhausted) == a.st.Signature {
		a.st.Config = &next
		_ = a.st.save(a.statePath())
		return contracts.GateState{AppliedRevision: a.st.Applied, ExecutionSeq: notice.ExecutionSeq, Apply: &contracts.ApplyReport{Mode: contracts.ApplyUnchanged, Reason: "accounting"}}
	}
	state, err := a.applyLocked(ctx, next)
	state.ExecutionSeq = notice.ExecutionSeq
	if err != nil || state.FailedRevision != 0 {
		a.st.Baseline = baseline
	}
	if err != nil {
		state.Error = err.Error()
		state.FailedRevision = next.Revision
	}
	return state
}

func (a *Agent) appliedRevision() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.st.Applied
}

func (a *Agent) freshKernelEvents(events []contracts.KernelEvent) []contracts.KernelEvent {
	a.mu.Lock()
	since := a.reportedEventAt
	a.mu.Unlock()
	var fresh []contracts.KernelEvent
	for _, e := range events {
		if !since.IsZero() && !e.At.After(since) {
			continue
		}
		fresh = append(fresh, e)
	}
	if len(fresh) > 40 {
		fresh = fresh[len(fresh)-40:]
	}
	return fresh
}

func (a *Agent) noteReportedEvents(events []contracts.KernelEvent) {
	if len(events) == 0 {
		return
	}
	a.mu.Lock()
	if at := events[len(events)-1].At; at.After(a.reportedEventAt) {
		a.reportedEventAt = at
	}
	a.mu.Unlock()
}

func hotUsers(cfg contracts.GateConfig) map[string]bool {
	out := map[string]bool{}
	for name, text := range cfg.Drivers {
		d, err := kernelconf.ParseDriver(text)
		if err == nil && d.Has("handler_users") {
			out[name] = true
		}
	}
	return out
}

func (a *Agent) forceRestart(cfg contracts.GateConfig, exhausted map[string]bool) map[string]bool {
	if cfg.ForceDisconnect != nil && !*cfg.ForceDisconnect {
		return nil
	}
	if a.forced == nil {
		a.forced = map[string]bool{}
	}
	prev := map[string]bool{}
	if a.st.Config != nil {
		for _, c := range a.st.Config.Credentials {
			if c.Internal || c.Revoke != "" || !c.InWindow(time.Now()) {
				continue
			}
			prev[c.StatsKey] = true
		}
	}
	out := map[string]bool{}
	for _, c := range cfg.Credentials {
		if c.Internal || !prev[c.StatsKey] {
			continue
		}
		reason := c.Revoke
		if reason == "" && !c.InWindow(time.Now()) {
			reason = "expired"
		}
		if reason == "" && exhausted[c.UserKey] {
			reason = "exhausted"
		}
		if reason == "" {
			continue
		}
		key := c.StatsKey + ":" + reason
		if a.forced[key] {
			continue
		}
		a.forced[key] = true
		for _, in := range cfg.Inbounds {
			if in.ID == c.InboundID {
				out[in.Kernel] = true
			}
		}
	}
	return out
}

// readCounters 读取两个内核的按用户累计；某内核未运行时跳过。
func readCounters(ctx context.Context, cfg contracts.GateConfig) (map[string]Counter, bool) {
	raw, ok := map[string]Counter{}, true
	for name, spec := range map[string][2]string{"xray": {"xray", "xray.app.stats.command.StatsService"}, "sing-box": {"singbox", "v2ray.core.app.stats.command.StatsService"}} {
		port := cfg.Ports.XrayStats
		if name == "sing-box" {
			port = cfg.Ports.SingboxStats
		}
		if _, used := cfg.Kernels[name]; !used || port == 0 {
			continue
		}
		client, err := xray.NewGRPCStatsClientWithEngineAndService(ctx, fmt.Sprintf("127.0.0.1:%d", port), spec[0], spec[1])
		if err != nil {
			ok = false
			continue
		}
		stats, err := client.QueryAllStats(ctx)
		client.Close()
		if err != nil {
			ok = false
			continue
		}
		for key, s := range stats {
			raw[key] = Counter{Up: s.Uplink, Down: s.Downlink}
		}
	}
	return raw, ok
}

// meter 每 10 秒读取计数；用户集合因到期 / 额度变化时在本地重建配置；约每 30 秒上报中心。
func (a *Agent) meter(ctx context.Context) error {
	st, err := a.helper.Status(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.st.Config == nil {
		return nil
	}
	cfg := *a.st.Config
	raw, fresh := readCounters(ctx, cfg)
	restarted := false
	if err == nil {
		for name, pid := range st.Running {
			if prev, ok := a.pids[name]; ok && prev != pid {
				restarted = true
			}
			a.pids[name] = pid
		}
	}
	a.st.absorb(raw, restarted)
	exhausted := a.st.exhausted(cfg)
	if sig := signature(cfg, time.Now(), exhausted); sig != a.st.Signature {
		if state, err := a.applyLocked(ctx, cfg); err != nil || state.FailedRevision != 0 {
			log.Printf("local enforcement rebuild: %v %s", err, state.Error)
		}
	}
	a.st.Seq++
	if a.ReportEvery > 1 && a.st.Seq%a.ReportEvery != 0 && !restarted {
		return a.st.save(a.statePath())
	}
	rep := a.st.prepareMeter(fresh, a.meterProtocol.Load() == 2)
	if err := a.st.save(a.statePath()); err != nil {
		return err
	}
	var ack contracts.MeterAck
	_, err = a.center.call(ctx, "POST", "/agent/meter", rep, &ack)
	if err != nil {
		return err
	}
	if rep.Protocol == 2 {
		if err = a.st.acknowledgeMeter(ack); err != nil {
			return err
		}
		return a.st.save(a.statePath())
	}
	return nil
}

func (a *Agent) heartbeat(ctx context.Context) error {
	hb := contracts.Heartbeat{NodeTestVersion: 3, Version: buildinfo.Version, Arch: runtime.GOARCH, Kernels: map[string]string{}, Host: sampleHost()}
	hb.SelfUpdate = a.updateStatus()
	hb.Hostname, _ = os.Hostname()
	hb.Center = a.center.origin()
	panelCerts := map[int64]string{}
	a.mu.Lock()
	if a.st.Config != nil {
		for _, ref := range a.st.Config.Certs {
			if ref.Panel != "" {
				panelCerts[ref.ID] = ref.Panel
			}
		}
	}
	a.mu.Unlock()
	if st, certs, err := a.helper.PanelReport(ctx, panelCerts); err == nil {
		hb.Panel, hb.PanelCerts = &st, certs
	}
	if st, err := a.helper.Status(ctx); err == nil {
		if st.Host != nil {
			copied := *st.Host
			hb.Host = &copied
		}
		hb.ListeningPorts, hb.Firewall = st.Listening, st.Firewall
		hb.KernelEvents = a.freshKernelEvents(st.Events)
		for name := range st.Running {
			hb.Kernels[name] = st.Active[name]
		}
	}
	var reply contracts.HeartbeatReply
	if hb.Host != nil {
		health := contracts.HealthCheck{}
		if hb.Host.Health != nil {
			health = *hb.Host.Health
		}
		var ids []int64
		a.mu.Lock()
		if a.st.Config != nil {
			for _, in := range a.st.Config.Inbounds {
				if in.CertID > 0 {
					ids = append(ids, in.CertID)
				}
			}
		}
		a.mu.Unlock()
		cert := hoststats.ObserveCertificates(a.P.CertsDir, ids, time.Now().UTC())
		health.CertDays, health.CertID, health.CertExpiresAt, health.CertObservedAt = cert.CertDays, cert.CertID, cert.CertExpiresAt, cert.CertObservedAt
		hb.Host.Health = &health
	}
	now := time.Now()
	wire := hb
	if a.heartbeatProtocol.Load() == contracts.HeartbeatProtocol {
		var err error
		wire, err = a.heartbeats.prepare(hb, now, a.HeartbeatFullEvery, a.HostSampleEvery)
		if err != nil {
			return err
		}
	}
	path := "/agent/heartbeat"
	if wire.Protocol == contracts.HeartbeatProtocol {
		path += "/v2"
	}
	code, err := a.center.call(ctx, "POST", path, wire, &reply)
	sentV2 := err == nil && wire.Protocol == contracts.HeartbeatProtocol && code != 404
	if code == 404 && wire.Protocol == contracts.HeartbeatProtocol {
		a.heartbeatProtocol.Store(0)
		a.heartbeats.reset()
		_, err = a.center.call(ctx, "POST", "/agent/heartbeat", hb, &reply)
		sentV2 = false
	}
	if err != nil {
		return err
	}
	if sentV2 {
		a.noteReportedEvents(wire.KernelEvents)
	}
	if reply.HeartbeatProtocol == contracts.HeartbeatProtocol {
		a.heartbeatProtocol.Store(contracts.HeartbeatProtocol)
		a.heartbeats.acknowledge(wire, reply, now)
	}
	a.followCenter(ctx, reply.Release)
	if want := strings.TrimRight(reply.Center, "/"); want != "" && want != hb.Center && !loopbackCenter(hb.Center) {
		return a.switchCenter(ctx, want)
	}
	return nil
}

// centerClientFor 创建用来验证新地址的客户端（测试中替换以信任测试证书）。
var centerClientFor = newCenterClient

// switchRetry 是新地址认证失败后的重试间隔。
const switchRetry = 10 * time.Minute

// switchCenter 先用新地址完成一次设备认证（签名挑战），成功后由辅助程序改写设备配置，再改用新地址。
// 失败保持旧地址，10 分钟后再试；设备 ID 与私钥不变。
func (a *Agent) switchCenter(ctx context.Context, center string) error {
	a.mu.Lock()
	if time.Since(a.switchTried) < switchRetry {
		a.mu.Unlock()
		return nil
	}
	a.switchTried = time.Now()
	a.mu.Unlock()
	if validCenter(center) != nil || !strings.HasPrefix(center, "https://") {
		return fmt.Errorf("center switch to %q rejected: not https", center)
	}
	probe, err := centerClientFor(a.cfg)
	if err != nil {
		return err
	}
	probe.setOrigin(center)
	if _, err = probe.bearer(ctx); err != nil {
		return fmt.Errorf("center switch to %s: authentication failed: %w", center, err)
	}
	if err = a.helper.SetCenter(ctx, center); err != nil {
		return fmt.Errorf("center switch to %s: %w", center, err)
	}
	a.center.setOrigin(center)
	log.Printf("center switched to %s", center)
	return nil
}

// loopbackCenter 判断是否经本机回环连接中心（单机角色），这种连接不跟随公共地址切换。
func loopbackCenter(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return strings.EqualFold(u.Hostname(), "localhost") || ip != nil && ip.IsLoopback()
}

func (a *Agent) actionLoop(ctx context.Context) {
	for ctx.Err() == nil {
		var actions []contracts.Action
		if _, err := a.center.call(ctx, "GET", "/agent/actions", nil, &actions); err != nil {
			sleep(ctx, 5*time.Second)
			continue
		}
		for _, act := range actions {
			// 下载、上传、测速与自更新耗时长，各自在后台跑，不堵住后面的动作；其余动作按顺序执行。
			if asyncActions[act.Kind] {
				go a.finishAction(ctx, act)
			} else {
				a.finishAction(ctx, act)
			}
		}
	}
}

var asyncActions = map[string]bool{contracts.ActionReleaseReplace: true, contracts.ActionAssetFetch: true, contracts.ActionSourceProbe: true}

func (a *Agent) finishAction(ctx context.Context, act contracts.Action) {
	res := a.runAction(ctx, act)
	res.ID = act.ID
	if _, err := a.center.call(ctx, "POST", "/agent/actions/"+strconv.FormatInt(act.ID, 10), res, nil); err != nil {
		log.Printf("action %d result: %v", act.ID, err)
	}
}

var errUnknownAction = errors.New("action not allowed")

// driverLabel 是错误前缀「驱动 xray-26.3、sing-box-1.12：」，让控制台看出是哪个驱动生成的配置出了问题。
func driverLabel(cfg contracts.GateConfig) string {
	var names []string
	for _, k := range []string{"xray", "sing-box"} {
		if d, err := kernelconf.DriverFor(cfg, k); err == nil && d != nil {
			names = append(names, d.Name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "驱动 " + strings.Join(names, "、") + "："
}
