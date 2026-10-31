package center

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// liveEvent 是推给页面的一帧（一台 Gate）：速率单位字节 / 秒；没有出现的路径或出口表示这一帧算不出来（未知）。
type liveEvent struct {
	Gate   int                 `json:"gate"`
	At     int64               `json:"at"` // 毫秒
	Net    *liveRate           `json:"net,omitempty"`
	Paths  map[string]liveRate `json:"paths"`
	Exits  map[string]liveRate `json:"exits"`
	Active map[string]int      `json:"active,omitempty"` // 一组里这一帧流量最大的成员（-1：这一帧都没有流量）
}

// eventFor 把一帧换算成页面上的对象。管理员的出口速率取内核的出口计数；
// 普通用户只看自己路径的速率之和（出口计数里有别人的流量）。
func (m *linkMap) eventFor(gid int, f liveFrame) liveEvent {
	ev := liveEvent{Gate: gid, At: f.At.UnixMilli(), Paths: map[string]liveRate{}, Exits: map[string]liveRate{}}
	pathExit := map[string]string{}
	for _, p := range m.Paths {
		pathExit[p.ID] = p.ExitID
	}
	for key, pid := range m.index.keys[gid] {
		if r, ok := f.Keys[key]; ok {
			cur := ev.Paths[pid]
			ev.Paths[pid] = liveRate{Up: cur.Up + r.Up, Down: cur.Down + r.Down}
		}
	}
	if !m.Admin {
		for pid, r := range ev.Paths {
			e := pathExit[pid]
			cur := ev.Exits[e]
			ev.Exits[e] = liveRate{Up: cur.Up + r.Up, Down: cur.Down + r.Down}
		}
		return ev
	}
	ev.Net = f.Net
	for tag, eid := range m.index.tags[gid] {
		if r, ok := f.Outbounds[tag]; ok {
			cur := ev.Exits[eid]
			ev.Exits[eid] = liveRate{Up: cur.Up + r.Up, Down: cur.Down + r.Down}
		}
	}
	for eid, tags := range m.index.members[gid] {
		best, bestRate, known := -1, 0.0, false
		var sum liveRate
		for i, tag := range tags {
			r, ok := f.Outbounds[tag]
			if !ok {
				continue
			}
			known = true
			sum.Up, sum.Down = sum.Up+r.Up, sum.Down+r.Down
			if r.total() > bestRate {
				best, bestRate = i, r.total()
			}
		}
		if !known {
			continue
		}
		if ev.Active == nil {
			ev.Active = map[string]int{}
		}
		ev.Active[eid] = best
		if _, ok := ev.Exits[eid]; !ok {
			ev.Exits[eid] = sum // 组本身没有计数时用成员之和
		}
	}
	return ev
}

// linkMapLive 用 SSE 推实时速率：先推每台 Gate 最近 5 分钟的历史，之后有新帧就推；结构变了推 topology。
// 一次连接最多 2 分钟，浏览器自动重连（带 Last-Event-ID，只补发之后的帧），也让中心升级、重启时不被长连接拖住。
func (s *Server) linkMapLive(c *gin.Context, admin bool) {
	ctx := c.Request.Context()
	viewer := 0
	if !admin {
		viewer = c.GetInt("user_id")
	}
	m, err := s.buildLinkMap(ctx, viewer)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	since := time.Time{}
	if v := c.GetHeader("Last-Event-ID"); v != "" {
		var ms int64
		if _, err := fmt.Sscan(v, &ms); err == nil {
			since = time.UnixMilli(ms)
		}
	}
	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	rc := http.NewResponseController(w)
	send := func(event string, id int64, v any) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
		b, _ := json.Marshal(v)
		if id > 0 {
			fmt.Fprintf(w, "id: %d\n", id)
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send("hello", 0, gin.H{"version": m.Version, "stale_ms": liveStale.Milliseconds(), "every_ms": 2000}) {
		return
	}
	s.ensureLive(ctx, m.index.gates)
	flush := func() bool {
		latest := since
		for _, gid := range m.index.gates {
			for _, f := range s.live.frames(gid, since) {
				if !send("frame", f.At.UnixMilli(), m.eventFor(gid, f)) {
					return false
				}
				if f.At.After(latest) {
					latest = f.At
				}
			}
		}
		since = latest
		return true
	}
	if !flush() {
		return
	}
	end := time.After(2 * time.Minute)
	renew := time.NewTicker(liveRenew)
	defer renew.Stop()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		wait := s.live.wait()
		select {
		case <-ctx.Done():
			return
		case <-end:
			return
		case <-s.events.stopped():
			return
		case <-ping.C:
			if !send("ping", 0, gin.H{}) {
				return
			}
		case <-renew.C:
			next, err := s.buildLinkMap(ctx, viewer)
			if err == nil {
				if next.Version != m.Version && !send("topology", 0, gin.H{"version": next.Version}) {
					return
				}
				m = next
			}
			s.ensureLive(ctx, m.index.gates)
		case <-wait:
			if !flush() {
				return
			}
		}
	}
}
