package center

import (
	"testing"

	"github.com/imzyb/MiGate/internal/store/ent"
)

func budgetRow(gid int, alloc, confirmed, used int64, rate float64) *ent.Budget {
	return &ent.Budget{GateID: gid, Allocated: alloc, Confirmed: confirmed, Used: used, Rate: rate}
}

// reserved 是各 Gate 实际可能被用掉的额度合计（已确认与新值取大者）。不变量：不超过剩余额度。
func reserved(plan map[int]int64, rows []*ent.Budget) int64 {
	total := int64(0)
	for _, r := range rows {
		v := plan[r.GateID]
		if r.Confirmed > v {
			v = r.Confirmed
		}
		if v-r.Used > 0 {
			total += v - r.Used
		}
	}
	return total
}

func TestPlanSingleGateGetsEverything(t *testing.T) {
	plan := planBudgets(10<<30, []*ent.Budget{budgetRow(1, 0, 0, 3<<30, 0)})
	if plan[1] != 13<<30 {
		t.Fatalf("plan = %d", plan[1])
	}
}

func TestPlanMultiGateChunksAndNeverOverAllocates(t *testing.T) {
	const gib = int64(1 << 30)
	rows := []*ent.Budget{budgetRow(1, 0, 0, 0, 0), budgetRow(2, 0, 0, 0, float64(5*gib)/86400)}
	plan := planBudgets(100*gib, rows)
	if plan[1] != minChunk {
		t.Fatalf("idle gate chunk = %d, want %d", plan[1], minChunk)
	}
	if plan[2] < 4*gib || plan[2] > 6*gib {
		t.Fatalf("busy gate chunk = %d, want about 5 GiB", plan[2])
	}
	// 只剩很少额度时，两台合计也不能超过剩余。
	plan = planBudgets(40<<20, rows)
	if got := reserved(plan, rows); got > 40<<20 {
		t.Fatalf("reserved %d > remaining", got)
	}
}

func TestPlanShrinkWaitsForConfirmation(t *testing.T) {
	const gib = int64(1 << 30)
	// 用户原来只在 Gate 1 上，整份 10 GiB 已确认；现在多了 Gate 2。
	rows := []*ent.Budget{budgetRow(1, 10*gib, 10*gib, 1*gib, 0), budgetRow(2, 0, 0, 0, 0)}
	plan := planBudgets(9*gib, rows)
	if plan[1] >= 10*gib {
		t.Fatalf("gate 1 must be cut back, got %d", plan[1])
	}
	// Gate 1 尚未确认下调，仍按 10 GiB 占用：Gate 2 这一轮拿不到额度，合计不超额。
	if plan[2] != 0 {
		t.Fatalf("gate 2 = %d before gate 1 confirmed the cut", plan[2])
	}
	if got := reserved(plan, rows); got > 9*gib {
		t.Fatalf("reserved %d > remaining", got)
	}
	// Gate 1 确认后再次计算：Gate 2 拿到一块。
	rows[0].Allocated, rows[0].Confirmed = plan[1], plan[1]
	plan = planBudgets(9*gib, rows)
	if plan[2] != minChunk {
		t.Fatalf("gate 2 after confirm = %d", plan[2])
	}
	if got := reserved(plan, rows); got > 9*gib {
		t.Fatalf("reserved %d > remaining", got)
	}
}

func TestPlanExhaustedUserGetsNothingMore(t *testing.T) {
	rows := []*ent.Budget{budgetRow(1, 5<<30, 5<<30, 5<<30, 0), budgetRow(2, 1<<30, 1<<30, 1<<30, 0)}
	plan := planBudgets(0, rows)
	if plan[1] != 5<<30 || plan[2] != 1<<30 {
		t.Fatalf("plan = %v", plan)
	}
}
