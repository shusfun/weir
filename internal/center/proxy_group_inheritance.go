package center

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/proxygroupversion"
	"github.com/shusfun/weir/internal/store/ent/user"
	"github.com/shusfun/weir/packages/contracts"
)

const (
	groupCustomized = "customized"
	groupRejected   = "rejected"
)

type proxyPolicyLockContextKey struct{}

func withProxyPolicyLock(ctx context.Context, uid int) context.Context {
	return context.WithValue(ctx, proxyPolicyLockContextKey{}, uid)
}

func proxyPolicyLockHeld(ctx context.Context, uid int) bool {
	held, ok := ctx.Value(proxyPolicyLockContextKey{}).(int)
	return ok && held == uid
}

func writeGroupVersion(ctx context.Context, tx *ent.Tx, ownerID int, group contracts.ProxyGroup, deleted bool) (err error) {
	finish := operationStage(ctx, "proxy.group.version", "owner_id", ownerID, "group_id", group.ID, "deleted", deleted)
	defer func() { finish(err) }()
	active, err := tx.ProxyGroupVersion.Query().Where(proxygroupversion.OwnerIDEQ(ownerID), proxygroupversion.GroupIDEQ(group.ID), proxygroupversion.ValidUntilIsNil()).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if active != nil {
		if !deleted && sameProxyGroup(active.Snapshot, group) {
			return nil
		}
		if !now.After(active.ValidFrom) {
			now = active.ValidFrom.Add(time.Microsecond)
		}
		if err := active.Update().SetValidUntil(now).Exec(ctx); err != nil {
			return err
		}
	}
	if deleted {
		return nil
	}
	return tx.ProxyGroupVersion.Create().SetOwnerID(ownerID).SetGroupID(group.ID).SetSnapshot(group).
		SetValidFrom(now).SetTraceID(taskTrace(ctx)).Exec(ctx)
}

func allocateProxyGroupID(policy *contracts.ProxyPolicy) int {
	next := max(policy.NextGroupID, 1)
	for _, group := range policy.Groups {
		next = max(next, group.ID+1)
	}
	policy.NextGroupID = next + 1
	return next
}

func groupNameTaken(groups []contracts.ProxyGroup, name string) bool {
	return slices.ContainsFunc(groups, func(group contracts.ProxyGroup) bool { return strings.EqualFold(group.Name, name) })
}

func (s *Server) refreshGroupInheritance(ctx context.Context, uid int) (account *ent.User, err error) {
	if proxyPolicyLockHeld(ctx, uid) {
		account, err = s.Store.Ent.User.Get(ctx, uid)
		if err != nil {
			return nil, err
		}
		return s.syncGroupInheritanceLocked(ctx, account, true)
	}
	err = s.Store.Lock.Do(ctx, "proxy-policy:"+itoa(uid), func(ctx context.Context) error {
		var err error
		account, err = s.Store.Ent.User.Get(ctx, uid)
		if err != nil {
			return err
		}
		account, err = s.syncGroupInheritanceLocked(ctx, account, true)
		return err
	})
	return account, err
}

