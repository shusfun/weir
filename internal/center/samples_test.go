package center

import (
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestHistoryRecordsKnownZeroAndLeavesGapsUnknown(t *testing.T) {
	_, admin, g := setupGate(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "h@example.test"}, 200)
	uid := int(u["id"].(float64))
	key := g.config(0).Credentials[0].StatsKey

	// 还没有任何上报：全部是未知（null），不是 0。
	points := mustJSON(t, admin, "GET", "/api/v1/admin/users/"+itoa(uid)+"/history?hours=1", nil, 200)["points"].([]any)
	if len(points) != 60 {
		t.Fatalf("points = %d", len(points))
	}
	for _, p := range points {
		if p.(map[string]any)["traffic"] != nil {
			t.Fatalf("no data must be unknown: %v", p)
		}
	}
	// 计数不可用的上报不记；可用的上报即使没有增量也记为 0，之后的增量累加。
	meter := func(seq int64, status string, down int64) {
		rep := contracts.MeterReport{Epoch: "e1", Seq: seq, Status: status, Usage: []contracts.MeterSample{{StatsKey: key, Down: down}}}
		if code, _ := g.call("POST", "/api/v1/agent/meter", rep); code != 200 {
			t.Fatalf("meter = %d", code)
		}
	}
	meter(1, "unavailable", 0)
	last := func(path string) map[string]any {
		ps := mustJSON(t, admin, "GET", path, nil, 200)["points"].([]any)
		return ps[len(ps)-1].(map[string]any)
	}
	if p := last("/api/v1/admin/users/" + itoa(uid) + "/history"); p["traffic"] != nil {
		t.Fatalf("unavailable counters must stay unknown: %v", p)
	}
	meter(2, "fresh", 0)
	if p := last("/api/v1/admin/users/" + itoa(uid) + "/history"); p["traffic"] != float64(0) {
		t.Fatalf("known zero = %v", p)
	}
	meter(3, "fresh", 5000)
	meter(4, "fresh", 8000)
	if p := last("/api/v1/admin/users/" + itoa(uid) + "/history?hours=24"); p["traffic"] != float64(8000) {
		t.Fatalf("user traffic = %v", p)
	}
	if p := last("/api/v1/admin/gates/" + itoa(int(g.id)) + "/history?hours=6"); p["traffic"] != float64(8000) {
		t.Fatalf("gate traffic = %v", p)
	}
	// 心跳的主机观测：读到的字段记下，读不到的保持未知。
	cpu, total, used := 12.5, uint64(4<<30), uint64(1<<30)
	heartbeat(t, g, contracts.Heartbeat{Host: &contracts.HostSample{CPUPercent: &cpu, MemTotal: &total, MemUsed: &used}})
	p := last("/api/v1/admin/gates/" + itoa(int(g.id)) + "/history")
	if p["cpu"] != 12.5 || p["mem_used"] != float64(1<<30) || p["disk_used"] != nil {
		t.Fatalf("host point = %v", p)
	}
}
