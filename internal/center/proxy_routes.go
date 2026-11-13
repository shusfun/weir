package center

import (
	"context"
	"errors"
	"fmt"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/packages/contracts"
)

// 代理接口统一按登录账号与 user 查询参数解析归属。
func (s *Server) proxyRoutes(auth, admin *gin.RouterGroup) {
	s.pendingExitRoutes(auth)
	auth.GET("/proxy/preview", s.previewSubscription)
	auth.GET("/proxy", func(c *gin.Context) { s.getProxy(c, false) })
	auth.POST("/proxy/groups", func(c *gin.Context) { s.writeProxyGroup(c, false) })
	auth.PUT("/proxy/groups/:group", func(c *gin.Context) { s.writeProxyGroup(c, false) })
	auth.DELETE("/proxy/groups/:group", func(c *gin.Context) { s.writeProxyGroup(c, false) })
	auth.POST("/proxy/migration/ack", func(c *gin.Context) { s.ackProxyMigration(c, false) })
	auth.PUT("/proxy", func(c *gin.Context) { s.putProxy(c, false) })
	auth.PUT("/proxy/custom", s.asJob("保存自定义清单", "已取消，清单保持原样", func(c *gin.Context) { s.putCustomRules(c, false) }))
	auth.POST("/proxy/custom/parse", func(c *gin.Context) { s.parseCustomList(c, false) })
	auth.POST("/proxy/custom/append", s.asJob("添加清单规则", "已取消，清单保持原样", func(c *gin.Context) { s.appendCustomEntries(c, false) }))
	auth.PATCH("/proxy/custom/entries/:entry", s.asJob("更新清单规则", "已取消，清单保持原样", func(c *gin.Context) { s.changeCustomEntry(c, false) }))
	auth.DELETE("/proxy/custom/entries/:entry", s.asJob("删除清单规则", "已取消，清单保持原样", func(c *gin.Context) { s.changeCustomEntry(c, false) }))
	auth.POST("/proxy/custom/entries/:entry/test", func(c *gin.Context) { s.testCustomEntry(c, false) })
	auth.POST("/proxy/custom/test", func(c *gin.Context) { s.testCustomRules(c, false) })
	auth.PUT("/proxy/types/:type/exit", s.asJob("给一类节点加出口", "已取消，出口保持原样", func(c *gin.Context) { s.putTypeExit(c, false) }))
	auth.DELETE("/proxy/types/:type/exit", func(c *gin.Context) { s.deleteTypeExit(c, false) })
}

func (s *Server) proxySubject(c *gin.Context, internal bool) (*ent.User, bool) {
	actor := c.MustGet("account").(*ent.User)
	raw := c.Query("user")
	if internal && c.Param("uid") != "" {
		raw = c.Param("uid")
	}
	if raw == "" {
		raw = itoa(actor.ID)
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		failure(c, 400, "invalid_user")
		return nil, false
	}
	if actor.Role != "admin" && id != actor.ID {
		s.audit(c, "proxy.denied", "user:"+itoa(id), "forbidden")
		failure(c, 403, "forbidden")
		return nil, false
	}
	u, err := s.Store.Ent.User.Get(c.Request.Context(), id)
	if ent.IsNotFound(err) {
		failure(c, 404, "user_not_found")
		return nil, false
	}
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return nil, false
	}
	return u, true
}

// proxyType 是代理页里的一个类型：节点、是否放进订阅、统一出口。
type proxyType struct {
	nodeType
	nodes []*ent.Node
}

// userTypes 把用户可用、已启用的节点按类型归类（按方案顺序）。
func (s *Server) userTypes(ctx context.Context, u *ent.User) ([]*proxyType, error) {
	nodes, err := s.userNodeIDs(ctx, u)
	if err != nil {
		return nil, err
	}
	ti := s.typeIndex(ctx)
	byKey := map[string]*proxyType{}
	var out []*proxyType
	for _, n := range sortedNodes(nodes) {
		if !n.Enabled {
			continue
		}
		t := ti.of(n)
		pt, ok := byKey[t.Key]
		if !ok {
			pt = &proxyType{nodeType: t}
			byKey[t.Key] = pt
			out = append(out, pt)
		}
		pt.nodes = append(pt.nodes, n)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, nil
}

func (s *Server) policyChains(ctx context.Context, uid int, typ string) ([]*ent.Chain, error) {
	return s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(uid), chain.HasExitWith(entexit.PolicyTypeEQ(typ))).Order(ent.Asc(chain.FieldID)).All(ctx)
}

