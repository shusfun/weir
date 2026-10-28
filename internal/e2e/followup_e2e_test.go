//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/budget"
)

// relay 是 Gate 与中心之间可断开的 TCP 中转：统计经过的字节（中心带宽），断开时拒绝新连接并切断已有连接（模拟中心断线）。
// 中心仍收到来自本机回环的连接，按「本机直连」处理，不需要 HTTPS。
type relay struct {
	ln    net.Listener
	bytes atomic.Int64
	down  atomic.Bool
	mu    sync.Mutex
	conns map[net.Conn]bool
}

func newRelay(t *testing.T, target string) *relay {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &relay{ln: ln, conns: map[net.Conn]bool{}}
	t.Cleanup(func() { ln.Close(); r.setDown(true) })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if r.down.Load() {
				c.Close()
				continue
			}
			go r.pipe(c, target)
		}
	}()
	return r
}

func (r *relay) url() string { return "http://" + r.ln.Addr().String() }

func (r *relay) track(c net.Conn, on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if on {
		r.conns[c] = true
	} else {
		delete(r.conns, c)
	}
}

func (r *relay) pipe(c net.Conn, target string) {
	up, err := net.Dial("tcp", target)
	if err != nil {
		c.Close()
		return
	}
	r.track(c, true)
	r.track(up, true)
	done := make(chan struct{}, 2)
	// 边转发边计数：Agent 的长轮询与保持连接不会很快关闭，关闭时才累加会漏算。
	cp := func(dst, src net.Conn) {
		io.Copy(countingWriter{dst, &r.bytes}, src)
		dst.Close()
		done <- struct{}{}
	}
	go cp(up, c)
	go cp(c, up)
	<-done
	<-done
	r.track(c, false)
	r.track(up, false)
}

type countingWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	return n, err
}

func (r *relay) setDown(v bool) {
	r.down.Store(v)
	if !v {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for c := range r.conns {
		c.Close()
	}
}

// slowTarget 按给定速度持续输出，直到客户端断开；rate 为 0 时不限速。
func slowTarget(t *testing.T, rate int) *httptest.Server {
	chunk := make([]byte, 32<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		size := len(chunk)
		if rate > 0 && rate < size*10 {
			size = rate / 10
		}
		for {
			if _, err := w.Write(chunk[:size]); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			if rate > 0 {
				time.Sleep(time.Duration(float64(time.Second) * float64(size) / float64(rate)))
			}
			select {
			case <-r.Context().Done():
				return
			default:
			}
		}
	}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	return srv
}

// puller 经一个本地 SOCKS / mixed 端口不停下载，记下收到的字节与最后一次收到数据的时间。
type puller struct {
	got  atomic.Int64
	last atomic.Int64
	stop context.CancelFunc
}

func pull(t *testing.T, port int, target string) *puller {
	ctx, cancel := context.WithCancel(context.Background())
	p := &puller{stop: cancel}
	p.last.Store(time.Now().UnixNano())
	t.Cleanup(cancel)
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
	go func() {
		buf := make([]byte, 64<<10)
		for ctx.Err() == nil {
			req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
			resp, err := hc.Do(req)
			if err != nil {
				time.Sleep(300 * time.Millisecond)
				continue
			}
			for {
				n, err := resp.Body.Read(buf)
				if n > 0 {
					p.got.Add(int64(n))
					p.last.Store(time.Now().UnixNano())
				}
				if err != nil {
					break
				}
			}
			resp.Body.Close()
		}
	}()
	return p
}

func (p *puller) idle() time.Duration { return time.Since(time.Unix(0, p.last.Load())) }

// subOutbound 用 sing-box 格式拉取用户的订阅，取出名为 tag 的出站。
func (e *env) subOutbound(uid int, tag string) map[string]any {
	e.t.Helper()
	var detail map[string]any
	b, _ := e.get("/api/v1/admin/users/"+strconv.Itoa(uid), nil)
	json.Unmarshal(b, &detail)
	token := detail["link_items"].([]any)[0].(map[string]any)["token"].(string)
	body, _ := e.get("/sub/"+token, map[string]string{"User-Agent": "sing-box/1.14.3"})
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	json.Unmarshal(body, &doc)
	for _, ob := range doc.Outbounds {
		if ob["tag"] == tag {
			return ob
		}
	}
	e.t.Fatalf("subscription has no outbound %q: %s", tag, body)
	return nil
}

// appliedGate 等这台 Gate 应用到中心最新的配置版本。
func (e *env) appliedGate(gid int, what string) {
	e.t.Helper()
	eventually(e.t, 40*time.Second, what, func() bool {
		g, err := e.s.Store.Ent.Gate.Get(e.ctx, gid)
		return err == nil && g.AppliedRevision == g.ConfigRevision && g.AppliedRevision > 0
	})
}

// SUB-03：客户端经 Gate 下载的大流量不经过中心；中心只有订阅与心跳 / 计量 / 配置的流量。
func TestCenterCarriesOnlyControlTraffic(t *testing.T) {
	e := setup(t)
	sb := e.seedSingbox()
	link := newRelay(t, e.url[len("http://"):])
	e.gateCenter = link.url()
	e.startGate("bw")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "ss2022", "name": "S", "skip_probe": true}, nil)
	u := e.post("/api/v1/admin/users", map[string]any{"email": "bw@e2e.test", "password": "E2eUser1"}, nil)
	e.applied("node for bandwidth test")
	port := e.runSingboxClient(sb, e.subOutbound(int(u["id"].(float64)), "S"))
	// 限速 10 MiB/s 下载 20 秒：这段时间里 Gate 照常经中转发心跳、计量（每秒一次）与长轮询配置。
	target := slowTarget(t, 10<<20)
	time.Sleep(2 * time.Second)
	before, seen := link.bytes.Load(), e.s.Store.Ent.Gate.GetX(e.ctx, e.gateID).LastSeenAt
	p := pull(t, port, target.URL)
	time.Sleep(20 * time.Second)
	p.stop()
	center := link.bytes.Load() - before
	after := e.s.Store.Ent.Gate.GetX(e.ctx, e.gateID).LastSeenAt
	if !after.After(*seen) || e.s.Store.Ent.User.GetX(e.ctx, int(u["id"].(float64))).Used < p.got.Load()/2 {
		t.Fatalf("heartbeat / metering did not go through the center during the download")
	}
	t.Logf("client received %d MiB via the gate in 20 s; the center link carried %d KiB (heartbeats, metering, config) in the same time", p.got.Load()>>20, center>>10)
	if link.bytes.Load() == 0 {
		t.Fatal("the gate did not reach the center through the counting relay")
	}
	if center > 2<<20 {
		t.Fatalf("center carried %d bytes while %d bytes went through the gate", center, p.got.Load())
	}
}

