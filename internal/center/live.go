package center

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/packages/contracts"
)

// 链路页的实时数据：只在中心内存里（单个中心进程），不写数据库。
// 有人在看时才让 Gate 每 2 秒上报（live.start，期限 60 秒，看的人还在就每 30 秒续一次）；
// 中心按相邻两次计数算速率，每台 Gate 留最近 5 分钟，新打开页面先拿到这段历史。
// 计数回退（内核重启）或间隔异常时那一项算不出速率，页面显示「未知」，不当成 0。

const (
	liveKeep    = 150 // 2 秒一帧，约 5 分钟
	liveLease   = 60  // live.start 的期限（秒）
	liveRenew   = 30 * time.Second
	liveStale   = 10 * time.Second // 超过这么久没有新帧，页面显示「数据中断」
	liveMaxSpan = 30 * time.Second // 两帧间隔超过它不算速率
)

// liveFrame 是某台 Gate 一帧的速率（字节 / 秒）；没有的键表示这一项算不出来（未知）。
type liveFrame struct {
	At        time.Time           `json:"at"`
	Net       *liveRate           `json:"net,omitempty"`
	Keys      map[string]liveRate `json:"-"`
	Outbounds map[string]liveRate `json:"-"`
}

type liveRate struct {
	Up   float64 `json:"up"`
	Down float64 `json:"down"`
}

func (r liveRate) total() float64 { return r.Up + r.Down }

type liveGate struct {
	frames  []liveFrame
	last    *contracts.LiveReport
	started time.Time // 上一次发 live.start 的时间
}

type liveHub struct {
	mu     sync.Mutex
	gates  map[int]*liveGate
	notify chan struct{} // 有新帧时关闭并换一个新的，唤醒推送
}

func (h *liveHub) gate(id int) *liveGate {
	if h.gates == nil {
		h.gates = map[int]*liveGate{}
	}
	g := h.gates[id]
	if g == nil {
		g = &liveGate{}
		h.gates[id] = g
	}
	return g
}

// wait 返回下一次有新帧时会关闭的通道。
func (h *liveHub) wait() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.notify == nil {
		h.notify = make(chan struct{})
	}
	return h.notify
}

func rateOf(prev, cur contracts.MeterSample, secs float64) (liveRate, bool) {
	if cur.Up < prev.Up || cur.Down < prev.Down {
		return liveRate{}, false
	}
	return liveRate{Up: float64(cur.Up-prev.Up) / secs, Down: float64(cur.Down-prev.Down) / secs}, true
}

// ingest 收下一次上报，和上一次相比算出速率。
func (h *liveHub) ingest(gid int, rep contracts.LiveReport) {
	h.mu.Lock()
	defer h.mu.Unlock()
	g := h.gate(gid)
	prev := g.last
	g.last = &rep
	if prev == nil {
		return
	}
	secs := rep.At.Sub(prev.At).Seconds()
	if secs <= 0 || time.Duration(secs*float64(time.Second)) > liveMaxSpan {
		return
	}
	// 间隔按 Gate 的时钟算（两次上报之间一致），帧的时间用中心收到的时间（页面据此判断是否中断）。
	f := liveFrame{At: time.Now().UTC(), Keys: map[string]liveRate{}, Outbounds: map[string]liveRate{}}
	if rep.NetRx != nil && rep.NetTx != nil && prev.NetRx != nil && prev.NetTx != nil && *rep.NetRx >= *prev.NetRx && *rep.NetTx >= *prev.NetTx {
		f.Net = &liveRate{Up: float64(*rep.NetTx-*prev.NetTx) / secs, Down: float64(*rep.NetRx-*prev.NetRx) / secs}
	}
	index := func(list []contracts.MeterSample) map[string]contracts.MeterSample {
		m := make(map[string]contracts.MeterSample, len(list))
		for _, x := range list {
			m[x.StatsKey] = x
		}
		return m
	}
	before := index(prev.Users)
	for _, cur := range rep.Users {
		if p, ok := before[cur.StatsKey]; ok {
			if r, ok := rateOf(p, cur, secs); ok {
				f.Keys[cur.StatsKey] = r
			}
		}
	}
	before = index(prev.Outbounds)
	for _, cur := range rep.Outbounds {
		if p, ok := before[cur.StatsKey]; ok {
			if r, ok := rateOf(p, cur, secs); ok {
				f.Outbounds[cur.StatsKey] = r
			}
		}
	}
	g.frames = append(g.frames, f)
	if len(g.frames) > liveKeep {
		g.frames = g.frames[len(g.frames)-liveKeep:]
	}
	if h.notify != nil {
		close(h.notify)
	}
	h.notify = make(chan struct{})
}

// frames 返回某台 Gate 在 since 之后的帧（since 为零时返回全部历史）。
func (h *liveHub) frames(gid int, since time.Time) []liveFrame {
	h.mu.Lock()
	defer h.mu.Unlock()
	g := h.gates[gid]
	if g == nil {
		return nil
	}
	var out []liveFrame
	for _, f := range g.frames {
		if f.At.After(since) {
			out = append(out, f)
		}
	}
	return out
}

// ensureLive 让这些 Gate 保持实时上报：距上次发 live.start 超过 30 秒就再发一次（期限 60 秒）。
func (s *Server) ensureLive(ctx context.Context, gids []int) {
	now := time.Now()
	var due []int
	s.live.mu.Lock()
	for _, id := range gids {
		g := s.live.gate(id)
		if now.Sub(g.started) >= liveRenew {
			g.started = now
			due = append(due, id)
		}
	}
	s.live.mu.Unlock()
	for _, id := range due {
		_, _ = s.enqueueAction(ctx, id, contracts.ActionLiveStart, map[string]any{"seconds": liveLease}, time.Duration(liveLease)*time.Second)
	}
}

// agentLive 收 Gate 的实时上报。
func (s *Server) agentLive(c *gin.Context) {
	var rep contracts.LiveReport
	if !bind(c, &rep) {
		return
	}
	if rep.At.IsZero() || len(rep.Users) > 50000 || len(rep.Outbounds) > 5000 {
		failure(c, 400, "invalid_live_report")
		return
	}
	s.live.ingest(gateFrom(c).ID, rep)
	c.Status(204)
}
