package gate

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func TestRuleMeterIndependentEngineRestartAndPending(t *testing.T) {
	st := State{Epoch: "a", Seq: 1}
	now := time.Now().UTC()
	st.absorbRules("xray", map[int64]Counter{1: {Down: 100}}, false, now)
	st.absorbRules("sing-box", map[int64]Counter{1: {Down: 200}}, false, now)
	st.absorbRules("xray", map[int64]Counter{1: {Down: 5}}, true, now.Add(time.Second))
	st.absorbRules("sing-box", map[int64]Counter{1: {Down: 230}}, false, now.Add(time.Second))
	rep := st.prepareMeter(true)
	if len(rep.Rules) != 2 || rep.Rules[0].Down != 230 || rep.Rules[1].Down != 105 || len(rep.Usage) != 0 {
		t.Fatalf("engine baseline mixed: %+v", rep)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := st.save(path); err != nil {
		t.Fatal(err)
	}
	st = loadState(path)
	st.Seq++
	st.absorbRules("xray", map[int64]Counter{1: {Down: 10}}, false, now.Add(2*time.Second))
	if got := st.prepareMeter(true); got.Rules[1].Down != 105 || got.Seq != rep.Seq {
		t.Fatal("pending report mutated")
	}
	if err := st.acknowledgeMeter(contracts.MeterAck{Epoch: rep.Epoch, Seq: rep.Seq, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	st.markRulesUnavailable(map[string]map[string]int64{"xray": {"tag": 1}, "sing-box": {"tag": 1}})
	st.absorbRules("sing-box", map[int64]Counter{1: {Down: 250}}, false, now.Add(3*time.Second))
	next := st.prepareMeter(false)
	if next.Rules[0].Down != 250 || next.Rules[0].Status != "fresh" || next.Rules[1].Down != 110 || next.Rules[1].Status != "unavailable" {
		t.Fatalf("coverage/rollup: %+v", next)
	}
}