// QUOTA-01：两台 Gate 上的同一个用户（额度 200 MiB）同时持续下载，中途断开中心：合计不超额度，用完后两台都停。
// 再验「新增 Gate 时先收回、再分配」：原 Gate 下调确认之前，新 Gate 拿不到额度。
func TestTwoGatesQuotaWithCenterDown(t *testing.T) {
	e := setup(t)
	sb := e.seedSingbox()
	link := newRelay(t, e.url[len("http://"):])
	e.gateCenter = link.url()
	gids := map[string]int{}
	for _, name := range []string{"qa", "qb", "qc"} {
		e.startGate(name)
		gids[name] = e.gateID
		e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	}
	node := func(gate, name string) int {
		n := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": gids[gate], "preset": "ss2022", "name": name, "skip_probe": true}, nil)
		return int(n["id"].(float64))
	}
	na, nb := node("qa", "A"), node("qb", "B")
	const quota = 200 << 20
	u := e.post("/api/v1/admin/users", map[string]any{"email": "quota@e2e.test", "password": "E2eUser1", "quota": quota, "all_nodes": false, "node_ids": []int{na, nb}}, nil)
	uid := int(u["id"].(float64))
	e.appliedGate(gids["qa"], "gate A")
	e.appliedGate(gids["qb"], "gate B")

	// 每台 Gate 当前执行的额度（已确认的值与新值取大者，即 Gate 可能按它放行的上限）；合计不能超过额度。
	enforced := func() (map[int]int64, int64) {
		rows := e.s.Store.Ent.Budget.Query().Where(budget.UserIDEQ(uid)).AllX(e.ctx)
		out, sum := map[int]int64{}, int64(0)
		for _, r := range rows {
			v := max(r.Allocated, r.Confirmed)
			out[r.GateID] = v
			sum += v
		}
		return out, sum
	}
	var worst, overshoot atomic.Int64
	var worstRows atomic.Value
	watch, unwatch := context.WithCancel(context.Background())
	t.Cleanup(unwatch)
	go func() {
		for watch.Err() == nil {
			uq := e.s.Store.Ent.User.GetX(e.ctx, uid)
			rows := e.s.Store.Ent.Budget.Query().Where(budget.UserIDEQ(uid)).AllX(e.ctx)
			// 中心分配的额度合计不超过额度；Gate 自己超出已确认额度用掉的部分（一个计量周期内的流量）单独统计，不算分配超额。
			sum := int64(0)
			desc := ""
			for _, r := range rows {
				sum += max(r.Allocated, r.Confirmed)
				desc += fmt.Sprintf(" [gate %d alloc %d conf %d used %d rev %d]", r.GateID, r.Allocated, r.Confirmed, r.Used, r.Revision)
				if r.Used > r.Confirmed && r.Used-r.Confirmed > overshoot.Load() && r.Confirmed > 0 {
					overshoot.Store(r.Used - r.Confirmed)
				}
			}
			excess := sum - uq.Quota
			for _, r := range rows {
				if r.Used > r.Confirmed && r.Confirmed > 0 {
					excess -= r.Used - r.Confirmed
				}
			}
			if excess > worst.Load() {
				worst.Store(excess)
				worstRows.Store(fmt.Sprintf("user used %d, sum %d:%s", uq.Used, sum, desc))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	target := slowTarget(t, 3<<20)
	pa := pull(t, e.runSingboxClient(sb, e.subOutbound(uid, "A")), target.URL)
	pb := pull(t, e.runSingboxClient(sb, e.subOutbound(uid, "B")), target.URL)
	eventually(t, 60*time.Second, "60 MiB metered with the center up", func() bool { return e.s.Store.Ent.User.GetX(e.ctx, uid).Used >= 60<<20 })
	alloc, sum := enforced()
	t.Logf("center going down: used %d MiB, allocated A=%d MiB B=%d MiB (sum %d MiB of %d)", e.s.Store.Ent.User.GetX(e.ctx, uid).Used>>20,
		alloc[gids["qa"]]>>20, alloc[gids["qb"]]>>20, sum>>20, quota>>20)
	link.setDown(true)
	eventually(t, 120*time.Second, "both gates stop the user while the center is down", func() bool { return pa.idle() > 8*time.Second && pb.idle() > 8*time.Second })
	got := pa.got.Load() + pb.got.Load()
	t.Logf("offline: clients received A=%d MiB B=%d MiB, total %d MiB", pa.got.Load()>>20, pb.got.Load()>>20, got>>20)
	link.setDown(false)
	// 中心恢复后两台 Gate 补报用量；合计以 Gate 计量为准。
	eventually(t, 40*time.Second, "usage reported after the center is back", func() bool {
		rows := e.s.Store.Ent.Budget.Query().Where(budget.UserIDEQ(uid)).AllX(e.ctx)
		var used int64
		for _, r := range rows {
			used += r.Used
		}
		return used >= quota-8<<20
	})
	used := e.s.Store.Ent.User.GetX(e.ctx, uid).Used
	t.Logf("after reconnect: metered %d bytes (quota %d, over by %d KiB); allocation never exceeded the quota by more than %d bytes", used, quota, (used-quota)>>10, worst.Load())
	if worst.Load() > 0 {
		t.Fatalf("center allocations exceeded the quota by %d bytes: %v", worst.Load(), worstRows.Load())
	}
	// Gate 每秒检查一次用量、再重载内核：已知会多放行约一个计量周期的流量（这里 3 MiB/s）。验收清单记录实际超出量。
	t.Logf("largest per-gate overshoot beyond its confirmed allocation: %d KiB", overshoot.Load()>>10)
	if overshoot.Load() > 8<<20 || used > quota+16<<20 {
		t.Fatalf("overshoot %d / metered %d is more than one metering interval per gate", overshoot.Load(), used)
	}
	time.Sleep(3 * time.Second)
	if pa.idle() < 3*time.Second || pb.idle() < 3*time.Second {
		t.Fatalf("a gate resumed the exhausted user after the center came back (idle A=%v B=%v)", pa.idle(), pb.idle())
	}
	pa.stop()
	pb.stop()

	// 「约撑 24 小时」：多 Gate 时每块 = 最近速度 × 24 小时（最少 32 MiB，不超过剩余）。按实测速度核对计算。
	for _, r := range e.s.Store.Ent.Budget.Query().Where(budget.UserIDEQ(uid)).AllX(e.ctx) {
		t.Logf("gate %d: rate %.0f KiB/s → 24 h chunk %d MiB (capped by the remaining quota here)", r.GateID, r.Rate/1024, int64(r.Rate*86400)>>20)
	}

	// 先收回、再分配：新用户只在 Gate A 上，剩余额度整份在 A；再给他加 Gate C 上的节点。
	nc := node("qc", "C")
	u2 := e.post("/api/v1/admin/users", map[string]any{"email": "reclaim@e2e.test", "password": "E2eUser1", "quota": 1 << 30, "all_nodes": false, "node_ids": []int{na}}, nil)
	uid2 := int(u2["id"].(float64))
	e.appliedGate(gids["qa"], "gate A holds the whole quota")
	rowA := e.s.Store.Ent.Budget.Query().Where(budget.UserIDEQ(uid2), budget.GateIDEQ(gids["qa"])).OnlyX(e.ctx)
	if rowA.Allocated != 1<<30 || rowA.Confirmed != 1<<30 {
		t.Fatalf("single gate must hold the whole quota: %+v", rowA)
	}
	type snap struct{ aAlloc, aConf, cAlloc int64 }
	var seen []snap
	var mu sync.Mutex
	stopSnap := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopSnap:
				return
			default:
			}
			var s snap
			for _, r := range e.s.Store.Ent.Budget.Query().Where(budget.UserIDEQ(uid2)).AllX(e.ctx) {
				switch r.GateID {
				case gids["qa"]:
					s.aAlloc, s.aConf = r.Allocated, r.Confirmed
				case gids["qc"]:
					s.cAlloc = r.Allocated
				}
			}
			mu.Lock()
			seen = append(seen, s)
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
		}
	}()
	if code, v := call(t, e.admin, "PATCH", e.url+"/api/v1/admin/users/"+strconv.Itoa(uid2), map[string]any{"node_ids": []int{na, nc}}); code != 200 {
		t.Fatalf("add gate C = %d %v", code, v)
	}
	eventually(t, 60*time.Second, "gate C gets a chunk", func() bool {
		r, err := e.s.Store.Ent.Budget.Query().Where(budget.UserIDEQ(uid2), budget.GateIDEQ(gids["qc"])).Only(e.ctx)
		return err == nil && r.Allocated > 0 && r.Confirmed == r.Allocated
	})
	close(stopSnap)
	mu.Lock()
	defer mu.Unlock()
	shrunk := false
	for _, s := range seen {
		if s.cAlloc > 0 && s.aConf >= 1<<30 {
			t.Fatalf("gate C got %d before gate A confirmed its cut (A allocated %d confirmed %d)", s.cAlloc, s.aAlloc, s.aConf)
		}
		if max(s.aAlloc, s.aConf)+s.cAlloc > 1<<30 {
			t.Fatalf("reserved A %d + C %d > quota", max(s.aAlloc, s.aConf), s.cAlloc)
		}
		shrunk = shrunk || s.aAlloc < 1<<30
	}
	last := seen[len(seen)-1]
	if !shrunk {
		t.Fatal("gate A was never cut back")
	}
	t.Logf("reclaim: A cut to %d MiB and confirmed before C got %d MiB (%d samples)", last.aConf>>20, last.cAlloc>>20, len(seen))
}

