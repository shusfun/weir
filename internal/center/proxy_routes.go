package center

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/packages/contracts"
)

// 代理页的接口：管理员按用户（/admin/users/:uid/proxy）、用户本人（/me/proxy）共用同一组处理函数。

func (s *Server) proxyRoutes(auth, admin *gin.RouterGroup) {
	for _, g := range []struct {
		r     *gin.RouterGroup
		base  string
		admin bool
	}{{admin, "/users/:uid", true}, {auth, "/me", false}} {
		admin := g.admin
		g.r.GET(g.base+"/proxy", func(c *gin.Context) { s.getProxy(c, admin) })
		g.r.PUT(g.base+"/proxy", func(c *gin.Context) { s.putProxy(c, admin) })
		g.r.PUT(g.base+"/proxy/types/:type/exit", func(c *gin.Context) { s.putTypeExit(c, admin) })
		g.r.DELETE(g.base+"/proxy/types/:type/exit", func(c *gin.Context) { s.deleteTypeExit(c, admin) })
	}
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
	return s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(uid), chain.PolicyTypeEQ(typ)).Order(ent.Asc(chain.FieldID)).All(ctx)
}

func (s *Server) getProxy(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
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
	in := rankInput{prev: s.loadRankPrev(ctx, u.ID), turn: s.rankTurn(ctx, u.ID)}
	var nodeIDs []int
	for _, t := range types {
		for _, n := range t.nodes {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}
	own, all := s.userRates(ctx, u.ID), s.nodeRates(ctx, nodeIDs)
	var refs []gin.H
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
				if p, ok := s.loadExitProbe(ctx, "chain:"+itoa(ch.ID)); ok && !p.OK && h.Status != "down" {
					h = nodeHealth{Status: "down", Why: "出口实测失败"}
				}
			} else if len(chains) > 0 {
				missing = append(missing, n.Name)
			}
			if h.Status != "down" {
				online++
			}
			rate := pathRate(own, all, n.ID, chainID)
			item := gin.H{"id": n.ID, "name": n.Name, "health": h.Status, "why": h.Why, "rate": rate}
			if g != nil {
				item["gate_id"], item["gate_name"] = g.ID, g.Name
			}
			nodes = append(nodes, item)
			if !disabled[t.Key] {
				in.keys = append(in.keys, pathKey(n.ID, chainID))
				in.health, in.rates = append(in.health, h), append(in.rates, rate)
				refs = append(refs, gin.H{"node_id": n.ID, "name": n.Name, "type": t.Label, "health": h.Status, "why": h.Why, "rate": rate})
			}
		}
		v := gin.H{"key": t.Key, "label": t.Label, "enabled": !disabled[t.Key], "nodes": nodes, "online": online, "missing_exit": missing}
		if len(chains) > 0 {
			v["exit"] = s.chainExitView(ctx, chains[0])
		}
		items = append(items, v)
	}
	// 排序预览：与下一次客户端拉取时的结果相同，但不记作这次的结果（只有拉取才推进轮换、影响下一次排序）
	ranking := []gin.H{}
	for _, r := range rankEntries(in, pol.Auto.Mode, pol.Auto.Top) {
		ref := refs[r.Index]
		ref["rank"], ref["auto"], ref["exploring"] = r.Rank, r.Auto, r.Exploring
		ranking = append(ranking, ref)
	}
	sort.SliceStable(ranking, func(i, j int) bool {
		ri, rj := ranking[i]["rank"].(int), ranking[j]["rank"].(int)
		if (ri == 0) != (rj == 0) {
			return ri != 0
		}
		return ri < rj
	})
	cats := make([]gin.H, 0, len(ruleCategories))
	for _, rc := range ruleCategories {
		cats = append(cats, gin.H{"key": rc.Key, "label": rc.Label, "hint": rc.Hint, "default": rc.Default, "target": firstNonEmpty(pol.Rules[rc.Key], rc.Default)})
	}
	return gin.H{"policy": pol, "types": items, "categories": cats, "ranking": ranking, "allow_chain": u.AllowChain,
		"min_refresh": contracts.MinRefreshMinutes}, nil
}

// chainExitView 是类型出口的显示内容（与链式代理列表一致：密码给本人与管理员看原文）。
func (s *Server) chainExitView(ctx context.Context, ch *ent.Chain) gin.H {
	v := gin.H{"kind": ch.ExitKind, "address": ch.ExitAddress, "port": ch.ExitPort, "username": ch.ExitUsername, "node_id": ch.ExitNodeID}
	if pw, err := s.reveal(ch.ExitPassword); err == nil {
		v["password"] = pw
	}
	if l, err := s.reveal(ch.ExitLink); err == nil && l != "" {
		v["link"] = l
	}
	if ch.ExitKind == chain.ExitKindSource {
		v["source"] = ch.ExitSource
	}
	return v
}

type proxyRequest struct {
	Disabled []string             `json:"disabled"`
	Rules    map[string]string    `json:"rules"`
	Auto     contracts.AutoPolicy `json:"auto"`
}

