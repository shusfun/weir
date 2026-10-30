package center

import (
	"context"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/ratesample"
	"github.com/shusfun/weir/packages/contracts"
)

func ok() nodeHealth   { return nodeHealth{Status: "ok"} }
func down() nodeHealth { return nodeHealth{Status: "down"} }
func rate(bps float64) rateStat {
	if bps == 0 {
		return rateStat{}
	}
	return rateStat{P90: bps, Minutes: 10, From: "self"}
}

func autoKeys(in rankInput, r []rankedEntry) (auto []string, exploring string) {
	for _, e := range r {
		if e.Auto {
			auto = append(auto, in.keys[e.Index])
		}
		if e.Exploring {
			exploring = in.keys[e.Index]
		}
	}
	return
}

func TestRankEntriesByRate(t *testing.T) {
	// a 快、b 中、c 慢、d 没数据、e 不可用
	in := rankInput{keys: []string{"a", "b", "c", "d", "e"}, health: []nodeHealth{ok(), ok(), ok(), ok(), down()},
		rates: []rateStat{rate(9e6), rate(5e6), rate(1e6), rate(0), rate(20e6)}, prev: map[string]bool{}}

	// 放 3 个：前 2 个按速率，第 3 个是轮换位（没数据的 d）；不可用的 e 再快也不放
	r := rankEntries(in, contracts.AutoRanked, 3)
	auto, exp := autoKeys(in, r)
	if len(auto) != 3 || auto[0] != "a" || auto[1] != "b" || auto[2] != "d" || exp != "d" || r[4].Rank != 0 {
		t.Fatalf("ranked top3 = %v exploring %q ranks %+v", auto, exp, r)
	}
	if r[0].Rank != 1 || r[1].Rank != 2 || r[2].Rank != 3 || r[3].Rank != 4 {
		t.Fatalf("ranks = %+v", r)
	}
	// 放 1 个：不留轮换位，只放最快的
	if auto, exp := autoKeys(in, rankEntries(in, contracts.AutoRanked, 1)); len(auto) != 1 || auto[0] != "a" || exp != "" {
		t.Fatalf("top1 = %v %q", auto, exp)
	}
	// 客户端测延迟：全部可用的都放
	if auto, _ := autoKeys(in, rankEntries(in, contracts.AutoLatency, 3)); len(auto) != 4 {
		t.Fatalf("latency = %v", auto)
	}
	// 上一次在里面的 b 按 1.25 倍算：5e6×1.25 仍低于 9e6，a 照样第一；c 按 1.25 倍也追不上 b
	in.prev = map[string]bool{"b": true, "c": true}
	if auto, _ := autoKeys(in, rankEntries(in, contracts.AutoRanked, 3)); auto[0] != "a" || auto[1] != "b" {
		t.Fatalf("hysteresis = %v", auto)
	}
	// b 比 a 只慢一点（8e6 vs 9e6），上一次在里面：b 留在第一，不来回换
	in.rates[1] = rate(8e6)
	in.prev = map[string]bool{"b": true}
	if r := rankEntries(in, contracts.AutoRanked, 3); r[1].Rank != 1 {
		t.Fatalf("near tie must keep incumbent: %+v", r)
	}
}

func TestRankEntriesExploreRotationAndFill(t *testing.T) {
	in := rankInput{keys: []string{"a", "x", "y", "z"}, health: []nodeHealth{ok(), ok(), ok(), ok()},
		rates: []rateStat{rate(9e6), rate(0), rate(0), rate(0)}, prev: map[string]bool{}}
	// 有速率的只有 1 个，放 3 个：a + 轮换位 + 再用没数据的补 1 个；每次拉取轮到下一个
	var seen []string
	for turn := 0; turn < 3; turn++ {
		in.turn = turn
		auto, exp := autoKeys(in, rankEntries(in, contracts.AutoRanked, 3))
		if len(auto) != 3 || auto[0] != "a" {
			t.Fatalf("turn %d auto = %v", turn, auto)
		}
		seen = append(seen, exp)
	}
	if seen[0] == seen[1] || seen[1] == seen[2] || seen[0] == seen[2] {
		t.Fatalf("explore slot must rotate: %v", seen)
	}
	// 全都没有速率数据：放全部可用节点，交给客户端测延迟
	in.rates[0] = rate(0)
	if auto, exp := autoKeys(in, rankEntries(in, contracts.AutoRanked, 2)); len(auto) != 4 || exp != "" {
		t.Fatalf("no data = %v %q", auto, exp)
	}
}

