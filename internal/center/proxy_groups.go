package center

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/packages/contracts"
)

func validProxyTarget(target string) bool { return target == "direct" || target == "proxy" }

// 只转换内存；首次实际写入时与业务改动一起保存。
func migrateProxyTargets(p contracts.ProxyPolicy) contracts.ProxyPolicy {
	if p.Version >= 3 {
		return p
	}
	old := len(p.Groups) > 0 || len(p.Custom.Entries) > 0 || p.Custom.Pending != nil || p.Custom.Source != ""
	convert := func(entries []contracts.CustomEntry) []contracts.CustomEntry {
		entries = slices.Clone(entries)
		for i := range entries {
			e := &entries[i]
			if e.Target == "reject" {
				e.Enabled = false
				e.Notice = "已不支持拒绝"
			} else if e.Target != "direct" {
				e.Target = "proxy"
			}
		}
		return entries
	}
	p.Custom.Entries = convert(p.Custom.Entries)
	if p.Custom.Pending != nil {
		pending := *p.Custom.Pending
		pending.Entries = convert(pending.Entries)
		p.Custom.Pending = &pending
	}
	for i := range p.Groups {
		if p.Groups[i].IntervalSeconds == 0 {
			p.Groups[i].IntervalSeconds = 60
		}
		if p.Groups[i].Kind == "" {
			p.Groups[i].Kind = "url-test"
		}
	}
	p.TargetMigration = nil
	if old {
		p.TargetMigration = []string{"旧代理目标已统一为走 PROXY；拒绝条目已停用；类别策略与速度选路已移除"}
	}
	p.Version = 3
	return p
}