// NODE-01：容器联网，建 REALITY 节点不填伪装目标：Gate 从内置列表实测选出合适站点，保存前真实连接测试通过；真实客户端经它访问外网。
func TestRealityTargetChosenByGateProbe(t *testing.T) {
	if c, err := net.DialTimeout("tcp", "www.microsoft.com:443", 5*time.Second); err != nil {
		t.Skipf("no internet in the container: %v", err)
	} else {
		c.Close()
	}
	e := setup(t)
	sb := e.seedSingbox()
	e.startGate("probe")
	e.s.Store.Ent.Gate.UpdateOneID(e.gateID).SetPublicHost("127.0.0.1").ExecX(e.ctx)
	probe := e.post(fmt.Sprintf("/api/v1/admin/gates/%d/probe-reality", e.gateID), map[string]any{}, nil)
	n := e.post("/api/v1/admin/nodes", map[string]any{"gate_id": e.gateID, "preset": "vless-reality", "name": "R"}, nil)
	st := n["settings"].(map[string]any)
	t.Logf("gate probe recommended %v; node uses %v", probe["best"], st["reality_dest"])
	if st["reality_dest"] == "" || st["reality_dest"] == nil {
		t.Fatalf("no target chosen: %v", n)
	}
	results, _ := probe["results"].([]any)
	chosen := st["reality_server_names"]
	ok := false
	for _, raw := range results {
		r := raw.(map[string]any)
		if r["host"] == chosen {
			ok = r["suitable"] == true && r["tls13"] == true && r["h2"] == true && r["cross_redirect"] != true
			t.Logf("chosen %s: %v", chosen, r)
		}
	}
	if !ok {
		t.Fatalf("chosen target %v is not a suitable probe result: %v", chosen, results)
	}
	u := e.post("/api/v1/admin/users", map[string]any{"email": "probe@e2e.test", "password": "E2eUser1"}, nil)
	e.applied("REALITY node with a probed target")
	port := e.runSingboxClient(sb, e.subOutbound(int(u["id"].(float64)), "R"))
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	hc := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
	eventually(t, 40*time.Second, "internet via the probed REALITY node", func() bool {
		resp, err := hc.Get("https://www.gstatic.com/generate_204")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 204
	})
	t.Logf("client reached the internet through REALITY with target %v", chosen)
}