func (s *Server) syncGroupInheritanceLocked(ctx context.Context, account *ent.User, persist bool) (out *ent.User, err error) {
	policy := loadPolicy(account)
	if account.Role != user.RoleUser || !account.AllNodes && len(account.NodeIds) == 0 && len(policy.GroupInheritance) == 0 {
		return account, nil
	}
	sourceVersions, err := s.activeGroupVersions(ctx, account)
	if err != nil {
		return nil, err
	}
	before := slices.Clone(policy.Groups)
	tracked := make(map[int]bool, len(policy.GroupInheritance))
	for _, binding := range policy.GroupInheritance {
		if binding.State != groupCustomized {
			tracked[binding.LocalID] = true
		}
	}
	policy.Groups = slices.DeleteFunc(policy.Groups, func(group contracts.ProxyGroup) bool { return tracked[group.ID] })
	minimum := inheritedGroupMinMembers(account)
	for _, source := range sourceVersions {
		group := source.Snapshot
		if len(group.Members) < minimum || len(policy.Groups) >= contracts.MaxProxyGroups || groupNameTaken(policy.Groups, group.Name) {
			continue
		}
		index := slices.IndexFunc(policy.GroupInheritance, func(binding contracts.ProxyGroupInheritance) bool {
			return binding.OwnerID == source.OwnerID && binding.GroupID == source.GroupID
		})
		if index >= 0 {
			binding := policy.GroupInheritance[index]
			if binding.State != "" {
				continue
			}
			group.ID = binding.LocalID
		} else {
			group.ID = allocateProxyGroupID(&policy)
			policy.GroupInheritance = append(policy.GroupInheritance, contracts.ProxyGroupInheritance{
				OwnerID: source.OwnerID, GroupID: source.GroupID, LocalID: group.ID,
			})
		}
		policy.Groups = append(policy.Groups, group)
	}
	slices.SortFunc(policy.Groups, func(left, right contracts.ProxyGroup) int { return left.ID - right.ID })
	if slices.EqualFunc(before, policy.Groups, sameProxyGroup) {
		return account, nil
	}
	policy.Revision++
	if !persist {
		copy := *account
		copy.ProxyPolicy = policyMap(policy)
		return &copy, nil
	}
	finish := operationStage(ctx, "proxy.group.inherit", "user_id", account.ID, "policy_revision", policy.Revision, "group_count", len(policy.Groups))
	defer func() { finish(err) }()
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	updated, err := tx.User.UpdateOneID(account.ID).SetProxyPolicy(policyMap(policy)).Save(ctx)
	if err != nil {
		return nil, err
	}
	err = tx.Audit.Create().SetActor(causeFrom(ctx).Actor).SetAction("proxy.group.inherit").SetTarget("user:" + itoa(account.ID)).SetResult("ok").
		SetDetail(map[string]any{"trace_id": taskTrace(ctx), "policy_revision": policy.Revision}).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "proxy groups reconciled", "trace", taskTrace(ctx), "user_id", account.ID)
	return updated.Unwrap(), nil
}

func sameProxyGroup(left, right contracts.ProxyGroup) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Kind == right.Kind &&
		left.IntervalSeconds == right.IntervalSeconds && left.ToleranceMS == right.ToleranceMS && slices.Equal(left.Members, right.Members)
}

func (s *Server) activeGroupVersions(ctx context.Context, account *ent.User) ([]*ent.ProxyGroupVersion, error) {
	if !account.AllNodes && len(account.NodeIds) == 0 {
		return nil, nil
	}
	primary, err := s.Store.Ent.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Order(ent.Asc(user.FieldCreatedAt, user.FieldID)).First(ctx)
	if err != nil {
		return nil, err
	}
	versions, err := s.Store.Ent.ProxyGroupVersion.Query().Where(proxygroupversion.OwnerIDEQ(primary.ID), proxygroupversion.ValidUntilIsNil()).
		Order(ent.Asc(proxygroupversion.FieldGroupID)).All(ctx)
	if err != nil || len(versions) == 0 {
		return versions, err
	}
	nodes, err := s.userNodeIDs(ctx, account)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(nodes))
	for id := range nodes {
		allowed["node:"+itoa(id)] = true
	}
	for _, version := range versions {
		version.Snapshot.Members = slices.DeleteFunc(version.Snapshot.Members, func(ref string) bool { return !allowed[ref] })
	}
	return versions, nil
}

func inheritedGroupMinMembers(account *ent.User) int {
	if account.AllNodes {
		return 1
	}
	return 2
}

func groupInheritanceMinimums(account *ent.User, policy contracts.ProxyPolicy) map[int]int {
	minimums := make(map[int]int, len(policy.GroupInheritance))
	for _, binding := range policy.GroupInheritance {
		if binding.State == "" {
			minimums[binding.LocalID] = inheritedGroupMinMembers(account)
		}
	}
	return minimums
}

func markInheritedGroup(policy *contracts.ProxyPolicy, id int, state string) {
	for index, binding := range policy.GroupInheritance {
		if binding.LocalID == id {
			policy.GroupInheritance[index].State = state
			return
		}
	}
}
