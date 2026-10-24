package gate

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/xray"
	"github.com/shusfun/weir/packages/contracts"
)

// Agent 以 migate-gate 身份运行：与中心通信、计量、本地执行额度与到期，经辅助程序应用配置。
type Agent struct {
	P      Paths
	cfg    Config
	center *centerClient
	helper *helperClient
	mu     sync.Mutex // 保护 st 与应用过程
	st     State
	pids   map[string]int
	// 计量与心跳周期；ReportEvery 为每隔几次计量上报一次中心。测试中可调短。
	MeterEvery     time.Duration
	HeartbeatEvery time.Duration
	ReportEvery    int64
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
		MeterEvery: 10 * time.Second, HeartbeatEvery: 30 * time.Second, ReportEvery: 3}, nil
}

func (a *Agent) statePath() string { return filepath.Join(a.P.Data, "state.json") }

// Run 启动时先按本地已生效配置重建（中心不可达也能继续服务），然后并行运行各循环。
func (a *Agent) Run(ctx context.Context) error {
	a.mu.Lock()
	if a.st.Config != nil {
		if _, err := a.applyLocked(ctx, *a.st.Config); err != nil {
			log.Printf("reapply last config: %v", err)
		}
	}
	_ = a.st.save(a.statePath())
	a.mu.Unlock()
	go a.loop(ctx, a.HeartbeatEvery, a.heartbeat)
	go a.loop(ctx, a.MeterEvery, a.meter)
	go a.actionLoop(ctx)
	a.configLoop(ctx)
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
		a.mu.Unlock()
		var cfg contracts.GateConfig
		code, err := a.center.call(ctx, "GET", "/agent/config?since="+strconv.FormatInt(since, 10), nil, &cfg)
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
		}
		if cfg.Version != "" && cfg.Version != buildinfo.Version && a.selfUpdate(ctx, cfg.Version) {
			sleep(ctx, time.Minute) // 辅助程序会重启本服务
			continue
		}
		a.mu.Lock()
		state, err := a.applyLocked(ctx, cfg)
		a.mu.Unlock()
		if err != nil {
			log.Printf("apply revision %d: %v", cfg.Revision, err)
			sleep(ctx, 10*time.Second)
			continue
		}
		if _, err = a.center.call(ctx, "POST", "/agent/state", state, nil); err != nil {
			log.Printf("report state: %v", err)
		}
		if state.FailedRevision != 0 {
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

// selfUpdate 从中心下载同版本程序并交给辅助程序替换；中心不提供（平台不同）时返回 false。
func (a *Agent) selfUpdate(ctx context.Context, version string) bool {
	data, sum, err := a.center.download(ctx, "/agent/release/"+version, "X-Release-SHA256")
	if err != nil || sumOf(data) != sum {
		log.Printf("self update to %s unavailable: %v", version, err)
		return false
	}
	if err = a.helper.Release(ctx, version, sum, data); err != nil {
		log.Printf("self update to %s failed: %v", version, err)
		return false
	}
	return true
}

// ensureKernels 下载并安装配置要求、本机还没有的内核版本（摘要必须与中心下发的一致）。
func (a *Agent) ensureKernels(ctx context.Context, cfg contracts.GateConfig) error {
	st, err := a.helper.Status(ctx)
	if err != nil {
		return err
	}
	for name, k := range cfg.Kernels {
		have := false
		for _, v := range st.Installed[name] {
			have = have || v == k.Version
		}
		if have {
			continue
		}
		data, _, err := a.center.download(ctx, "/agent/kernels/"+name+"/"+k.Version, "X-Kernel-SHA256")
		if err != nil {
			return fmt.Errorf("download %s %s: %w", name, k.Version, err)
		}
		if sumOf(data) != k.SHA256 {
			return fmt.Errorf("%s %s checksum mismatch", name, k.Version)
		}
		if err = a.helper.InstallKernel(ctx, name, k.Version, k.SHA256, data); err != nil {
			return err
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
		return contracts.GateState{AppliedRevision: a.st.Applied, FailedRevision: cfg.Revision, Error: err.Error()}, nil
	}
	req := ApplyRequest{Kernels: map[string]string{}, Configs: map[string][]byte{"xray": out.Xray, "sing-box": out.Singbox}, StatsPorts: map[string]int{"xray": cfg.Ports.XrayStats, "sing-box": cfg.Ports.SingboxStats}}
	for name, k := range cfg.Kernels {
		req.Kernels[name] = k.Version
	}
	for _, in := range cfg.Inbounds {
		req.Ports = append(req.Ports, in.Port)
	}
	req.Ports = append(req.Ports, cfg.Ports.XrayStats, cfg.Ports.SingboxStats)
	res, err := a.helper.Apply(ctx, req)
	if err != nil {
		a.st.Baseline = baseline
		return contracts.GateState{}, err
	}
	if !res.OK {
		a.st.Baseline = baseline
		return contracts.GateState{AppliedRevision: a.st.Applied, FailedRevision: cfg.Revision, Error: res.Error, ConflictPort: res.ConflictPort}, nil
	}
	copy := cfg
	a.st.Config, a.st.Applied = &copy, cfg.Revision
	a.st.Signature = signature(cfg, time.Now(), exhausted)
	_ = a.st.save(a.statePath())
	return contracts.GateState{AppliedRevision: cfg.Revision}, nil
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
	rep := contracts.MeterReport{Revision: a.st.Applied, Epoch: a.st.Epoch, Seq: a.st.Seq, Status: "fresh"}
	if !fresh {
		rep.Status = "unavailable"
	}
	for key, t := range a.st.Totals {
		rep.Usage = append(rep.Usage, contracts.MeterSample{StatsKey: key, Up: t.Up, Down: t.Down})
	}
	if err := a.st.save(a.statePath()); err != nil {
		return err
	}
	_, err = a.center.call(ctx, "POST", "/agent/meter", rep, nil)
	return err
}

func (a *Agent) heartbeat(ctx context.Context) error {
	hb := contracts.Heartbeat{Version: buildinfo.Version, Arch: runtime.GOARCH, Kernels: map[string]string{}, Host: sampleHost()}
	hb.Hostname, _ = os.Hostname()
	if st, err := a.helper.Status(ctx); err == nil {
		hb.ListeningPorts = st.Listening
		for name := range st.Running {
			hb.Kernels[name] = st.Active[name]
		}
	}
	_, err := a.center.call(ctx, "POST", "/agent/heartbeat", hb, nil)
	return err
}

func (a *Agent) actionLoop(ctx context.Context) {
	for ctx.Err() == nil {
		var actions []contracts.Action
		if _, err := a.center.call(ctx, "GET", "/agent/actions", nil, &actions); err != nil {
			sleep(ctx, 5*time.Second)
			continue
		}
		for _, act := range actions {
			res := a.runAction(ctx, act)
			res.ID = act.ID
			if _, err := a.center.call(ctx, "POST", "/agent/actions/"+strconv.FormatInt(act.ID, 10), res, nil); err != nil {
				log.Printf("action %d result: %v", act.ID, err)
			}
		}
	}
}

var errUnknownAction = errors.New("action not allowed")