func TestP90(t *testing.T) {
	v := []float64{}
	for i := 1; i <= 10; i++ {
		v = append(v, float64(i))
	}
	if p90(v) != 9 || p90([]float64{3}) != 3 || p90(nil) != 0 {
		t.Fatalf("p90 = %v", p90(v))
	}
}

// 速率来自 Gate 按凭据上报的计数：相邻两次的下载增量 / 间隔；内核重启（纪元变化）与间隔过长不算。
func TestMeterRecordsRate(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := context.Background()
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "ss2022", "skip_probe": true}, 200)
	nid := int(n["id"].(float64))
	created := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "carol@example.test", "password": "Passw0rd-carol", "all_nodes": true}, 200)
	uid := int(created["id"].(float64))
	cred := s.Store.Ent.Credential.Query().Where(credential.UserIDEQ(uid), credential.NodeIDEQ(nid)).OnlyX(ctx)
	report := func(seq int64, epoch string, down int64) {
		t.Helper()
		if code, body := g.call("POST", "/api/v1/agent/meter", contracts.MeterReport{Revision: 1, Epoch: epoch, Seq: seq, Status: "fresh",
			Usage: []contracts.MeterSample{{StatsKey: cred.StatsKey, Down: down}}}); code != 200 {
			t.Fatalf("meter = %d %s", code, body)
		}
	}
	report(1, "e1", 1000) // 第一次：没有上一次的时间，只记账
	if n := s.Store.Ent.RateSample.Query().CountX(ctx); n != 0 {
		t.Fatalf("first report must not record a rate: %d", n)
	}
	// 让上一次看起来是 10 秒前，再报 10 MB
	s.Store.Ent.Credential.UpdateOneID(cred.ID).SetMeterAt(time.Now().Add(-10 * time.Second)).ExecX(ctx)
	report(2, "e1", 1000+10<<20)
	rows := s.Store.Ent.RateSample.Query().Where(ratesample.UserIDEQ(uid), ratesample.NodeIDEQ(nid)).AllX(ctx)
	if len(rows) != 1 || rows[0].Bytes != 10<<20 || rows[0].Seconds < 9 || rows[0].Seconds > 12 {
		t.Fatalf("rate sample = %+v", rows)
	}
	// 内核重启（新纪元）：不算速率
	s.Store.Ent.Credential.UpdateOneID(cred.ID).SetMeterAt(time.Now().Add(-10 * time.Second)).ExecX(ctx)
	report(1, "e2", 5<<20)
	if b := s.Store.Ent.RateSample.Query().OnlyX(ctx).Bytes; b != 10<<20 {
		t.Fatalf("restart must not add rate bytes: %d", b)
	}
	// 断线很久后的第一次上报：不算速率
	s.Store.Ent.Credential.UpdateOneID(cred.ID).SetMeterAt(time.Now().Add(-time.Hour)).ExecX(ctx)
	report(2, "e2", 50<<20)
	if b := s.Store.Ent.RateSample.Query().OnlyX(ctx).Bytes; b != 10<<20 {
		t.Fatalf("long gap must not add rate bytes: %d", b)
	}
	own := s.userRates(ctx, uid)[pathKey(nid, 0)]
	all := s.nodeRates(ctx, []int{nid})[nid]
	if !own.known() || own.From != "self" || all.From != "all" || own.P90 < 800_000 || own.P90 > 1_200_000 {
		t.Fatalf("rates own=%+v all=%+v", own, all)
	}
	// 代理页的节点与排序带上速率
	carol := login(t, s, "carol@example.test", "Passw0rd-carol")
	v := mustJSON(t, carol, "GET", "/api/v1/me/proxy", nil, 200)
	r := v["ranking"].([]any)[0].(map[string]any)["rate"].(map[string]any)
	if r["from"] != "self" || r["minutes"].(float64) != 1 {
		t.Fatalf("proxy ranking rate = %v", r)
	}
}
