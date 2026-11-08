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

func TestRankEntriesByRate(t *testing.T) {
	in := rankInput{keys: []string{"slow", "fast", "unknown", "down"}, health: []nodeHealth{ok(), ok(), ok(), down()}, rates: []rateStat{rate(1e6), rate(9e6), rate(0), rate(20e6)}}
	r := rankEntries(in)
	if r[0].Rank != 2 || r[1].Rank != 1 || r[2].Rank != 3 || r[3].Rank != 0 {
		t.Fatal(r)
	}
	for i := range r {
		if r[i].Index != i {
			t.Fatal("排名改变输出顺序")
		}
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
	s.Store.Ent.RateSample.Update().SetAt(minuteOf(time.Now()).Add(-time.Minute)).ExecX(ctx)
	ownMap, err := s.userRates(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	allMap, err := s.nodeRates(ctx, []int{nid})
	if err != nil {
		t.Fatal(err)
	}
	own, all := ownMap[pathKey(nid, 0)], allMap[nid]
	if !own.known() || own.From != "self" || all.From != "all" || own.P90 < 800_000 || own.P90 > 1_200_000 {
		t.Fatalf("rates own=%+v all=%+v", own, all)
	}
	// 代理页的节点与排序带上速率
	carol := login(t, s, "carol@example.test", "Passw0rd-carol")
	v := mustJSON(t, carol, "GET", "/api/v1/proxy", nil, 200)
	r := v["ranking"].([]any)[0].(map[string]any)["rate"].(map[string]any)
	if r["from"] != "self" || r["minutes"].(float64) != 1 {
		t.Fatalf("proxy ranking rate = %v", r)
	}
}
