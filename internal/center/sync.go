package center

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/resource"
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

func pendingBumpKey(uid int) string { return "sync-bump:" + itoa(uid) }

// The journal is durable business state, not an evictable cache entry.
func (s *Server) pendingBumps(ctx context.Context, uid int, allowLegacyKV bool) ([]int, error) {
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ("credential-sync"), resource.KeyEQ(itoa(uid))).Only(ctx)
	if ent.IsNotFound(err) {
		if !allowLegacyKV {
			return nil, nil
		}
		raw, ok, e := s.Store.KV.Get(ctx, pendingBumpKey(uid))
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, nil
		}
		var ids []int
		if e = json.Unmarshal([]byte(raw), &ids); e != nil {
			return nil, e
		}
		if e = s.savePendingBumps(ctx, uid, ids); e != nil {
			return nil, e
		}
		return ids, s.Store.KV.Del(ctx, pendingBumpKey(uid))
	}
	if err != nil {
		return nil, err
	}
	var state struct {
		Gates []int `json:"gates"`
	}
	if err = decodeStrict(row.Data, &state); err != nil {
		return nil, err
	}
	return state.Gates, nil
}
func (s *Server) savePendingBumps(ctx context.Context, uid int, ids []int) error {
	data := map[string]any{"gates": dedupe(ids)}
	return s.Store.Ent.Resource.Create().SetKind("credential-sync").SetKey(itoa(uid)).SetOwnerID(uid).SetData(data).OnConflictColumns(resource.FieldKind, resource.FieldKey).UpdateNewValues().Exec(ctx)
}

func (s *Server) SyncUser(ctx context.Context, uid int) error {
	err := s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		changed, err := s.syncUserLocked(ctx, uid)
		if err != nil {
			return err
		}
		pending, err := s.pendingBumps(ctx, uid, true)
		if err != nil {
			return err
		}
		gates := append(changed, pending...)
		if len(gates) == 0 {
			return nil
		}
		if err = s.savePendingBumps(ctx, uid, gates); err != nil {
			return err
		}
		if err = s.bumpGates(ctx, gates); err != nil {
			return err
		}
		return s.savePendingBumps(ctx, uid, nil)
	})
	if err != nil {
		return err
	}
	return s.Rebalance(ctx, uid)
}

func (s *Server) syncUserLocked(ctx context.Context, uid int) ([]int, error) {
	return s.syncUserLockedWithLegacyJournal(ctx, uid, true)
}

func (s *Server) syncUserLockedInTransaction(ctx context.Context, uid int) ([]int, error) {
	return s.syncUserLockedWithLegacyJournal(ctx, uid, false)
}

