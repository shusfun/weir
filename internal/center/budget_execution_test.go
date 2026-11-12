package center

import (
	"context"
	"testing"

	"github.com/shusfun/weir/internal/store/ent"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
)

func TestBudgetExecutionReclaimAndUnlimitedTransitions(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	u := s.Store.Ent.User.Create().SetEmail("budget-execution@test.local").SetPasswordHash("unused").SetQuota(200 << 20).SaveX(ctx)
	var rows []*ent.Budget
	for i := 0; i < 2; i++ {
		g := pairFakeGate(t, admin)
		exit := s.Store.Ent.Exit.Query().Where(entexit.GateIDEQ(int(g.id)), entexit.TagEQ("direct")).OnlyX(ctx)
		n := s.Store.Ent.Node.Create().SetGateID(int(g.id)).SetExitID(exit.ID).SetName("budget").SetKernel("sing-box").SetProtocol("shadowsocks").SetPort(30000 + i).SaveX(ctx)
		s.Store.Ent.Credential.Create().SetNodeID(n.ID).SetUserID(u.ID).SetStatsKey("budget-" + itoa(i)).SaveX(ctx)
		alloc := int64(0)
		if i == 0 {
			alloc = 200 << 20
		}
		rows = append(rows, s.Store.Ent.Budget.Create().SetGateID(int(g.id)).SetUserID(u.ID).SetAllocated(alloc).SetConfirmed(alloc).SaveX(ctx))
	}
	if err := s.Rebalance(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if r := s.Store.Ent.Budget.GetX(ctx, rows[1].ID); r.Allocated != 0 {
		t.Fatal("new gate received unconfirmed reclaimed budget")
	}
	execution, err := s.loadExecution(ctx, rows[0].GateID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.confirmExecution(ctx, rows[0].GateID, execution.Seq); err != nil {
		t.Fatal(err)
	}
	if r := s.Store.Ent.Budget.GetX(ctx, rows[1].ID); r.Allocated == 0 {
		t.Fatal("confirmed reclaim did not wake allocation")
	}
	for _, unlimited := range []bool{true, false} {
		u.Update().SetUnlimited(unlimited).ExecX(ctx)
		if err = s.Rebalance(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			execution, err = s.loadExecution(ctx, row.GateID)
			if err != nil {
				t.Fatal(err)
			}
			if execution.Budgets[userKey(u.ID)].Unlimited != unlimited {
				t.Fatalf("unlimited transition %v was lost", unlimited)
			}
		}
	}
}

func TestBudgetMeterCommitDuringRebalanceDoesNotGrantTwice(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	g := pairFakeGate(t, admin)
	u := s.Store.Ent.User.Create().SetEmail("interleave@test.local").SetPasswordHash("unused").SetQuota(1000).SaveX(ctx)
	exit := s.Store.Ent.Exit.Query().Where(entexit.GateIDEQ(int(g.id)), entexit.TagEQ("direct")).OnlyX(ctx)
	n := s.Store.Ent.Node.Create().SetGateID(int(g.id)).SetExitID(exit.ID).SetName("interleave").SetKernel("sing-box").SetProtocol("shadowsocks").SetPort(30001).SaveX(ctx)
	s.Store.Ent.Credential.Create().SetNodeID(n.ID).SetUserID(u.ID).SetStatsKey("interleave").SaveX(ctx)
	row := s.Store.Ent.Budget.Create().SetGateID(int(g.id)).SetUserID(u.ID).SetAllocated(1000).SetConfirmed(1000).SaveX(ctx)
	fired := false
	s.Store.Ent.Budget.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if !fired {
				fired = true
				tx, err := s.Store.Ent.Tx(ctx)
				if err != nil {
					return nil, err
				}
				if err = tx.User.UpdateOneID(u.ID).AddUsed(200).Exec(ctx); err != nil {
					tx.Rollback()
					return nil, err
				}
				if err = tx.Budget.UpdateOneID(row.ID).AddUsed(200).Exec(ctx); err != nil {
					tx.Rollback()
					return nil, err
				}
				if err = tx.Commit(); err != nil {
					return nil, err
				}
			}
			return next.Query(ctx, q)
		})
	}))
	if err := s.Rebalance(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if r := s.Store.Ent.Budget.GetX(ctx, row.ID); r.Allocated != 1000 {
		t.Fatalf("meter commit changed grant to %d", r.Allocated)
	}
}
