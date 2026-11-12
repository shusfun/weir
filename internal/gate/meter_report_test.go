package gate

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestMeterPendingReportSurvivesLossRestartAndOnlyAdvancesOnExactAck(t *testing.T) {
	st := State{Epoch: "epoch1", Seq: 10, Applied: 2, Totals: map[string]Counter{"a": {Up: 100}, "b": {Down: 200}}}
	first := st.prepareMeter(true, true)
	if first.Partial || first.Protocol != 2 || len(first.Usage) != 2 {
		t.Fatalf("first=%+v", first)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := st.save(path); err != nil {
		t.Fatal(err)
	}
	restored := loadState(path)
	restored.Totals["a"] = Counter{Up: 150}
	restored.Seq++
	got := restored.prepareMeter(true, true)
	one, _ := json.Marshal(first)
	two, _ := json.Marshal(got)
	if string(one) != string(two) {
		t.Fatal("unacknowledged packet changed after restart")
	}
	for _, ack := range []contracts.MeterAck{{Epoch: first.Epoch, Seq: 11, Accepted: true}, {Epoch: "epoch2", Seq: 10, Accepted: true}, {Epoch: first.Epoch, Seq: 10}} {
		if restored.acknowledgeMeter(ack) == nil {
			t.Fatal("wrong acknowledgement accepted")
		}
	}
	if err := restored.acknowledgeMeter(contracts.MeterAck{Epoch: first.Epoch, Seq: first.Seq, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	changed := restored.prepareMeter(true, true)
	if !changed.Partial || len(changed.Usage) != 1 || changed.Usage[0].StatsKey != "a" || changed.Usage[0].Up != 150 {
		t.Fatalf("changes=%+v", changed)
	}
	if err := restored.acknowledgeMeter(contracts.MeterAck{Epoch: changed.Epoch, Seq: changed.Seq, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	restored.Seq++
	empty := restored.prepareMeter(true, true)
	if len(empty.Usage) != 0 || !empty.Partial {
		t.Fatalf("idle=%+v", empty)
	}
	if err := restored.acknowledgeMeter(contracts.MeterAck{Epoch: empty.Epoch, Seq: empty.Seq, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	restored.Epoch = "epoch2"
	restored.Seq = 1
	newEpoch := restored.prepareMeter(true, true)
	if newEpoch.Partial || len(newEpoch.Usage) != 2 {
		t.Fatalf("new epoch lacks full baseline: %+v", newEpoch)
	}
}

func TestMeterRawCounterRestartAndRollbackRemainCumulative(t *testing.T) {
	st := State{Totals: map[string]Counter{}, Last: map[string]Counter{}}
	st.absorb(map[string]Counter{"a": {Up: 100, Down: 200}}, false)
	st.absorb(map[string]Counter{"a": {Up: 150, Down: 250}}, false)
	st.absorb(map[string]Counter{"a": {Up: 10, Down: 20}}, true)
	st.absorb(map[string]Counter{"a": {Up: 5, Down: 7}}, false)
	if got := st.Totals["a"]; got.Up != 165 || got.Down != 277 {
		t.Fatalf("restart usage=%+v", got)
	}
}

func TestMeterEmptyAcknowledgementSurvivesRestart(t *testing.T) {
	st := State{Epoch: "idle", Seq: 1, Totals: map[string]Counter{}}
	first := st.prepareMeter(true, true)
	if err := st.acknowledgeMeter(contracts.MeterAck{Epoch: first.Epoch, Seq: first.Seq, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := st.save(path); err != nil {
		t.Fatal(err)
	}
	// 空的确认表经 JSON omitempty 省略；随后第一笔增量仍须正常确认。
	st = loadState(path)
	st.Seq++
	st.Totals["first-user"] = Counter{Down: 42}
	report := st.prepareMeter(true, true)
	if !report.Partial || len(report.Usage) != 1 {
		t.Fatalf("first activity after restart = %+v", report)
	}
	if err := st.acknowledgeMeter(contracts.MeterAck{Epoch: report.Epoch, Seq: report.Seq, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	if st.Acknowledged["first-user"].Down != 42 {
		t.Fatalf("lost acknowledgement: %+v", st.Acknowledged)
	}
}