func cloneStringMap(src map[string]string) map[string]string {
	dst := map[string]string{}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

type proxyLine struct {
	Ref        string `json:"ref"`
	Name       string `json:"name"`
	NodeID     int    `json:"node_id"`
	ChainID    int    `json:"chain_id,omitempty"`
	GateID     int    `json:"gate_id"`
	GateName   string `json:"gate_name"`
	GateStatus string `json:"gate_status"`
	Health     string `json:"health"`
	Why        string `json:"why,omitempty"`
	Available  bool   `json:"available"`
	Chained    bool   `json:"chained"`
}

func (s *Server) proxyLines(ctx context.Context, u *ent.User) ([]proxyLine, error) {
	nodes, err := s.userNodeIDs(ctx, u)
	if err != nil {
		return nil, err
	}
	chains, err := s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(u.ID)).Order(ent.Asc(chain.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	pol, ti := loadPolicy(u), s.typeIndex(ctx)
	defaults := map[int]*ent.Chain{}
	var extra []*ent.Chain
	for _, ch := range chains {
		ex, err := s.Store.Ent.Exit.Get(ctx, ch.ExitID)
		if err != nil {
			return nil, err
		}
		if ex.PolicyType != "" {
			if ch.Enabled && u.AllowChain {
				defaults[ch.EntryNodeID] = ch
			}
		} else {
			extra = append(extra, ch)
		}
	}
	lines := []proxyLine{}
	appendLine := func(n *ent.Node, ch *ent.Chain, ref string, available bool, why string) error {
		g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
		if err != nil {
			return err
		}
		if g.Status != "active" {
			available, why = false, "VPS 已撤销"
		}
		h := s.healthOf(ctx, n, g)
		if ch != nil {
			if p, ok := s.loadExitProbe(ctx, exitOwner(ch.ExitID)); ok && !p.OK {
				h = nodeHealth{Status: "down", Why: "出口实测失败"}
			}
		}
		status := "unknown"
		if g.Status != "active" {
			status = "offline"
		} else if g.LastSeenAt != nil {
			if time.Since(*g.LastSeenAt) < 90*time.Second {
				status = "online"
			} else {
				status = "offline"
			}
		}
		exitID := n.ExitID
		if ch != nil {
			exitID = ch.ExitID
		}
		ex, err := s.Store.Ent.Exit.Get(ctx, exitID)
		if err != nil {
			return err
		}
		if !ex.Enabled {
			available, why = false, "出口已停用"
		}
		v := s.nodeChoice(ctx, n, g, ch)
		line := proxyLine{Ref: ref, Name: fmt.Sprint(v["display_name"]), NodeID: n.ID, GateID: g.ID, GateName: g.Name, GateStatus: status, Health: h.Status, Why: h.Why, Available: available, Chained: ex.Kind != "freedom"}
		if ch != nil {
			line.ChainID = ch.ID
		}
		if why != "" {
			line.Why = why
		}
		lines = append(lines, line)
		return nil
	}
	for _, n := range sortedNodes(nodes) {
		available := n.Enabled && !slices.Contains(pol.Disabled, ti.of(n).Key)
		why := ""
		if !available {
			why = "节点或所属类型已停用"
		}
		if err := appendLine(n, defaults[n.ID], "node:"+itoa(n.ID), available, why); err != nil {
			return nil, err
		}
	}
	for _, ch := range extra {
		n := nodes[ch.EntryNodeID]
		if n == nil {
			continue
		}
		available := n.Enabled && ch.Enabled && u.AllowChain && !slices.Contains(pol.Disabled, ti.of(n).Key)
		why := ""
		if !available {
			why = "链式线路或入口节点已停用"
		}
		if err := appendLine(n, ch, "chain:"+itoa(ch.ID), available, why); err != nil {
			return nil, err
		}
	}
	return lines, nil
}

func proxyTargetSet(lines []proxyLine, p contracts.ProxyPolicy) map[string]bool {
	return map[string]bool{"direct": true, "proxy": true}
}

func (s *Server) proxyTargets(ctx context.Context, u *ent.User, p contracts.ProxyPolicy) (map[string]bool, error) {
	lines, err := s.proxyLines(ctx, u)
	if err != nil {
		return nil, err
	}
	return proxyTargetSet(lines, p), nil
}

func proxyGroupsView(p contracts.ProxyPolicy, lines []proxyLine) []gin.H {
	byRef := map[string]proxyLine{}
	for _, l := range lines {
		byRef[l.Ref] = l
	}
	out := []gin.H{}
	for _, g := range p.Groups {
		members := []gin.H{}
		valid := 0
		for _, ref := range g.Members {
			l, ok := byRef[ref]
			why := l.Why
			if !ok {
				why = "线路已删除或取消授权"
			}
			if ok && l.Available {
				valid++
			}
			members = append(members, gin.H{"ref": ref, "line": l, "available": ok && l.Available, "reason": why})
		}
		out = append(out, gin.H{"group": g, "members": members, "valid_members": valid, "used_by": []string{}, "effective_target": func() string {
			if valid > 0 {
				return "group:" + itoa(g.ID)
			}
			return ""
		}()})
	}
	return out
}

type proxyGroupRequest struct {
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	Members         []string `json:"members"`
	ToleranceMS     *int     `json:"tolerance_ms"`
	Revision        *int     `json:"revision"`
	IntervalSeconds int      `json:"interval_seconds"`
}

func validateProxyGroup(g contracts.ProxyGroup, p contracts.ProxyPolicy, targets map[string]bool) (string, string) {
	if g.Name == "" || utf8.RuneCountInString(g.Name) > 80 || subout.CleanLabel(g.Name) != g.Name {
		return "name", "名称不能为空，最多 80 个字，不能包含逗号、引号、换行等分隔符"
	}
	reserved := []string{"PROXY", "自动选择", "DIRECT", "REJECT"}
	for _, name := range reserved {
		if strings.EqualFold(name, g.Name) {
			return "name", "名称与内置分组重名"
		}
	}
	for _, other := range p.Groups {
		if other.ID != g.ID && strings.EqualFold(other.Name, g.Name) {
			return "name", "已有同名分组"
		}
	}
	if g.Kind != "select" && g.Kind != "url-test" && g.Kind != "fallback" {
		return "kind", "请选择分组方式"
	}
	if len(g.Members) < 1 || len(g.Members) > contracts.MaxGroupMembers {
		return "members", "请选择 1 至 50 条线路"
	}
	seen := map[string]bool{}
	for _, ref := range g.Members {
		if seen[ref] || !(strings.HasPrefix(ref, "node:") || strings.HasPrefix(ref, "chain:")) || !targets[ref] {
			return "members", "成员只能是已授权且启用的具体线路，不能重复或引用分组"
		}
		seen[ref] = true
	}
	if g.ToleranceMS < 0 || g.ToleranceMS > 10000 {
		return "tolerance_ms", "延迟容差须为 0 至 10000 毫秒"
	}
	if g.IntervalSeconds < 30 || g.IntervalSeconds > 86400 {
		return "interval_seconds", "探测间隔须为 30 至 86400 秒"
	}
	return "", ""
}

func (s *Server) writeProxyGroup(c *gin.Context, admin bool) {
	u, ok := s.proxySubject(c, admin)
	if !ok {
		return
	}
	id := 0
	var err error
	if c.Request.Method != "POST" {
		id, err = strconv.Atoi(c.Param("group"))
		if err != nil || id <= 0 {
			failure(c, 400, "invalid_id")
			return
		}
	}
	var req proxyGroupRequest
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	var result contracts.ProxyGroup
	var field, detail string
	conflict := errors.New("proxy revision conflict")
	notFound, invalid := errors.New("group not found"), errors.New("invalid group")
	err = s.Store.Lock.Do(ctx, "proxy-policy:"+itoa(u.ID), func(ctx context.Context) error {
		latest, e := s.Store.Ent.User.Get(ctx, u.ID)
		if e != nil {
			return e
		}
		p := loadPolicy(latest)
		if req.Revision == nil || *req.Revision != p.Revision {
			return conflict
		}
		i := slices.IndexFunc(p.Groups, func(g contracts.ProxyGroup) bool { return g.ID == id })
		if id > 0 && i < 0 {
			return notFound
		}
		if c.Request.Method == "DELETE" {
			result = p.Groups[i]
		} else {
			if id == 0 && len(p.Groups) >= contracts.MaxProxyGroups {
				field, detail = "name", "每个用户最多 20 个分组"
				return invalid
			}
			result = contracts.ProxyGroup{ID: id, Name: strings.TrimSpace(req.Name), Kind: req.Kind, Members: req.Members, IntervalSeconds: req.IntervalSeconds, ToleranceMS: 50}
			if result.Kind == "" {
				result.Kind = "url-test"
			}
			if req.ToleranceMS != nil {
				result.ToleranceMS = *req.ToleranceMS
			}
			if result.IntervalSeconds == 0 {
				result.IntervalSeconds = 60
			}
			lines, e := s.proxyLines(ctx, latest)
			targets := map[string]bool{}
			for _, line := range lines {
				if line.Available {
					targets[line.Ref] = true
				}
			}
			if e != nil {
				return e
			}
			field, detail = validateProxyGroup(result, p, targets)
			if field != "" {
				return invalid
			}
		}
		if dryRun(c, dryResult{Preview: []string{"分组「" + result.Name + "」；客户端下次更新订阅时生效"}}) {
			return nil
		}
		if c.Request.Method == "DELETE" {
			p.Groups = slices.Delete(p.Groups, i, i+1)
		} else if id > 0 {
			p.Groups[i] = result
		} else {
			next := max(p.NextGroupID, 1)
			for _, g := range p.Groups {
				next = max(next, g.ID+1)
			}
			result.ID = next
			p.NextGroupID = next + 1
			p.Groups = append(p.Groups, result)
		}
		p.Revision++
		u, e = latest.Update().SetProxyPolicy(policyMap(p)).Save(ctx)
		return e
	})
	if errors.Is(err, conflict) {
		failure(c, 409, "proxy_revision_conflict")
		return
	}
	if errors.Is(err, notFound) {
		failure(c, 404, "proxy_group_not_found")
		return
	}
	if errors.Is(err, invalid) {
		failDetail(c, 400, "invalid_group_"+field, detail, gin.H{"field": field})
		return
	}
	if err != nil {
		failInternal(c, 500, "update_failed", err)
		return
	}
	if c.Writer.Written() {
		return
	}
	s.audit(c, "proxy.group."+strings.ToLower(c.Request.Method), "proxy_group:"+itoa(u.ID)+":"+itoa(result.ID), "ok")
	v, err := s.proxyView(ctx, u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	v["group_id"] = result.ID
	v["id"] = result.ID
	c.JSON(200, v)
}

func (s *Server) ackProxyMigration(c *gin.Context, admin bool) {
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
	conflict := errors.New("proxy revision conflict")
	err := s.Store.Lock.Do(c.Request.Context(), "proxy-policy:"+itoa(u.ID), func(ctx context.Context) error {
		latest, err := s.Store.Ent.User.Get(ctx, u.ID)
		if err != nil {
			return err
		}
		p := loadPolicy(latest)
		if req.Revision == nil || *req.Revision != p.Revision {
			return conflict
		}
		p.TargetMigration = nil
		p.MigrationConfirmed = true
		if dryRun(c, dryResult{Preview: []string{"确认已阅读目标迁移提示"}}) {
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
	if err != nil {
		failInternal(c, 500, "update_failed", err)
		return
	}
	if c.Writer.Written() {
		return
	}
	s.audit(c, "proxy.migration.ack", "user:"+itoa(u.ID), "ok")
	c.JSON(200, gin.H{"ok": true})
}

// 普通账号访问管理员路径时也留下拒绝记录，路径参数不参与归属判断。
func (s *Server) auditProxyDenied(c *gin.Context) {
	if strings.Contains(c.FullPath(), "/proxy") {
		if u, ok := c.Get("account"); ok {
			if account, ok := u.(*ent.User); ok && string(account.Role) != "admin" {
				s.audit(c, "proxy.denied", "user:"+c.Param("uid"), "forbidden")
			}
		}
	}
	c.Next()
}
