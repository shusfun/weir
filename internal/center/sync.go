package center

import (
	"context"
	"fmt"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/node"
)

// 凭据由「期望状态」推导：每条链接 × 它覆盖的每个节点 / 自建链式代理各一份独立凭据。
// 套餐、节点、链接、链式代理任何变化后调用同步函数，按差异增删凭据并重新分配额度。

func credKey(linkID, nodeID, chainID int) string {
	return fmt.Sprintf("%d/%d/%d", linkID, nodeID, chainID)
}

func newStatsKey() string { return "c" + randomHex(8) }

func contains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// userNodeIDs 返回用户可用的节点（all_nodes 时为全部节点，否则为仍存在的指定节点）。
func (s *Server) userNodeIDs(ctx context.Context, u *ent.User) (map[int]*ent.Node, error) {
	q := s.Store.Ent.Node.Query()
	if !u.AllNodes {
		if len(u.NodeIds) == 0 {
			return map[int]*ent.Node{}, nil
		}
		q = q.Where(node.IDIn(u.NodeIds...))
	}
	nodes, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int]*ent.Node, len(nodes))
	for _, n := range nodes {
		out[n.ID] = n
	}
	return out, nil
}

// SyncUser 让该用户的凭据与其套餐、链接、链式代理一致，然后重新分配额度并通知相关 Gate。
func (s *Server) SyncUser(ctx context.Context, uid int) error {
	var gates []int
	err := s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		var err error
		gates, err = s.syncUserLocked(ctx, uid)
		return err
	})
	if err != nil {
		return err
	}
	if err = s.bumpGates(ctx, gates); err != nil {
		return err
	}
	return s.Rebalance(ctx, uid)
}

func (s *Server) syncUserLocked(ctx context.Context, uid int) ([]int, error) {
	u, err := s.Store.Ent.User.Get(ctx, uid)
	if err != nil {
		return nil, err
	}
	nodes, err := s.userNodeIDs(ctx, u)
	if err != nil {
		return nil, err
	}
	chains, err := s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(uid)).All(ctx)
	if err != nil {
		return nil, err
	}
	links, err := s.Store.Ent.Link.Query().Where(link.UserIDEQ(uid)).All(ctx)
	if err != nil {
		return nil, err
	}
	type want struct {
		linkID, nodeID, chainID int
		protocol                string
	}
	desired := map[string]want{}
	for _, l := range links {
		for id, n := range nodes {
			if l.AllNodes || contains(l.NodeIds, id) {
				desired[credKey(l.ID, id, 0)] = want{l.ID, id, 0, n.Protocol}
			}
		}
		for _, ch := range chains {
			entry, ok := nodes[ch.EntryNodeID]
			if !ok || !(l.AllNodes || contains(l.ChainIds, ch.ID)) {
				continue
			}
			desired[credKey(l.ID, ch.EntryNodeID, ch.ID)] = want{l.ID, ch.EntryNodeID, ch.ID, entry.Protocol}
		}
	}
	existing, err := s.Store.Ent.Credential.Query().Where(credential.UserIDEQ(uid), credential.InternalEQ(false)).All(ctx)
	if err != nil {
		return nil, err
	}
	touched := map[int]bool{}
	for _, c := range existing {
		lid, cid := 0, 0
		if c.LinkID != nil {
			lid = *c.LinkID
		}
		if c.ChainID != nil {
			cid = *c.ChainID
		}
		key := credKey(lid, c.NodeID, cid)
		if _, ok := desired[key]; ok {
			delete(desired, key)
			continue
		}
		if err = s.Store.Ent.Credential.DeleteOne(c).Exec(ctx); err != nil {
			return nil, err
		}
		touched[c.NodeID] = true
	}
	for _, w := range desired {
		id, password := newSecret(w.protocol)
		create := s.Store.Ent.Credential.Create().SetNodeID(w.nodeID).SetUserID(uid).SetLinkID(w.linkID).
			SetStatsKey(newStatsKey()).SetUUID(id).SetPassword(password)
		if w.chainID != 0 {
			create.SetChainID(w.chainID)
		}
		if _, err = create.Save(ctx); err != nil {
			return nil, err
		}
		touched[w.nodeID] = true
	}
	return s.gatesOf(ctx, touched)
}

func (s *Server) gatesOf(ctx context.Context, nodeIDs map[int]bool) ([]int, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	ids := make([]int, 0, len(nodeIDs))
	for id := range nodeIDs {
		ids = append(ids, id)
	}
	nodes, err := s.Store.Ent.Node.Query().Where(node.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	seen, out := map[int]bool{}, []int{}
	for _, n := range nodes {
		if !seen[n.GateID] {
			seen[n.GateID] = true
			out = append(out, n.GateID)
		}
	}
	return out, nil
}

// syncInternalLocked 维护链式代理出口节点上的系统内部凭据：节点链（nodex-）与用户链（chainx-）。
func (s *Server) syncInternalLocked(ctx context.Context) ([]int, error) {
	desired := map[string]int{} // stats key → 出口节点
	nodes, err := s.Store.Ent.Node.Query().Where(node.ChainKindEQ(node.ChainKindNode), node.ChainNodeIDNotNil()).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		desired[nodeExitKey(n.ID)] = *n.ChainNodeID
	}
	chains, err := s.Store.Ent.Chain.Query().Where(chain.ExitKindEQ(chain.ExitKindNode), chain.ExitNodeIDNotNil()).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, ch := range chains {
		desired[chainExitKey(ch.ID)] = *ch.ExitNodeID
	}
	existing, err := s.Store.Ent.Credential.Query().Where(credential.InternalEQ(true)).All(ctx)
	if err != nil {
		return nil, err
	}
	touched := map[int]bool{}
	for _, c := range existing {
		if exit, ok := desired[c.StatsKey]; ok && exit == c.NodeID {
			delete(desired, c.StatsKey)
			continue
		}
		if err = s.Store.Ent.Credential.DeleteOne(c).Exec(ctx); err != nil {
			return nil, err
		}
		touched[c.NodeID] = true
	}
	for key, exitID := range desired {
		exit, err := s.Store.Ent.Node.Get(ctx, exitID)
		if err != nil {
			continue // 出口节点已删除：编译配置时该链会被撤下并提示
		}
		id, password := newSecret(exit.Protocol)
		if _, err = s.Store.Ent.Credential.Create().SetNodeID(exitID).SetStatsKey(key).SetUUID(id).SetPassword(password).SetInternal(true).Save(ctx); err != nil {
			return nil, err
		}
		touched[exitID] = true
	}
	return s.gatesOf(ctx, touched)
}

// SyncAll 在节点或链式代理变化后同步全部内部凭据与每个普通用户的凭据。
func (s *Server) SyncAll(ctx context.Context) error {
	var gates []int
	var users []int
	err := s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		g, err := s.syncInternalLocked(ctx)
		if err != nil {
			return err
		}
		gates = append(gates, g...)
		ids, err := s.Store.Ent.User.Query().IDs(ctx)
		if err != nil {
			return err
		}
		for _, uid := range ids {
			g, err := s.syncUserLocked(ctx, uid)
			if err != nil {
				return err
			}
			gates = append(gates, g...)
		}
		users = ids
		return nil
	})
	if err != nil {
		return err
	}
	if err = s.bumpGates(ctx, gates); err != nil {
		return err
	}
	for _, uid := range users {
		if err = s.Rebalance(ctx, uid); err != nil {
			return err
		}
	}
	return nil
}
