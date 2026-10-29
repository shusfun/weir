package gate

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/xray"
	"github.com/shusfun/weir/packages/contracts"
)

// 实时上报：有人打开链路页时中心发 live.start，Gate 在期限内每 2 秒读一次两个内核的按凭据、按出口累计计数与网卡收发，
// 直接发给中心（中心只放内存，算出速率推给页面）。期限到了自动停；中心每 30 秒续一次。计量与额度仍走 meter。

const (
	liveEvery   = 2 * time.Second
	liveMaxSpan = 2 * time.Minute
)

// startLive 延长实时上报的期限；没有在跑时启动一个循环。
func (a *Agent) startLive(seconds int) {
	if seconds <= 0 || time.Duration(seconds)*time.Second > liveMaxSpan {
		seconds = int(liveMaxSpan / time.Second)
	}
	until := time.Now().Add(time.Duration(seconds) * time.Second)
	a.liveMu.Lock()
	defer a.liveMu.Unlock()
	if until.After(a.liveUntil) {
		a.liveUntil = until
	}
	if a.liveRunning {
		return
	}
	a.liveRunning = true
	go a.liveLoop(a.runCtx)
}

func (a *Agent) liveLoop(ctx context.Context) {
	defer func() {
		a.liveMu.Lock()
		a.liveRunning = false
		a.liveMu.Unlock()
	}()
	for ctx.Err() == nil {
		a.liveMu.Lock()
		done := time.Now().After(a.liveUntil)
		a.liveMu.Unlock()
		if done {
			return
		}
		if err := a.liveOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("live: %v", err)
		}
		sleep(ctx, liveEvery)
	}
}

func (a *Agent) liveOnce(ctx context.Context) error {
	a.mu.Lock()
	var cfg contracts.GateConfig
	if a.st.Config != nil {
		cfg = *a.st.Config
	}
	a.mu.Unlock()
	rep := contracts.LiveReport{At: time.Now().UTC()}
	rep.NetRx, rep.NetTx = netCounters()
	rep.Users, rep.Outbounds = readLiveCounters(ctx, cfg, kernelconf.OutboundNames(cfg))
	_, err := a.center.call(ctx, "POST", "/agent/live", rep, nil)
	return err
}

// readLiveCounters 读取两个内核的按凭据与按出口累计；出口换算成 GateConfig 的标签（names），两个内核的同名出口相加。
// 某个内核读不到时它的计数缺席，中心据此显示「未知」而不是 0。
func readLiveCounters(ctx context.Context, cfg contracts.GateConfig, names map[string]string) (users, outbounds []contracts.MeterSample) {
	outs := map[string]*contracts.MeterSample{}
	defer func() {
		for _, o := range outs {
			outbounds = append(outbounds, *o)
		}
	}()
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
			continue
		}
		stats, err := client.QueryTrafficStats(ctx)
		client.Close()
		if err != nil {
			continue
		}
		for _, st := range stats {
			sample := contracts.MeterSample{StatsKey: st.ScopeKey, Up: st.Uplink, Down: st.Downlink}
			switch st.ScopeType {
			case "client":
				users = append(users, sample)
			case "outbound":
				tag, ok := names[st.ScopeKey]
				if !ok {
					continue
				}
				if o := outs[tag]; o != nil {
					o.Up, o.Down = o.Up+sample.Up, o.Down+sample.Down
				} else {
					sample.StatsKey = tag
					outs[tag] = &sample
				}
			}
		}
	}
	return users, outbounds
}
