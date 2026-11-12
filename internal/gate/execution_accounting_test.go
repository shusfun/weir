package gate

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func TestExecutionAccountingKeepsUnreportedUsage(t *testing.T) {
	dir := t.TempDir()
	cfg := contracts.GateConfig{
		Revision: 7, ExecutionSeq: 1,
		Credentials: []contracts.Credential{{StatsKey: "c1", UserKey: "u1"}},
		Budgets:     map[string]contracts.GateBudget{"u1": {Allocated: 1000, Used: 400}},
	}
	st := loadState(filepath.Join(dir, "state.json"))
	st.Config, st.Applied = &cfg, cfg.Revision
	st.Totals["c1"] = Counter{Down: 400}
	st.resetBaseline(cfg)
	st.Signature = signature(cfg, time.Now(), st.exhausted(cfg))
	a := &Agent{P: Paths{Data: dir}, st: st}
	// 中心收到累计 700 时，本地已经读到 800；仍有 100 字节在途。
	a.st.Totals["c1"] = Counter{Down: 800}
	notice := contracts.GateConfig{ExecutionOnly: true, ExecutionSeq: 2, Budgets: map[string]contracts.GateBudget{"u1": {Allocated: 1000, Used: 700}}}
	state := a.applyExecution(context.Background(), notice)
	if state.Error != "" || state.FailedRevision != 0 || state.Apply == nil || state.Apply.Reason != "accounting" {
		t.Fatalf("accounting unnecessarily changed kernel users: %+v", state)
	}
	if a.st.Baseline["u1"] != 700 || a.st.exhausted(*a.st.Config)["u1"] {
		t.Fatalf("confirmed usage counted twice: %+v", a.st)
	}
	// 重复通知和重启都不能再次推进基线。
	a.applyExecution(context.Background(), notice)
	a.st = loadState(a.statePath())
	if a.st.Baseline["u1"] != 700 {
		t.Fatalf("baseline after replay/restart = %d", a.st.Baseline["u1"])
	}
	a.st.Totals["c1"] = Counter{Down: 999}
	if a.st.exhausted(*a.st.Config)["u1"] {
		t.Fatal("stopped before the quota")
	}
	a.st.Totals["c1"] = Counter{Down: 1000}
	if !a.st.exhausted(*a.st.Config)["u1"] {
		t.Fatal("unreported bytes escaped quota enforcement")
	}
}
