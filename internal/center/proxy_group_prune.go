package center

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/user"
	"github.com/shusfun/weir/packages/contracts"
)

func (s *Server) signalStoreReady() {
	select {
	case s.storeReady <- struct{}{}:
	default:
	}
}

func (s *Server) pruneGroupsWhenReady(ctx context.Context) {
	for ctx.Err() == nil {
		s.state.RLock()
		if s.Store != nil {
			_, err := s.pruneGroupMembers(withTrace(ctx, randomHex(16)))
			s.state.RUnlock()
			if err != nil {
				slog.Warn("proxy group prune", "err", redact(err.Error()))
			}
			return
		}
		s.state.RUnlock()
		select {
		case <-ctx.Done():
			return
		case <-s.storeReady:
		}
	}
}

// pruneGroupMembers 是删除节点、链式代理与代理之后唯一的分组清理入口：
// 去掉指向已不存在的节点、链式代理的成员，以及 drop 里点名移除的成员（如因删代理而停用的链式代理），
// 成员清空的分组一并删除。暂时停用或不可用的线路不动，由编辑器展示原因并允许取消勾选。
// 幂等，启动时也调用一次清理历史残留。
func (s *Server) pruneGroupMembers(ctx context.Context, drop ...string) (removed int, err error) {
	finish := operationStage(ctx, "proxy.group.prune", "drop", len(drop), "removed", stageValue(func() slog.Value { return slog.IntValue(removed) }))
	defer func() { finish(err) }()
	dropped := map[string]bool{}
	for _, ref := range drop {
		dropped[ref] = true
	}
	ids, err := s.Store.Ent.User.Query().IDs(ctx)
	if err != nil {
		return 0, err
	}
	for _, uid := range ids {
		n, err := s.pruneUserGroupMembers(ctx, uid, dropped)
		if err != nil {
			return removed, err
		}
		removed += n
	}
	return removed, nil
}

func (s *Server) pruneUserGroupMembers(ctx context.Context, uid int, dropped map[string]bool) (int, error) {
	removed := 0
	err := s.Store.Lock.Do(ctx, "proxy-policy:"+itoa(uid), func(ctx context.Context) error {
		u, err := s.Store.Ent.User.Get(ctx, uid)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		p := loadPolicy(u)
		if len(p.Groups) == 0 {
			return nil
		}
		alive, err := s.liveLineRefs(ctx, u, p)
		if err != nil {
			return err
		}
		groups := p.Groups[:0]
		var edited, deleted []contracts.ProxyGroup
		for _, g := range p.Groups {
			originalCount := len(g.Members)
			kept := g.Members[:0]
			for _, ref := range g.Members {
				if alive[ref] && !dropped[ref] {
					kept = append(kept, ref)
				} else {
					removed++
				}
			}
			g.Members = kept
			if len(kept) > 0 {
				groups = append(groups, g)
				if len(kept) != originalCount {
					edited = append(edited, g)
				}
			} else if originalCount > 0 {
				deleted = append(deleted, g)
			}
		}
		if removed == 0 {
			return nil
		}
		p.Groups = groups
		p.Revision++
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if u.Role == user.RoleAdmin {
			for _, group := range edited {
				if err := writeGroupVersion(ctx, tx, uid, group, false); err != nil {
					return err
				}
			}
			for _, group := range deleted {
				if err := writeGroupVersion(ctx, tx, uid, group, true); err != nil {
					return err
				}
			}
		}
		if err = tx.User.UpdateOneID(uid).SetProxyPolicy(policyMap(p)).Exec(ctx); err != nil {
			return err
		}
		if err = tx.Audit.Create().SetActor(causeFrom(ctx).Actor).SetAction("proxy.group.prune").SetTarget("user:" + itoa(uid)).
			SetResult("ok removed=" + itoa(removed)).SetDetail(map[string]any{"trace_id": taskTrace(ctx)}).Exec(ctx); err != nil {
			return err
		}
		return tx.Commit()
	})
	return removed, err
}

// liveLineRefs 返回分组成员里仍指向现存对象的 ref。
func (s *Server) liveLineRefs(ctx context.Context, u *ent.User, p contracts.ProxyPolicy) (map[string]bool, error) {
	nodeIDs, chainIDs := []int{}, []int{}
	for _, g := range p.Groups {
		for _, ref := range g.Members {
			kind, id, ok := strings.Cut(ref, ":")
			n, err := strconv.Atoi(id)
			if !ok || err != nil {
				continue
			}
			switch kind {
			case "node":
				nodeIDs = append(nodeIDs, n)
			case "chain":
				chainIDs = append(chainIDs, n)
			}
		}
	}
	alive := map[string]bool{}
	if len(nodeIDs) > 0 {
		ids, err := s.Store.Ent.Node.Query().Where(node.IDIn(nodeIDs...)).IDs(ctx)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			alive["node:"+itoa(id)] = true
		}
	}
	if len(chainIDs) > 0 {
		ids, err := s.Store.Ent.Chain.Query().Where(chain.IDIn(chainIDs...), chain.UserIDEQ(u.ID)).IDs(ctx)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			alive["chain:"+itoa(id)] = true
		}
	}
	return alive, nil
}
