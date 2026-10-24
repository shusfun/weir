package center

import (
	"context"
	"sort"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/budget"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/node"
)

const (
	minChunk     int64 = 32 << 20 // 多 Gate 时每块至少 32 MiB
	chunkHorizon       = 24 * time.Hour
)

// userGates 返回该用户当前有有效（非内部）凭据的 Gate。
func (s *Server) userGates(ctx context.Context, uid int) ([]int, error) {
	creds, err := s.Store.Ent.Credential.Query().Where(credential.UserIDEQ(uid), credential.InternalEQ(false)).All(ctx)
	if err != nil || len(creds) == 0 {
		return nil, err
	}
	ids := make([]int, 0, len(creds))
	for _, c := range creds {
		ids = append(ids, c.NodeID)
	}
	nodes, err := s.Store.Ent.Node.Query().Where(node.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var gates []int
	for _, n := range nodes {
		if !seen[n.GateID] {
			seen[n.GateID] = true
			gates = append(gates, n.GateID)
		}
	}
	sort.Ints(gates)
	return gates, nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// planBudgets 是纯计算：给出每台 Gate 的新累计额度。规则：
//   - 只有一台 Gate：剩余额度整份给它（中心断线多久都不影响，也不会超额）。
//   - 多台 Gate：每块按该 Gate 上的使用速度估算约 24 小时用量，最少 32 MiB；
//     剩余不足四分之一块时补满一块，持有超过一块时收回到一块。
//   - 已下调但 Gate 尚未确认的额度仍按旧值占用；各 Gate 占用合计不超过剩余额度。
func planBudgets(remaining int64, rows []*ent.Budget) map[int]int64 {
	out := map[int]int64{}
	if len(rows) == 0 {
		return out
	}
	if remaining < 0 {
		remaining = 0
	}
	if len(rows) == 1 {
		out[rows[0].GateID] = rows[0].Used + remaining
		return out
	}
	for _, r := range rows {
		chunk := int64(r.Rate * chunkHorizon.Seconds())
		if chunk < minChunk {
			chunk = minChunk
		}
		if chunk > remaining {
			chunk = remaining
		}
		head := r.Allocated - r.Used
		if head < chunk/4 || head > chunk {
			out[r.GateID] = r.Used + chunk
		} else {
			out[r.GateID] = r.Allocated
		}
	}
	reserve := func(r *ent.Budget) int64 { return maxInt64(out[r.GateID], r.Confirmed) - r.Used }
	total := int64(0)
	for _, r := range rows {
		total += maxInt64(reserve(r), 0)
	}
	over := total - remaining
	// 先削减「增加」的部分（新值高于 Gate 已确认的值）；仍超出时按 Gate 顺序削减其余新值。
	for pass := 0; pass < 2 && over > 0; pass++ {
		for _, r := range rows {
			if over <= 0 {
				break
			}
			floor := maxInt64(r.Confirmed, r.Used)
			if pass == 1 {
				floor = r.Used
			}
			if out[r.GateID] > floor {
				cut := out[r.GateID] - floor
				if cut > over {
					cut = over
				}
				out[r.GateID] -= cut
				over -= cut
			}
		}
	}
	return out
}

// rebalanceUser 重新计算并保存该用户在各 Gate 上的额度，返回额度有变化、需要下发新配置的 Gate。
// 不限额度的用户不需要额度记录（下发时标记为不限）。
func (s *Server) rebalanceUser(ctx context.Context, uid int) ([]int, error) {
	var changed []int
	err := s.Store.Lock.Do(ctx, "budget:"+itoa(uid), func(ctx context.Context) error {
		u, err := s.Store.Ent.User.Get(ctx, uid)
		if err != nil {
			return err
		}
		gates, err := s.userGates(ctx, uid)
		if err != nil {
			return err
		}
		rows := make([]*ent.Budget, 0, len(gates))
		for _, gid := range gates {
			row, err := s.Store.Ent.Budget.Query().Where(budget.UserIDEQ(uid), budget.GateIDEQ(gid)).Only(ctx)
			if ent.IsNotFound(err) {
				row, err = s.Store.Ent.Budget.Create().SetUserID(uid).SetGateID(gid).Save(ctx)
			}
			if err != nil {
				return err
			}
			rows = append(rows, row)
		}
		if u.Quota == 0 {
			return nil
		}
		for gid, alloc := range planBudgets(u.Quota-u.Used, rows) {
			for _, r := range rows {
				if r.GateID != gid || r.Allocated == alloc {
					continue
				}
				rev, err := s.bumpGate(ctx, gid)
				if err != nil {
					return err
				}
				if _, err = r.Update().SetAllocated(alloc).SetRevision(rev).Save(ctx); err != nil {
					return err
				}
				changed = append(changed, gid)
			}
		}
		return nil
	})
	return changed, err
}

// Rebalance 重新分配某用户的额度并唤醒受影响 Gate 的配置长轮询（套餐、节点、链接变化后调用）。
func (s *Server) Rebalance(ctx context.Context, uid int) error {
	gates, err := s.rebalanceUser(ctx, uid)
	if err != nil {
		return err
	}
	// 凭据集合可能也变了：即使额度未变，也让该用户所在的 Gate 重新下发配置。
	all, err := s.userGates(ctx, uid)
	if err != nil {
		return err
	}
	return s.bumpGates(ctx, append(gates, all...))
}

// confirmBudgets 在 Gate 回报已应用 revision 后，把该 Gate 上版本不晚于它的额度标记为已确认。
// 下调被确认后释放的额度可分给其他 Gate，因此返回需要重新平衡的用户。
func (s *Server) confirmBudgets(ctx context.Context, gid int, applied int64) ([]int, error) {
	rows, err := s.Store.Ent.Budget.Query().Where(budget.GateIDEQ(gid), budget.RevisionLTE(applied)).All(ctx)
	if err != nil {
		return nil, err
	}
	var users []int
	for _, r := range rows {
		if r.Confirmed == r.Allocated {
			continue
		}
		freed := r.Confirmed > r.Allocated
		if _, err = r.Update().SetConfirmed(r.Allocated).Save(ctx); err != nil {
			return nil, err
		}
		if freed {
			users = append(users, r.UserID)
		}
	}
	return users, nil
}