// putProxy 保存类型开关、分流规则与自动选择；客户端下次更新订阅时生效。
func (s *Server) putProxy(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	var req proxyRequest
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	types, err := s.userTypes(ctx, u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	known := map[string]bool{}
	for _, t := range types {
		known[t.Key] = true
	}
	for cat, target := range req.Rules {
		rc, ok := ruleCategory(cat)
		switch {
		case !ok:
			failure(c, 400, "unknown_rule_category")
			return
		case target == "auto" || target == "select" || target == "direct" || known[target]:
		case target == "reject" && rc.Key != "final":
		default:
			failure(c, 400, "invalid_rule_target")
			return
		}
	}
	if req.Auto.Mode != "" && req.Auto.Mode != contracts.AutoLatency && req.Auto.Mode != contracts.AutoRanked {
		failure(c, 400, "invalid_auto_mode")
		return
	}
	if req.Auto.RefreshMinutes != 0 && (req.Auto.RefreshMinutes < contracts.MinRefreshMinutes || req.Auto.RefreshMinutes > 24*60) {
		failure(c, 400, "invalid_refresh")
		return
	}
	if req.Auto.Top < 0 || req.Auto.Top > 20 {
		failure(c, 400, "invalid_top")
		return
	}
	// 已不存在的类型从关闭列表里去掉；规则里写默认值的不存（以后默认值变了跟着变）
	pol := contracts.ProxyPolicy{Auto: req.Auto, Rules: map[string]string{}}
	for _, t := range dedupeStr(req.Disabled) {
		if known[t] {
			pol.Disabled = append(pol.Disabled, t)
		}
	}
	for cat, target := range req.Rules {
		if rc, _ := ruleCategory(cat); target != rc.Default {
			pol.Rules[cat] = target
		}
	}
	pol = pol.Normalized()
	if dryRun(c, dryResult{Preview: []string{"客户端下次更新订阅时按新的分组与分流生效；订阅地址不变"}}) {
		return
	}
	if u, err = u.Update().SetProxyPolicy(policyMap(pol)).Save(ctx); err != nil {
		failInternal(c, 500, "update_failed", err)
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
func (s *Server) putTypeExit(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
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
		Exit      ChainSpec `json:"exit"`
		SkipProbe bool      `json:"skip_probe"` // 仅管理员可用
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	type planned struct {
		node  *ent.Node
		probe *exitProbe
	}
	var ok2 []planned
	var failed []gin.H
	for _, n := range t.nodes {
		if req.Exit.Kind == "node" && req.Exit.NodeID == n.ID {
			continue
		}
		_, probe, err := s.checkUserChain(ctx, u, chainRequest{EntryNodeID: n.ID, Exit: req.Exit, SkipProbe: req.SkipProbe}, admin)
		if err != nil {
			code, detail, _ := strings.Cut(err.Error(), ": ")
			failed = append(failed, gin.H{"node_id": n.ID, "name": n.Name, "code": code, "detail": detail})
			continue
		}
		ok2 = append(ok2, planned{n, probe})
	}
	if len(failed) > 0 {
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
	existing, err := s.policyChains(ctx, u.ID, t.Key)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	byEntry := map[int]*ent.Chain{}
	for _, ch := range existing {
		byEntry[ch.EntryNodeID] = ch
	}
	gates, _ := s.userGates(ctx, u.ID)
	keep := map[int]bool{}
	var probes []func()
	for _, p := range ok2 {
		name := truncate(p.node.Name+" · 经"+exitLabel(req.Exit), 100)
		var id int
		if ch := byEntry[p.node.ID]; ch != nil {
			upd := ch.Update().SetName(name).SetEnabled(true)
			if err = s.saveChainExit(upd.Mutation(), req.Exit); err == nil {
				_, err = upd.Save(ctx)
			}
			id = ch.ID
		} else {
			create := s.Store.Ent.Chain.Create().SetUserID(u.ID).SetEntryNodeID(p.node.ID).SetName(name).SetPolicyType(t.Key)
			if err = s.saveChainExit(create.Mutation(), req.Exit); err == nil {
				var ch *ent.Chain
				if ch, err = create.Save(ctx); err == nil {
					id = ch.ID
				}
			}
		}
		if err != nil {
			failInternal(c, 500, "update_failed", err)
			return
		}
		keep[id] = true
		probe, owner := p.probe, "chain:"+itoa(id)
		probes = append(probes, func() { s.saveExitProbe(ctx, owner, probe) })
	}
	if err = s.dropPolicyChains(ctx, existing, keep); err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	if err = s.afterChainChange(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	_ = s.bumpGates(ctx, gates)
	for _, f := range probes {
		f()
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

func exitLabel(spec ChainSpec) string {
	switch spec.Kind {
	case "socks5", "http", "https":
		return exitKindLabels[spec.Kind]
	case "link":
		return "分享链接"
	case "source":
		return "订阅节点"
	case "node":
		return "另一节点"
	}
	return "出口"
}

var exitKindLabels = map[string]string{"socks5": "SOCKS5", "http": "HTTP 代理", "https": "HTTPS 代理"}

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
func (s *Server) deleteTypeExit(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
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
	gates, _ := s.userGates(ctx, u.ID)
	if err = s.dropPolicyChains(ctx, chains, nil); err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	if err = s.afterChainChange(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	_ = s.bumpGates(ctx, gates)
	s.audit(c, "proxy.type_exit_delete", "user:"+itoa(u.ID), "ok")
	v, err := s.proxyView(ctx, u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, v)
}
