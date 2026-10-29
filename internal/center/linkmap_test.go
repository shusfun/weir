package center

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/geoip"
	"github.com/shusfun/weir/packages/contracts"
)

type fakeGeo map[string]geoip.Location

func (f fakeGeo) Lookup(ip string) (geoip.Location, bool) { l, ok := f[ip]; return l, ok }
func (f fakeGeo) Status() geoip.Status                    { return geoip.Status{Ready: true} }

func u64p(v uint64) *uint64 { return &v }

// 链路页：管理员看全部、用户只看自己的；管理员配的出口对用户只留类型与地区；
// 实时速率按相邻两次计数算，用户的出口速率只算自己的路径（出口计数里有别人的流量）。
func TestLinkMapScopesAndLiveRates(t *testing.T) {
	s, admin, g := setupGate(t)
	gid := int(g.id)
	s.GeoDB = fakeGeo{
		"203.0.113.50": {IP: "203.0.113.50", Lat: 22.3, Lon: 114.2, City: "香港", Country: "HK", Org: "HK DC"},
		"198.51.100.5": {IP: "198.51.100.5", Lat: 35.7, Lon: 139.7, City: "东京", Country: "JP", Org: "Home ISP"},
	}
	mustJSON(t, admin, "PATCH", "/api/v1/admin/gates/"+itoa(gid), map[string]any{"public_host": "203.0.113.50"}, 200)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gid, "preset": "vless-reality", "skip_probe": true,
		"chain": map[string]any{"kind": "socks5", "address": "198.51.100.5", "port": 1080}}, 200)
	nid := int(n["id"].(float64))
	a := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "a@example.test", "all_nodes": true}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "b@example.test", "all_nodes": true}, 200)
	alice := login(t, s, "a@example.test", a["password"].(string))

	full := mustJSON(t, admin, "GET", "/api/v1/admin/linkmap", nil, 200)
	if len(full["paths"].([]any)) != 2 || len(full["users"].([]any)) != 2 {
		t.Fatalf("admin map = %v", full)
	}
	mine := mustJSON(t, alice, "GET", "/api/v1/me/linkmap", nil, 200)
	paths := mine["paths"].([]any)
	if len(paths) != 1 || mine["users"] != nil || mine["admin"] != false {
		t.Fatalf("user map = %v", mine)
	}
	exit := mine["exits"].([]any)[0].(map[string]any)
	place := exit["place"].(map[string]any)
	if exit["kind"] != "socks5" || exit["address"] != nil || strings.Contains(exit["sub"].(string), "198.51.100.5") || place["known"] != true || place["city"] != "东京" || place["org"] != nil || place["ip"] != nil {
		t.Fatalf("admin exit must be redacted for users: %v", exit)
	}
	gate := mine["gates"].([]any)[0].(map[string]any)
	if gate["host"] != nil || gate["place"].(map[string]any)["city"] != "香港" {
		t.Fatalf("user gate = %v", gate)
	}

	// 两次上报之间 2 秒：每个用户的凭据各涨 2000 / 4000 字节，出口计数是两人之和。
	cfg := g.config(0)
	keys := map[int]string{}
	for _, c := range cfg.Credentials {
		if c.InboundID == int64(nid) && c.UserKey != "internal" {
			keys[len(keys)] = c.StatsKey
		}
	}
	t0 := time.Now().UTC()
	s.live.ingest(gid, contracts.LiveReport{At: t0, NetRx: u64p(0), NetTx: u64p(0), Users: []contracts.MeterSample{{StatsKey: keys[0]}, {StatsKey: keys[1]}}, Outbounds: []contracts.MeterSample{{StatsKey: "nodechain-" + itoa(nid)}}})
	s.live.ingest(gid, contracts.LiveReport{At: t0.Add(2 * time.Second), NetRx: u64p(20000), NetTx: u64p(10000),
		Users: []contracts.MeterSample{{StatsKey: keys[0], Down: 2000}, {StatsKey: keys[1], Down: 4000}}, Outbounds: []contracts.MeterSample{{StatsKey: "nodechain-" + itoa(nid), Down: 6000}}})
	ctx := context.Background()
	am, _ := s.buildLinkMap(ctx, 0)
	um, _ := s.buildLinkMap(ctx, int(a["id"].(float64)))
	frames := s.live.frames(gid, time.Time{})
	if len(frames) != 1 {
		t.Fatalf("frames = %d", len(frames))
	}
	ae, ue := am.eventFor(gid, frames[0]), um.eventFor(gid, frames[0])
	eid := "nodechain:" + itoa(nid)
	if ae.Exits[eid].Down != 3000 || ae.Net == nil || ae.Net.Down != 10000 || len(ae.Paths) != 2 {
		t.Fatalf("admin event = %+v", ae)
	}
	if ue.Net != nil || len(ue.Paths) != 1 || ue.Exits[eid].Down != 1000 && ue.Exits[eid].Down != 2000 {
		t.Fatalf("user event = %+v", ue)
	}
	// 计数回退（内核重启）：这一帧算不出来，不当成 0。
	s.live.ingest(gid, contracts.LiveReport{At: t0.Add(4 * time.Second), Users: []contracts.MeterSample{{StatsKey: keys[0], Down: 10}}})
	if f := s.live.frames(gid, frames[0].At); len(f) != 1 || len(f[0].Keys) != 0 {
		t.Fatalf("counter reset must be unknown: %+v", f)
	}

	// SSE：先推 hello，再推历史帧；有人看时让 Gate 开始实时上报。
	rctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("GET", "https://example.com/api/v1/me/linkmap/live", nil).WithContext(rctx)
	req.AddCookie(alice.cookie)
	w := httptest.NewRecorder()
	alice.router.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "event: hello") || !strings.Contains(body, "event: frame") || strings.Contains(body, `"net"`) {
		t.Fatalf("sse = %s", body)
	}
	if n, _ := s.Store.Ent.GateAction.Query().Count(ctx); n == 0 {
		t.Fatal("live.start not sent")
	}
}