func (s *Server) getProxy(c *gin.Context, admin bool) {
	u, ok := s.proxySubject(c, admin)
	if !ok {
		return
	}
	v, err := s.proxyView(c.Request.Context(), u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) proxyView(ctx context.Context, u *ent.User) (gin.H, error) {
	types, err := s.userTypes(ctx, u)
	if err != nil {
		return nil, err
	}
	pol := loadPolicy(u)
	disabled := map[string]bool{}
	for _, t := range pol.Disabled {
		disabled[t] = true
	}
	gates := map[int]*ent.Gate{}
	var nodeIDs []int
	for _, t := range types {
		for _, n := range t.nodes {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}
	own, err := s.userRates(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	all, err := s.nodeRates(ctx, nodeIDs)
	if err != nil {
		return nil, err
	}
	items := []gin.H{}
	for _, t := range types {
		chains, err := s.policyChains(ctx, u.ID, t.Key)
		if err != nil {
			return nil, err
		}
		byEntry := map[int]*ent.Chain{}
		for _, ch := range chains {
			byEntry[ch.EntryNodeID] = ch
		}
		nodes := []gin.H{}
		var missing []string
		online := 0
		for _, n := range t.nodes {
			g, ok := gates[n.GateID]
			if !ok {
				g, _ = s.Store.Ent.Gate.Get(ctx, n.GateID)
				gates[n.GateID] = g
			}
			h := s.healthOf(ctx, n, g)
			chainID := 0
			if ch := byEntry[n.ID]; ch != nil {
				chainID = ch.ID
				if p, ok := s.loadExitProbe(ctx, exitOwner(ch.ExitID)); ok && !p.OK && h.Status != "down" {
					h = nodeHealth{Status: "down", Why: "出口实测失败"}
				}
			} else if len(chains) > 0 {
				missing = append(missing, n.Name)
			}
			if h.Status != "down" {
				online++
			}
			rate := pathRate(own, all, n.ID, chainID)
			item := s.nodeChoice(ctx, n, g, byEntry[n.ID])
			item["health"], item["why"], item["rate"] = h.Status, h.Why, rate
			if g != nil {
				item["gate_id"], item["gate_name"] = g.ID, g.Name
			}
			nodes = append(nodes, item)
		}
		v := gin.H{"key": t.Key, "label": t.Label, "enabled": !disabled[t.Key], "nodes": nodes, "online": online, "missing_exit": missing, "udp": typeUDP(s, ctx, t.nodes)}
		if len(chains) > 0 {
			v["exit"] = s.chainExitView(ctx, chains[0])
		}
		items = append(items, v)
	}
	// 名次仅供展示，客户端成员顺序由统一布局决定。
	ranking := []gin.H{}
	preview, err := s.customPlanned(ctx, u, pol)
	if err != nil {
		return nil, err
	}
	for i, e := range preview.Entries {
		r := preview.Ranked[i]
		ranking = append(ranking, gin.H{"ref": e.Ref, "node_id": e.NodeID, "chain_id": e.ChainID, "name": e.Name, "type": e.Type, "health": r.Health.Status, "why": r.Health.Why, "rate": r.Rate, "rank": r.Rank, "auto": r.Auto})
	}
	sort.SliceStable(ranking, func(i, j int) bool {
		ri, rj := ranking[i]["rank"].(int), ranking[j]["rank"].(int)
		if (ri == 0) != (rj == 0) {
			return ri != 0
		}
		return ri < rj
	})
	lines, err := s.proxyLines(ctx, u)
	if err != nil {
		return nil, err
	}
	groups := proxyGroupsView(pol, lines)
	custom := customView(pol.Custom)
	custom["revision"] = pol.Revision
	names, planItems := []string{}, []subout.Item{}
	usedNames := map[string]bool{}
	for _, line := range lines {
		if !line.Available {
			continue
		}
		name := line.Name
		if name == "" {
			name = line.Ref
		}
		if usedNames[name] {
			base := name + "（" + line.Ref + "）"
			name = base
			for i := 2; usedNames[name]; i++ {
				name = fmt.Sprintf("%s %d", base, i)
			}
		}
		usedNames[name] = true
		names = append(names, name)
		planItems = append(planItems, subout.Item{Ref: line.Ref, Direct: !line.Chained, Auto: !line.Chained && line.Health != "down"})
	}
	plan := subout.Plan{Groups: pol.Groups, HiddenLines: pol.HiddenLines}
	layout, _ := plan.Layout(names, planItems)
	presets := []gin.H{}
	for _, rc := range ruleCategories {
		if rc.Key == "cn" || rc.Key == "final" {
			continue
		}
		presets = append(presets, gin.H{"key": rc.Key, "label": rc.Label, "domains": rc.Domains})
	}
	pol.Custom = contracts.CustomRules{}
	return gin.H{"user_id": u.ID, "policy": pol, "custom": custom, "types": items, "layout": layout, "presets": presets, "ranking": ranking, "allow_chain": u.AllowChain,
		"min_refresh": contracts.MinRefreshMinutes, "groups": groups, "lines": lines}, nil
}

// chainExitView 是类型出口的显示内容（与链式代理列表一致：密码给本人与管理员看原文）。
func (s *Server) chainExitView(ctx context.Context, ch *ent.Chain) gin.H {
	e, err := s.Store.Ent.Exit.Get(ctx, ch.ExitID)
	if err != nil {
		return gin.H{"error": "出口未知"}
	}
	v, err := s.exitResourceView(ctx, e)
	if err != nil {
		return gin.H{"error": "出口未知"}
	}
	return v
}

type proxyRequest struct {
	Revision    *int                 `json:"revision"`
	Disabled    []string             `json:"disabled"`
	HiddenLines []string             `json:"hidden_lines"`
	Auto        contracts.AutoPolicy `json:"auto"`
}

func (s *Server) putProxy(c *gin.Context, admin bool) {
	u, ok := s.proxySubject(c, admin)
	if !ok {
		return
	}
	var req proxyRequest
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	conflict, invalid := errors.New("proxy revision conflict"), errors.New("invalid settings")
	var code, detail string
	err := s.Store.Lock.Do(ctx, "proxy-policy:"+itoa(u.ID), func(ctx context.Context) error {
		latest, err := s.Store.Ent.User.Get(ctx, u.ID)
		if err != nil {
			return err
		}
		p := loadPolicy(latest)
		if req.Revision == nil || *req.Revision != p.Revision {
			return conflict
		}
		if req.Auto.RefreshMinutes < 30 || req.Auto.RefreshMinutes > 1440 {
			code, detail = "invalid_refresh", "刷新间隔须为 30 至 1440 分钟"
			return invalid
		}
		types, err := s.userTypes(ctx, latest)
		if err != nil {
			return err
		}
		for _, key := range req.Disabled {
			if !slices.ContainsFunc(types, func(t *proxyType) bool { return t.Key == key }) {
				code, detail = "invalid_disabled", "请选择已授权的协议类型"
				return invalid
			}
		}
		lines, err := s.proxyLines(ctx, latest)
		if err != nil {
			return err
		}
		for _, ref := range req.HiddenLines {
			if !slices.Contains(p.HiddenLines, ref) && !slices.ContainsFunc(lines, func(l proxyLine) bool { return l.Ref == ref }) {
				code, detail = "invalid_hidden_lines", "请选择已授权的线路"
				return invalid
			}
		}
		p.Disabled, p.HiddenLines, p.Auto = dedupeStr(req.Disabled), dedupeStr(req.HiddenLines), req.Auto
		p = p.Normalized()
		if dryRun(c, dryResult{Preview: []string{"客户端下次更新订阅时生效，订阅地址不变"}}) {
			return nil
		}
		p.Revision++
		u, err = latest.Update().SetProxyPolicy(policyMap(p)).Save(ctx)
		return err
	})
	if errors.Is(err, conflict) {
		failure(c, 409, "proxy_revision_conflict")
		return
	}
	if errors.Is(err, invalid) {
		failDetail(c, 400, code, detail, nil)
		return
	}
	if err != nil {
		failInternal(c, 500, "update_failed", err)
		return
	}
	if c.Writer.Written() {
		return
	}
	s.audit(c, "proxy.update", "user:"+itoa(u.ID), "ok")
	v, err := s.proxyView(ctx, u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, v)
}

func dedupeStr(v []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range v {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// typeParam 找到用户可用的某个类型。
func (s *Server) typeParam(c *gin.Context, u *ent.User) (*proxyType, bool) {
	types, err := s.userTypes(c.Request.Context(), u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return nil, false
	}
	for _, t := range types {
		if t.Key == c.Param("type") {
			return t, true
		}
	}
	failure(c, 404, "type_not_found")
	return nil, false
}

// putTypeExit 给一个类型的全部节点加同一个出口：每个节点一条链式代理（入口是这个节点），保存前逐个由入口 VPS 实测，
// 有一个不通就整体不保存并列出原因。出口是另一个节点时，那个节点自己作入口时跳过（不能经自己出去）。
// 以后新增的这类节点不会自动加出口，页面上会列出还没加的节点，再保存一次即可。
func (s *Server) putTypeExitNow(c *gin.Context, admin bool) {
	u, ok := s.proxySubject(c, admin)
	if !ok {
		return
	}
	if !u.AllowChain {
		failure(c, 403, "chain_disabled")
		return
	}
	t, ok := s.typeParam(c, u)
	if !ok {
		return
	}
	var req struct {
		Exit     ExitSpec `json:"exit"`
		Revision *int     `json:"revision"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Revision == nil || *req.Revision != loadPolicy(u).Revision {
		failure(c, 409, "proxy_revision_conflict")
		return
	}
	ctx := c.Request.Context()
	type planned struct {
		node  *ent.Node
		probe *exitProbe
	}
	for _, n := range t.nodes {
		if req.Exit.Kind == "node" && req.Exit.NodeID == n.ID {
			continue
		}
		g, e := s.Store.Ent.Gate.Get(ctx, n.GateID)
		if e != nil {
			failInternal(c, 503, "query_failed", e)
			return
		}
		if _, _, _, e = s.validateExitSelection(ctx, g, n.ID, &u.ID, 0, &req.Exit, true, c.MustGet("account").(*ent.User).Role == "admin", n.Kernel); e != nil {
			failErr(c, 422, e)
			return
		}
	}
	var ok2 []planned
	var failed []gin.H
	var verificationFailures []error
	jobTitle(ctx, "给「"+t.Label+"」加出口")
	for i, n := range t.nodes {
		if req.Exit.Kind == "node" && req.Exit.NodeID == n.ID {
			continue
		}
		// 每个节点经出口的实测在 checkUserChain 里报步骤，这里给它加上是第几个节点
		pctx := jobPrefix(ctx, fmt.Sprintf("第 %d/%d 个节点（%s）· ", i+1, len(t.nodes), n.Name))
		_, probe, err := s.checkUserChain(pctx, u, chainRequest{EntryNodeID: n.ID, Exit: req.Exit}, admin)
		if err != nil {
			var verification exitVerificationError
			if errors.As(err, &verification) {
				verificationFailures = append(verificationFailures, fmt.Errorf("%s：%w", n.Name, err))
			}
			code, detail, _ := strings.Cut(err.Error(), ": ")
			failed = append(failed, gin.H{"node_id": n.ID, "name": n.Name, "code": code, "detail": detail})
			continue
		}
		ok2 = append(ok2, planned{n, probe})
	}
	if len(failed) > 0 {
		if len(verificationFailures) > 0 && s.deferExitChange(c, exitVerificationError{errors.Join(verificationFailures...)}) {
			return
		}
		s.audit(c, "proxy.type_exit", "user:"+itoa(u.ID), "failed")
		failDetail(c, 422, "type_exit_failed", "有节点经这个出口连不通，没有保存", gin.H{"nodes": failed})
		return
	}
	if len(ok2) == 0 {
		failure(c, 400, "type_exit_no_nodes")
		return
	}
	if dryRun(c, dryResult{Preview: []string{t.Label + " 的 " + itoa(len(ok2)) + " 个节点改为经这个出口；订阅里这些节点换成经出口的版本"}}) {
		return
	}
	if !jobCommit(c) {
		return
	}
	existing, err := s.policyChains(ctx, u.ID, t.Key)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	byEntry := map[int]*ent.Chain{}
	for _, ch := range existing {
		byEntry[ch.EntryNodeID] = ch
	}
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	defer tx.Rollback()
	exits := map[int]*ent.Exit{}
	previous := map[int]*ent.Exit{}
	for _, ch := range existing {
		e, eerr := tx.Exit.Get(ctx, ch.ExitID)
		if eerr != nil {
			failInternal(c, 500, "query_failed", eerr)
			return
		}
		previous[e.GateID] = e
	}
	keep := map[int]bool{}
	for _, p := range ok2 {
		e := exits[p.node.GateID]
		if e == nil {
			e, err = s.persistExit(ctx, tx.Client(), p.node.GateID, &u.ID, t.Key, req.Exit, previous[p.node.GateID])
			if err != nil {
				failInternal(c, 500, "save_failed", err)
				return
			}
			exits[p.node.GateID] = e
		}
		var ch *ent.Chain
		if prev := byEntry[p.node.ID]; prev != nil {
			ch, err = tx.Chain.UpdateOneID(prev.ID).SetExitID(e.ID).SetEnabled(true).Save(ctx)
		} else {
			ch, err = tx.Chain.Create().SetUserID(u.ID).SetEntryNodeID(p.node.ID).SetName(truncate(p.node.Name+" · 经"+exitKindLabel(req.Exit.Kind), 100)).SetExitID(e.ID).Save(ctx)
		}
		if err != nil {
			failInternal(c, 500, "save_failed", err)
			return
		}
		keep[ch.ID] = true
	}
	for _, ch := range existing {
		if !keep[ch.ID] {
			if _, err = tx.Credential.Delete().Where(credential.ChainIDEQ(ch.ID)).Exec(ctx); err == nil {
				err = tx.Chain.DeleteOneID(ch.ID).Exec(ctx)
			}
			if err != nil {
				failInternal(c, 500, "save_failed", err)
				return
			}
		}
	}
	pol := loadPolicy(u)
	pol.Revision++
	if err = tx.User.UpdateOneID(u.ID).SetProxyPolicy(policyMap(pol)).Exec(ctx); err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	if err = tx.Commit(); err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	for _, p := range ok2 {
		if e := exits[p.node.GateID]; e != nil {
			if err = s.refreshExit(ctx, e, p.probe); err != nil {
				failInternal(c, 500, "source_exit_failed", err)
				return
			}
		}
	}
	if err = s.afterChainChange(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	s.audit(c, "proxy.type_exit", "user:"+itoa(u.ID), "ok")
	u, _ = s.Store.Ent.User.Get(ctx, u.ID)
	v, err := s.proxyView(ctx, u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, v)
}

// dropPolicyChains 删除不在 keep 里的类型出口链与它们的凭据。
func (s *Server) dropPolicyChains(ctx context.Context, chains []*ent.Chain, keep map[int]bool) error {
	for _, ch := range chains {
		if keep[ch.ID] {
			continue
		}
		if _, err := s.Store.Ent.Credential.Delete().Where(credential.ChainIDEQ(ch.ID)).Exec(ctx); err != nil {
			return err
		}
		if err := s.Store.Ent.Chain.DeleteOne(ch).Exec(ctx); err != nil && !ent.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// deleteTypeExit 去掉类型出口：这类节点恢复直接出去。
func (s *Server) deleteTypeExitNow(c *gin.Context, admin bool) {
	u, ok := s.proxySubject(c, admin)
	if !ok {
		return
	}
	var req struct {
		Revision *int `json:"revision"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Revision == nil || *req.Revision != loadPolicy(u).Revision {
		failure(c, 409, "proxy_revision_conflict")
		return
	}
	ctx := c.Request.Context()
	typ := c.Param("type")
	chains, err := s.policyChains(ctx, u.ID, typ)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	if len(chains) == 0 {
		failErr(c, 404, errors.New("type_exit_not_found"))
		return
	}
	if dryRun(c, dryResult{Preview: []string{"这类节点恢复直接从 VPS 出去"}}) {
		return
	}
	if !jobCommit(c) {
		return
	}
	gates, _ := s.userGates(ctx, u.ID)
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	defer tx.Rollback()
	for _, ch := range chains {
		if _, err = tx.Credential.Delete().Where(credential.ChainIDEQ(ch.ID)).Exec(ctx); err == nil {
			err = tx.Chain.DeleteOneID(ch.ID).Exec(ctx)
		}
		if err != nil {
			failInternal(c, 500, "delete_failed", err)
			return
		}
	}
	pol := loadPolicy(u)
	pol.Revision++
	if err = tx.User.UpdateOneID(u.ID).SetProxyPolicy(policyMap(pol)).Exec(ctx); err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	if err = tx.Commit(); err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	if err = s.afterChainChange(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	if err = s.bumpGates(ctx, gates); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	u, err = s.Store.Ent.User.Get(ctx, u.ID)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	s.audit(c, "proxy.type_exit_delete", "user:"+itoa(u.ID), "ok")
	v, err := s.proxyView(ctx, u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, v)
}