func (s *Server) syncUserLockedWithLegacyJournal(ctx context.Context, uid int, allowLegacyKV bool) ([]int, error) {
	u, err := s.Store.Ent.User.Get(ctx, uid)
	if err != nil {
		return nil, err
	}
	nodes, err := s.userNodeIDs(ctx, u)
	if err != nil {
		return nil, err
	}
	chains, err := s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(uid)).Order(ent.Asc(chain.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	links, err := s.Store.Ent.Link.Query().Where(link.UserIDEQ(uid)).Order(ent.Asc(link.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	type want struct {
		linkID, nodeID, chainID int
		protocol                string
	}
	desired := map[string]want{}
	// WireGuard 没有按用户的流量统计，Gate 无法执行额度：只给不限额度的用户发凭据。
	wgBlocked := func(protocol string) bool { return protocol == "wireguard" && !u.Unlimited }
	for _, l := range links {
		for id, n := range nodes {
			if wgBlocked(n.Protocol) {
				continue
			}
			if l.AllNodes || contains(l.NodeIds, id) {
				desired[credKey(l.ID, id, 0)] = want{l.ID, id, 0, n.Protocol}
			}
		}
		for _, ch := range chains {
			entry, ok := nodes[ch.EntryNodeID]
			// 类型出口生成的链跟着它的入口节点走：链接包含这个入口，就包含经出口的那条
			picked := contains(l.ChainIds, ch.ID) || s.exitPolicy(ctx, ch.ExitID) != "" && contains(l.NodeIds, ch.EntryNodeID)
			if !ok || wgBlocked(entry.Protocol) || !(l.AllNodes || picked) {
				continue
			}
			desired[credKey(l.ID, ch.EntryNodeID, ch.ID)] = want{l.ID, ch.EntryNodeID, ch.ID, entry.Protocol}
		}
	}
	existing, err := s.Store.Ent.Credential.Query().Where(credential.UserIDEQ(uid), credential.InternalEQ(false)).All(ctx)
	if err != nil {
		return nil, err
	}
	// Allowance changes travel through execution budgets. WireGuard allowance
	// eligibility is already represented by the desired credential set below.
	projection := version([]any{u.Enabled, u.ExpiresAt, u.AllowChain, u.AllNodes, u.NodeIds})
	linkProjection := make([]any, 0, len(links))
	for _, l := range links {
		linkProjection = append(linkProjection, []any{l.ID, l.Enabled, l.AllNodes, l.NodeIds, l.ChainIds, l.StartsAt, l.EndsAt})
	}
	chainProjection := make([]any, 0, len(chains))
	for _, ch := range chains {
		chainProjection = append(chainProjection, []any{ch.ID, ch.Enabled, ch.EntryNodeID, ch.ExitID})
	}
	nodeProjection := make([]any, 0, len(nodes))
	ids := make([]int, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		n := nodes[id]
		nodeProjection = append(nodeProjection, []any{n.ID, n.GateID, n.Protocol})
	}
	projection = version([]any{projection, linkProjection, chainProjection, nodeProjection})
	stateKey := itoa(uid)
	previous, stateErr := s.Store.Ent.Resource.Query().Where(resource.KindEQ("credential-projection"), resource.KeyEQ(stateKey)).Only(ctx)
	if stateErr != nil && !ent.IsNotFound(stateErr) {
		return nil, stateErr
	}
	changedProjection := previous == nil || previous.Data["fingerprint"] != projection
	touched := map[int]bool{}
	if changedProjection {
		for id := range nodes {
			touched[id] = true
		}
		for _, c := range existing {
			touched[c.NodeID] = true
		}
	}
	// Journal destinations before deleting a credential: interrupted sync must
	// still revoke the previously installed configuration on its old Gate.
	pending, err := s.pendingBumps(ctx, uid, allowLegacyKV)
	if err != nil {
		return nil, err
	}
	for _, c := range existing {
		lid, cid := 0, 0
		if c.LinkID != nil {
			lid = *c.LinkID
		}
		if c.ChainID != nil {
			cid = *c.ChainID
		}
		if _, wanted := desired[credKey(lid, c.NodeID, cid)]; !wanted || c.ProbeParentID != nil {
			touched[c.NodeID] = true
		}
	}
	existingKeys := map[string]bool{}
	for _, c := range existing {
		lid, cid := 0, 0
		if c.LinkID != nil {
			lid = *c.LinkID
		}
		if c.ChainID != nil {
			cid = *c.ChainID
		}
		if c.ProbeParentID == nil {
			existingKeys[credKey(lid, c.NodeID, cid)] = true
		}
	}
	for key, w := range desired {
		if !existingKeys[key] {
			touched[w.nodeID] = true
		}
	}
	destinations, err := s.gatesOf(ctx, touched)
	if err != nil {
		return nil, err
	}
	if err = s.savePendingBumps(ctx, uid, append(pending, destinations...)); err != nil {
		return nil, err
	}
	if !changedProjection {
		touched = map[int]bool{}
	}
	for _, c := range existing {
		if c.ProbeParentID != nil {
			if err = s.Store.Ent.Credential.DeleteOne(c).Exec(ctx); err != nil {
				return nil, err
			}
			touched[c.NodeID] = true
			continue
		}
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
	if err = s.Store.Ent.Resource.Create().SetKind("credential-projection").SetKey(stateKey).SetOwnerID(uid).SetData(map[string]any{"fingerprint": projection}).OnConflictColumns(resource.FieldKind, resource.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		return nil, err
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
	es, err := s.Store.Ent.Exit.Query().Where(entexit.KindEQ(entexit.KindNode), entexit.EnabledEQ(true), entexit.ExitNodeIDNotNil()).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		desired[exitCredentialKey(e.ID)] = *e.ExitNodeID
	}
	existing, err := s.Store.Ent.Credential.Query().Where(credential.InternalEQ(true), credential.PurposeNEQ("probe")).All(ctx)
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

// reconcileCredentialJournal also covers deleted users whose last revocation
// was interrupted after their account row was removed.
func (s *Server) reconcileCredentialJournal(ctx context.Context) error {
	return s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ("credential-sync")).Order(ent.Asc(resource.FieldID)).All(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var state struct {
				Gates []int `json:"gates"`
			}
			if err = decodeStrict(row.Data, &state); err != nil {
				return err
			}
			if len(state.Gates) == 0 {
				continue
			}
			if err = s.bumpGates(ctx, state.Gates); err != nil {
				return err
			}
			if err = row.Update().SetData(map[string]any{"gates": []int{}}).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

// SyncAll 在节点或链式代理变化后同步全部内部凭据与每个普通用户的凭据。
func (s *Server) SyncAll(ctx context.Context) error {
	err := s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		gates, err := s.syncInternalLocked(ctx)
		if err != nil {
			return err
		}
		return s.bumpGates(ctx, gates)
	})
	if err != nil {
		return err
	}
	ids, err := s.Store.Ent.User.Query().IDs(ctx)
	if err != nil {
		return err
	}
	for _, uid := range ids {
		if err = s.SyncUser(ctx, uid); err != nil {
			return err
		}
	}
	return s.reconcileCredentialJournal(ctx)
}
