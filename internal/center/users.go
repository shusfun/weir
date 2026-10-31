package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/budget"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/passkey"
	"github.com/shusfun/weir/internal/store/ent/recoverycode"
	"github.com/shusfun/weir/internal/store/ent/user"
)

const (
	maxLinks  = 10
	maxChains = 10
)

func (s *Server) userRoutes(auth, admin *gin.RouterGroup) {
	admin.GET("/users", s.listUsers)
	admin.POST("/users", s.createUser)
	admin.GET("/users/:uid", s.getUser)
	admin.PATCH("/users/:uid", s.updateUser)
	admin.DELETE("/users/:uid", s.deleteUser)
	admin.POST("/users/:uid/reset-usage", s.resetUsageHandler)
	// 链接与自建链式代理：管理员按用户操作，普通用户只能操作自己的。
	for _, g := range []struct {
		r     *gin.RouterGroup
		base  string
		admin bool
	}{{admin, "/users/:uid", true}, {auth, "/me", false}} {
		admin := g.admin
		g.r.GET(g.base+"/links", func(c *gin.Context) { s.listLinks(c, admin) })
		g.r.POST(g.base+"/links", func(c *gin.Context) { s.createLink(c, admin) })
		g.r.PATCH(g.base+"/links/:lid", func(c *gin.Context) { s.updateLink(c, admin) })
		g.r.DELETE(g.base+"/links/:lid", func(c *gin.Context) { s.deleteLink(c, admin) })
		g.r.POST(g.base+"/links/:lid/rotate", func(c *gin.Context) { s.rotateLink(c, admin) })
		g.r.GET(g.base+"/chains", func(c *gin.Context) { s.listChains(c, admin) })
		g.r.POST(g.base+"/chains", s.asJob("新建链式代理", "已取消，链式代理没有建出来", func(c *gin.Context) { s.createChain(c, admin) }))
		g.r.PATCH(g.base+"/chains/:cid", s.asJob("修改链式代理", "已取消，链式代理保持原样", func(c *gin.Context) { s.updateChain(c, admin) }))
		g.r.DELETE(g.base+"/chains/:cid", func(c *gin.Context) { s.deleteChain(c, admin) })
	}
	auth.GET("/me/nodes", s.myNodes)
	auth.GET("/me/chain-sources", func(c *gin.Context) { s.chainSources(c, false) })
	admin.GET("/users/:uid/chain-sources", func(c *gin.Context) { s.chainSources(c, true) })
	admin.GET("/chain-sources", func(c *gin.Context) { s.chainSources(c, true) })
	auth.GET("/me/subscription", s.mySubscription)
}

func linkToken() string { return randomToken(24) }

// subject 返回本次操作的目标用户：管理员接口取路径中的用户，否则为当前登录用户。
func (s *Server) subject(c *gin.Context, admin bool) (*ent.User, bool) {
	if !admin {
		return c.MustGet("account").(*ent.User), true
	}
	id, err := strconv.Atoi(c.Param("uid"))
	if err != nil {
		failure(c, 400, "invalid_id")
		return nil, false
	}
	u, err := s.Store.Ent.User.Get(c.Request.Context(), id)
	if err != nil {
		failure(c, 404, "user_not_found")
		return nil, false
	}
	return u, true
}

func (s *Server) adminUserView(ctx context.Context, u *ent.User) gin.H {
	v := userView(u)
	v["node_ids"], v["usage_reset_at"], v["updated_at"] = u.NodeIds, u.UsageResetAt, u.UpdatedAt
	v["links"], _ = s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).Count(ctx)
	v["chains"], _ = s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(u.ID), chain.PolicyTypeEQ("")).Count(ctx)
	return v
}

func (s *Server) listUsers(c *gin.Context) {
	ctx := c.Request.Context()
	items, err := s.Store.Ent.User.Query().Order(ent.Asc(user.FieldID)).All(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, u := range items {
		out = append(out, s.adminUserView(ctx, u))
	}
	c.JSON(200, gin.H{"items": out})
}

// planRequest 是「一个窗口建用户」的套餐字段；更新时缺省字段保持不变。
type planRequest struct {
	Name         *string         `json:"name"`
	Quota        *int64          `json:"quota"`
	ExpiresAt    json.RawMessage `json:"expires_at"`
	MonthlyReset *bool           `json:"monthly_reset"`
	AllNodes     *bool           `json:"all_nodes"`
	NodeIDs      *[]int          `json:"node_ids"`
	AllowChain   *bool           `json:"allow_chain"`
	Enabled      *bool           `json:"enabled"`
}

func (s *Server) applyPlan(ctx context.Context, m *ent.UserMutation, p planRequest) error {
	if p.Name != nil {
		name := strings.TrimSpace(*p.Name)
		if len(name) > 100 {
			return errors.New("invalid_name")
		}
		m.SetName(name)
	}
	if p.Quota != nil {
		if *p.Quota < 0 {
			return errors.New("invalid_quota")
		}
		m.SetQuota(*p.Quota)
	}
	if len(p.ExpiresAt) > 0 {
		if string(p.ExpiresAt) == "null" {
			m.ClearExpiresAt()
		} else {
			var t time.Time
			if json.Unmarshal(p.ExpiresAt, &t) != nil {
				return errors.New("invalid_expires_at")
			}
			m.SetExpiresAt(t)
		}
	}
	if p.MonthlyReset != nil {
		m.SetMonthlyReset(*p.MonthlyReset)
	}
	if p.AllNodes != nil {
		m.SetAllNodes(*p.AllNodes)
	}
	if p.NodeIDs != nil {
		ids := dedupe(*p.NodeIDs)
		if len(ids) > 0 {
			n, err := s.Store.Ent.Node.Query().Where(node.IDIn(ids...)).Count(ctx)
			if err != nil || n != len(ids) {
				return errors.New("unknown_node")
			}
		}
		m.SetNodeIds(ids)
	}
	if p.AllowChain != nil {
		m.SetAllowChain(*p.AllowChain)
	}
	if p.Enabled != nil {
		m.SetEnabled(*p.Enabled)
	}
	return nil
}

func dedupe(ids []int) []int {
	seen, out := map[int]bool{}, []int{}
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// createUser 一次完成：账号、套餐（额度 / 周期 / 每月重置 / 节点）与一条默认链接。
// 未填写密码时生成一个，只在本次响应中返回。
func (s *Server) createUser(c *gin.Context) {
	var req struct {
		planRequest
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !bind(c, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !validEmail(email) {
		failure(c, 400, "invalid_email")
		return
	}
	generated := ""
	if req.Password == "" {
		generated = "Mg" + randomToken(6) + "9"
		req.Password = generated
	} else if !validAccountPassword(req.Password) {
		failure(c, 400, "weak_password")
		return
	}
	role := user.RoleUser
	if req.Role == "admin" {
		role = user.RoleAdmin
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		failInternal(c, 500, "hash_failed", err)
		return
	}
	ctx := c.Request.Context()
	create := s.Store.Ent.User.Create().SetEmail(email).SetPasswordHash(string(hash)).SetRole(role)
	if req.AllNodes == nil {
		create.SetAllNodes(req.NodeIDs == nil)
	}
	if err = s.applyPlan(ctx, create.Mutation(), req.planRequest); err != nil {
		failure(c, 400, err.Error())
		return
	}
	u, err := create.Save(ctx)
	if err != nil {
		failure(c, 409, "email_taken")
		return
	}
	if _, err = s.Store.Ent.Link.Create().SetUserID(u.ID).SetName("默认").SetToken(linkToken()).Save(ctx); err != nil {
		failInternal(c, 500, "link_failed", err)
		return
	}
	if err = s.SyncUser(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	s.audit(c, "user.create", "user:"+itoa(u.ID), "ok")
	out := s.adminUserView(ctx, u)
	if generated != "" {
		out["password"] = generated
	}
	c.JSON(200, out)
}

// getUser 返回用户全部信息：套餐、链接（含令牌与每个节点的凭据原文）、自建链式代理。
func (s *Server) getUser(c *gin.Context) {
	u, ok := s.subject(c, true)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	out := s.adminUserView(ctx, u)
	links, err := s.linkViews(c, u, true)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	chains, err := s.chainViews(ctx, u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	out["link_items"], out["chain_items"] = links, chains
	out["withheld"] = s.withheldNodes(ctx, u)
	c.JSON(200, out)
}

// withheldNodes 是因为额度没有发给这个用户的节点：WireGuard 没有按用户的流量统计，Gate 无法执行额度，
// 只给不限额度的用户。写明原因，不悄悄少发。
func (s *Server) withheldNodes(ctx context.Context, u *ent.User) []gin.H {
	out := []gin.H{}
	if u.Quota == 0 {
		return out
	}
	nodes, err := s.userNodeIDs(ctx, u)
	if err != nil {
		return out
	}
	ids := make([]int, 0, len(nodes))
	for id, n := range nodes {
		if n.Protocol == "wireguard" {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		out = append(out, gin.H{"node_id": id, "name": nodes[id].Name, "reason": "WireGuard 没有按用户的流量统计，无法执行额度，只发给不限额度的用户"})
	}
	return out
}

func (s *Server) updateUser(c *gin.Context) {
	u, ok := s.subject(c, true)
	if !ok {
		return
	}
	var req struct {
		planRequest
		Password string `json:"password"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	me := c.GetInt("user_id")
	if u.ID == me && req.Enabled != nil && !*req.Enabled {
		failure(c, 400, "cannot_disable_self")
		return
	}
	upd := u.Update()
	if err := s.applyPlan(ctx, upd.Mutation(), req.planRequest); err != nil {
		failure(c, 400, err.Error())
		return
	}
	if req.Password != "" {
		if !validAccountPassword(req.Password) {
			failure(c, 400, "weak_password")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			failInternal(c, 500, "hash_failed", err)
			return
		}
		upd.SetPasswordHash(string(hash))
	}
	u, err := upd.Save(ctx)
	if err != nil {
		failInternal(c, 500, "update_failed", err)
		return
	}
	if (req.Enabled != nil && !*req.Enabled) || req.Password != "" {
		s.dropSessions(ctx, u.ID)
	}
	if err = s.SyncUser(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	s.audit(c, "user.update", "user:"+itoa(u.ID), "ok")
	c.JSON(200, s.adminUserView(ctx, u))
}

func (s *Server) dropSessions(ctx context.Context, uid int) {
	keys, _ := s.Store.KV.Keys(ctx, sessionPrefix(uid), 5000)
	for _, k := range keys {
		_ = s.Store.KV.Del(ctx, k)
	}
}

func (s *Server) deleteUser(c *gin.Context) {
	u, ok := s.subject(c, true)
	if !ok {
		return
	}
	if u.ID == c.GetInt("user_id") {
		failure(c, 400, "cannot_delete_self")
		return
	}
	ctx := c.Request.Context()
	gates, _ := s.userGates(ctx, u.ID)
	var err error
	for _, del := range []func() error{
		func() error {
			_, e := s.Store.Ent.Credential.Delete().Where(credential.UserIDEQ(u.ID)).Exec(ctx)
			return e
		},
		func() error { _, e := s.Store.Ent.Link.Delete().Where(link.UserIDEQ(u.ID)).Exec(ctx); return e },
		func() error { _, e := s.Store.Ent.Chain.Delete().Where(chain.UserIDEQ(u.ID)).Exec(ctx); return e },
		func() error { _, e := s.Store.Ent.Budget.Delete().Where(budget.UserIDEQ(u.ID)).Exec(ctx); return e },
		func() error { _, e := s.Store.Ent.Passkey.Delete().Where(passkey.UserIDEQ(u.ID)).Exec(ctx); return e },
		func() error {
			_, e := s.Store.Ent.RecoveryCode.Delete().Where(recoverycode.UserIDEQ(u.ID)).Exec(ctx)
			return e
		},
		func() error { return s.Store.Ent.User.DeleteOne(u).Exec(ctx) },
	} {
		if err = del(); err != nil {
			failInternal(c, 500, "delete_failed", err)
			return
		}
	}
	s.dropSessions(ctx, u.ID)
	// 用户链的内部出口凭据随之失效。
	if err = s.SyncAll(ctx); err == nil {
		err = s.bumpGates(ctx, gates)
	}
	if err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	s.audit(c, "user.delete", "user:"+itoa(u.ID), "ok")
	c.Status(204)
}

// ResetUsage 清零本周期用量（各 Gate 的累计额度记录保持，额度按新剩余量重新分配）。
func (s *Server) ResetUsage(ctx context.Context, uid int, at time.Time) error {
	if err := s.Store.Ent.User.UpdateOneID(uid).SetUsed(0).SetUsageResetAt(at).Exec(ctx); err != nil {
		return err
	}
	if _, err := s.Store.Ent.Link.Update().Where(link.UserIDEQ(uid)).SetUsed(0).Save(ctx); err != nil {
		return err
	}
	return s.Rebalance(ctx, uid)
}

func (s *Server) resetUsageHandler(c *gin.Context) {
	u, ok := s.subject(c, true)
	if !ok {
		return
	}
	if dryRun(c, dryResult{Preview: []string{fmt.Sprintf("把 %s 的已用流量（现在 %.2f GB）清零，从现在重新计算", firstNonEmpty(u.Name, u.Email), float64(u.Used)/(1<<30))}}) {
		return
	}
	if err := s.ResetUsage(c.Request.Context(), u.ID, time.Now()); err != nil {
		failInternal(c, 500, "reset_failed", err)
		return
	}
	s.audit(c, "user.reset_usage", "user:"+itoa(u.ID), "ok")
	c.Status(204)
}

// nextReset 返回按月重置的下一个时间点（以周期起点的日期为锚，月底自动取当月最后一天）。
func nextReset(anchor time.Time) time.Time {
	y, m, d := anchor.Date()
	first := time.Date(y, m+1, 1, anchor.Hour(), anchor.Minute(), anchor.Second(), 0, anchor.Location())
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, anchor.Hour(), anchor.Minute(), anchor.Second(), 0, anchor.Location())
}

// ResetDueUsage 为到期的按月重置用户清零用量；由后台每分钟调用。
func (s *Server) ResetDueUsage(ctx context.Context, now time.Time) error {
	users, err := s.Store.Ent.User.Query().Where(user.MonthlyResetEQ(true)).All(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		at := nextReset(u.UsageResetAt)
		if now.Before(at) {
			continue
		}
		for next := nextReset(at); !now.Before(next); next = nextReset(at) {
			at = next
		}
		if err = s.ResetUsage(ctx, u.ID, at); err != nil {
			return err
		}
	}
	return nil
}

// myNodes 返回当前用户可用的节点（只含名称、协议与所在 VPS，不含其他用户信息）。
func (s *Server) myNodes(c *gin.Context) {
	u := c.MustGet("account").(*ent.User)
	ctx := c.Request.Context()
	nodes, err := s.userNodeIDs(ctx, u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	out := []gin.H{}
	for _, n := range sortedNodes(nodes) {
		if !n.Enabled {
			continue
		}
		g, _ := s.Store.Ent.Gate.Get(ctx, n.GateID)
		item := gin.H{"id": n.ID, "name": n.Name, "protocol": n.Protocol, "preset": n.Preset}
		if g != nil {
			item["gate_name"] = g.Name
		}
		out = append(out, item)
	}
	c.JSON(200, gin.H{"items": out, "allow_chain": u.AllowChain})
}

func sortedNodes(m map[int]*ent.Node) []*ent.Node {
	out := make([]*ent.Node, 0, len(m))
	for _, n := range m {
		out = append(out, n)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (s *Server) mySubscription(c *gin.Context) {
	u := c.MustGet("account").(*ent.User)
	links, err := s.linkViews(c, u, false)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, gin.H{"quota": u.Quota, "used": u.Used, "expires_at": u.ExpiresAt, "monthly_reset": u.MonthlyReset, "next_reset": resetHint(u), "links": links,
		"withheld": s.withheldNodes(c.Request.Context(), u)})
}

func resetHint(u *ent.User) *time.Time {
	if !u.MonthlyReset {
		return nil
	}
	t := nextReset(u.UsageResetAt)
	return &t
}

// ---- 链接 ----

func (s *Server) linkParam(c *gin.Context, u *ent.User) (*ent.Link, bool) {
	id, err := strconv.Atoi(c.Param("lid"))
	if err != nil {
		failure(c, 400, "invalid_id")
		return nil, false
	}
	l, err := s.Store.Ent.Link.Query().Where(link.IDEQ(id), link.UserIDEQ(u.ID)).Only(c.Request.Context())
	if err != nil {
		failure(c, 404, "link_not_found")
		return nil, false
	}
	return l, true
}

func (s *Server) linkViews(c *gin.Context, u *ent.User, withCreds bool) ([]gin.H, error) {
	ctx := c.Request.Context()
	links, err := s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).Order(ent.Asc(link.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gin.H, 0, len(links))
	for _, l := range links {
		v := gin.H{
			"id": l.ID, "name": l.Name, "token": l.Token, "url": s.siteOrigin(c) + "/sub/" + l.Token,
			"all_nodes": l.AllNodes, "node_ids": l.NodeIds, "chain_ids": l.ChainIds, "enabled": l.Enabled,
			"used": l.Used, "last_used_at": l.LastUsedAt, "created_at": l.CreatedAt,
			"starts_at": l.StartsAt, "ends_at": l.EndsAt, "state": windowState(u, l, time.Now()),
		}
		// 实际结束时间：链接的结束与用户到期取早的那个（用户到期更早时页面要说明是套餐到期）
		if _, end := linkWindow(u, l); end != nil {
			v["effective_ends_at"] = end
		}
		entries, err := s.linkEntries(ctx, u, l)
		if err != nil {
			return nil, err
		}
		v["nodes"] = len(entries)
		if withCreds {
			v["entries"] = entries
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) listLinks(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	items, err := s.linkViews(c, u, true)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, gin.H{"items": items})
}

type linkRequest struct {
	Name     *string         `json:"name"`
	AllNodes *bool           `json:"all_nodes"`
	NodeIDs  *[]int          `json:"node_ids"`
	ChainIDs *[]int          `json:"chain_ids"`
	Enabled  *bool           `json:"enabled"`
	StartsAt json.RawMessage `json:"starts_at"`
	EndsAt   json.RawMessage `json:"ends_at"`
}

// applyLink 只接受该用户自己可用的节点与自己的链式代理。cur 是修改前的链接（新建时为空），
// 只改开始或结束其中一个时与另一个已保存的值一起校验。
func (s *Server) applyLink(ctx context.Context, u *ent.User, cur *ent.Link, m *ent.LinkMutation, req linkRequest) error {
	start, startSet, err := optionalTime(req.StartsAt)
	if err != nil {
		return err
	}
	end, endSet, err := optionalTime(req.EndsAt)
	if err != nil {
		return err
	}
	if startSet || endSet {
		if cur != nil && !startSet {
			start = cur.StartsAt
		}
		if cur != nil && !endSet {
			end = cur.EndsAt
		}
		if err := checkLinkWindow(u, start, end); err != nil {
			return err
		}
		if start != nil {
			m.SetStartsAt(start.UTC())
		} else {
			m.ClearStartsAt()
		}
		if end != nil {
			m.SetEndsAt(end.UTC())
		} else {
			m.ClearEndsAt()
		}
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if len(name) > 100 {
			return errors.New("invalid_name")
		}
		m.SetName(name)
	}
	if req.AllNodes != nil {
		m.SetAllNodes(*req.AllNodes)
	}
	if req.NodeIDs != nil {
		ids := dedupe(*req.NodeIDs)
		nodes, err := s.userNodeIDs(ctx, u)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if nodes[id] == nil {
				return errors.New("node_not_allowed")
			}
		}
		m.SetNodeIds(ids)
	}
	if req.ChainIDs != nil {
		ids := dedupe(*req.ChainIDs)
		if len(ids) > 0 {
			n, err := s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(u.ID), chain.IDIn(ids...), chain.PolicyTypeEQ("")).Count(ctx)
			if err != nil || n != len(ids) {
				return errors.New("chain_not_allowed")
			}
		}
		m.SetChainIds(ids)
	}
	if req.Enabled != nil {
		m.SetEnabled(*req.Enabled)
	}
	return nil
}

// createLink 新链接默认包含该用户的全部节点；每个用户最多 10 条，凭据各自独立、共享用户额度。
func (s *Server) createLink(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	var req linkRequest
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	if n, _ := s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).Count(ctx); n >= maxLinks {
		failure(c, 409, "link_limit")
		return
	}
	create := s.Store.Ent.Link.Create().SetUserID(u.ID).SetToken(linkToken())
	if err := s.applyLink(ctx, u, nil, create.Mutation(), req); err != nil {
		failure(c, 400, err.Error())
		return
	}
	l, err := create.Save(ctx)
	if err != nil {
		failInternal(c, 500, "create_failed", err)
		return
	}
	if err = s.SyncUser(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	s.audit(c, "link.create", "link:"+itoa(l.ID), "ok")
	s.respondLink(c, u, l.ID)
}

func (s *Server) respondLink(c *gin.Context, u *ent.User, id int) {
	items, err := s.linkViews(c, u, true)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	for _, v := range items {
		if v["id"] == id {
			c.JSON(200, v)
			return
		}
	}
	failure(c, 404, "link_not_found")
}

func (s *Server) updateLink(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	l, ok := s.linkParam(c, u)
	if !ok {
		return
	}
	var req linkRequest
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	upd := l.Update()
	if err := s.applyLink(ctx, u, l, upd.Mutation(), req); err != nil {
		failure(c, 400, err.Error())
		return
	}
	if dryRun(c, dryResult{Preview: []string{"订阅地址不变；客户端下次更新订阅时拿到新内容"}}) {
		return
	}
	if _, err := upd.Save(ctx); err != nil {
		failInternal(c, 500, "update_failed", err)
		return
	}
	if err := s.SyncUser(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	s.audit(c, "link.update", "link:"+itoa(l.ID), "ok")
	s.respondLink(c, u, l.ID)
}

func (s *Server) deleteLink(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	l, ok := s.linkParam(c, u)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if dryRun(c, dryResult{Preview: []string{"删除 " + firstNonEmpty(u.Name, u.Email) + " 的订阅链接「" + firstNonEmpty(l.Name, "订阅") + "」：这个订阅地址与它在各节点上的凭据随即失效"}}) {
		return
	}
	if _, err := s.Store.Ent.Credential.Delete().Where(credential.LinkIDEQ(l.ID)).Exec(ctx); err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	if err := s.Store.Ent.Link.DeleteOne(l).Exec(ctx); err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	if err := s.SyncUser(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	s.audit(c, "link.delete", "link:"+itoa(l.ID), "ok")
	c.Status(204)
}

// rotateLink 更换订阅地址；credentials 为真时同时更换该链接在所有节点上的凭据（旧客户端立即失效）。
func (s *Server) rotateLink(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	l, ok := s.linkParam(c, u)
	if !ok {
		return
	}
	var req struct {
		Credentials bool `json:"credentials"`
	}
	_ = c.ShouldBindJSON(&req)
	ctx := c.Request.Context()
	if _, err := l.Update().SetToken(linkToken()).Save(ctx); err != nil {
		failInternal(c, 500, "update_failed", err)
		return
	}
	if req.Credentials {
		if _, err := s.Store.Ent.Credential.Delete().Where(credential.LinkIDEQ(l.ID)).Exec(ctx); err != nil {
			failInternal(c, 500, "update_failed", err)
			return
		}
		if err := s.SyncUser(ctx, u.ID); err != nil {
			failInternal(c, 500, "sync_failed", err)
			return
		}
	}
	s.audit(c, "link.rotate", "link:"+itoa(l.ID), "ok")
	s.respondLink(c, u, l.ID)
}

// ---- 用户自建链式代理：生成一个新节点（入口节点 → 出口），只在入口计量一次 ----

func (s *Server) chainViews(ctx context.Context, u *ent.User) ([]gin.H, error) {
	items, err := s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(u.ID), chain.PolicyTypeEQ("")).Order(ent.Asc(chain.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gin.H, 0, len(items))
	for _, ch := range items {
		v := gin.H{"id": ch.ID, "name": ch.Name, "entry_node_id": ch.EntryNodeID, "exit_kind": ch.ExitKind, "exit_address": ch.ExitAddress,
			"exit_port": ch.ExitPort, "exit_username": ch.ExitUsername, "exit_node_id": ch.ExitNodeID, "enabled": ch.Enabled, "created_at": ch.CreatedAt}
		if ch.ExitKind == chain.ExitKindSource {
			v["exit_source"], v["source"] = ch.ExitSource, s.exitView(ctx, "chain:"+itoa(ch.ID))
		}
		if pw, err := s.reveal(ch.ExitPassword); err == nil {
			v["exit_password"] = pw
		}
		if l, err := s.reveal(ch.ExitLink); err == nil && l != "" {
			v["exit_link"] = l
		}
		if p, ok := s.loadExitProbe(ctx, "chain:"+itoa(ch.ID)); ok {
			v["probe"] = p
		}
		if n, err := s.Store.Ent.Node.Get(ctx, ch.EntryNodeID); err == nil {
			v["entry_name"] = n.Name
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) listChains(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	items, err := s.chainViews(c.Request.Context(), u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, gin.H{"items": items, "allow_chain": u.AllowChain})
}

type chainRequest struct {
	Name        string    `json:"name"`
	EntryNodeID int       `json:"entry_node_id"`
	Exit        ChainSpec `json:"exit"`
	Enabled     *bool     `json:"enabled"`
	SkipProbe   bool      `json:"skip_probe"` // 仅管理员可用
}

// literalPrivate 拒绝明显的私网 / 回环字面地址；域名在 Gate 上解析后再次检查。
func literalPrivate(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && !publicAddr(ip)
}

// nonPublic4 是 Go 的 IsPrivate 不包含、但同样不是公网的 IPv4 段：运营商级 NAT 与基准测试段（本机代理的 fake-ip 常用它）。
var nonPublic4 = []*net.IPNet{{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}, {IP: net.IPv4(198, 18, 0, 0), Mask: net.CIDRMask(15, 32)}}

func publicAddr(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range nonPublic4 {
		if n.Contains(ip) {
			return false
		}
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || len(ip) == net.IPv6len && ip.To4() == nil && ip[0]&0xfe == 0xfc)
}

// checkUserChain 校验用户自建链：入口必须是该用户可用的节点；出口可以是 SOCKS5 / HTTP(S) 代理、单条节点链接、
// 分给他的订阅组里的节点或他可用的另一节点，只允许公网地址，不接受订阅地址；由入口 Gate 实测出口可用后才保存。
func (s *Server) checkUserChain(ctx context.Context, u *ent.User, req chainRequest, admin bool) (*ent.Node, *exitProbe, error) {
	nodes, err := s.userNodeIDs(ctx, u)
	if err != nil {
		return nil, nil, err
	}
	entry := nodes[req.EntryNodeID]
	if entry == nil || !entry.Enabled {
		return nil, nil, errors.New("entry_not_allowed")
	}
	switch req.Exit.Kind {
	case "socks5", "http", "https", "link", "source":
	case "node":
		if nodes[req.Exit.NodeID] == nil {
			return nil, nil, errors.New("exit_not_allowed")
		}
	default:
		return nil, nil, errors.New("invalid_chain")
	}
	g, err := s.Store.Ent.Gate.Get(ctx, entry.GateID)
	if err != nil {
		return nil, nil, err
	}
	probe, err := s.validateChain(ctx, g, entry.ID, req.Exit, true, !(admin && req.SkipProbe), chainCtx{kernel: entry.Kernel, userID: u.ID, admin: admin})
	if err != nil {
		return nil, nil, err
	}
	return entry, probe, nil
}

func (s *Server) saveChainExit(m *ent.ChainMutation, spec ChainSpec) error {
	m.SetExitAddress("")
	m.SetExitPort(0)
	m.SetExitUsername("")
	m.SetExitPassword("")
	m.SetExitLink("")
	m.ClearExitNodeID()
	m.ClearExitSource()
	switch spec.Kind {
	case "source":
		m.SetExitKind(chain.ExitKindSource)
		m.SetExitSource(spec.source().toMap())
	case "socks5", "http", "https":
		sealed, err := s.seal(spec.Password)
		if err != nil {
			return err
		}
		m.SetExitKind(chain.ExitKind(spec.Kind))
		m.SetExitAddress(strings.Trim(strings.TrimSpace(spec.Address), "[]"))
		m.SetExitPort(spec.Port)
		m.SetExitUsername(spec.Username)
		m.SetExitPassword(sealed)
	case "link":
		sealed, err := s.seal(strings.TrimSpace(spec.Link))
		if err != nil {
			return err
		}
		m.SetExitKind(chain.ExitKindLink)
		m.SetExitLink(sealed)
		if o, _, err := parseExitLink(spec.Link); err == nil {
			m.SetExitAddress(o.Address)
			m.SetExitPort(o.Port)
		}
	default:
		m.SetExitKind(chain.ExitKindNode)
		m.SetExitNodeID(spec.NodeID)
	}
	return nil
}

// chainSpecOf 还原已保存的用户链出口（只改名称或入口时按原出口重新校验）。
func (s *Server) chainSpecOf(ch *ent.Chain) ChainSpec {
	spec := ChainSpec{Kind: string(ch.ExitKind), Address: ch.ExitAddress, Port: ch.ExitPort, Username: ch.ExitUsername}
	spec.Password, _ = s.reveal(ch.ExitPassword)
	spec.Link, _ = s.reveal(ch.ExitLink)
	if ch.ExitNodeID != nil {
		spec.NodeID = *ch.ExitNodeID
	}
	if ch.ExitKind == chain.ExitKindSource {
		sp := specOf(ch.ExitSource)
		spec.SourceID, spec.ProfileID, spec.Mode, spec.Match = sp.SourceID, sp.ProfileID, sp.Mode, sp.Match
	}
	return spec
}

func (s *Server) createChain(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	if !u.AllowChain {
		failure(c, 403, "chain_disabled")
		return
	}
	var req chainRequest
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	if n, _ := s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(u.ID), chain.PolicyTypeEQ("")).Count(ctx); n >= maxChains {
		failure(c, 409, "chain_limit")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		failure(c, 400, "invalid_name")
		return
	}
	jobTitle(ctx, "新建链式代理「"+name+"」")
	entry, probe, err := s.checkUserChain(ctx, u, req, admin)
	if err != nil {
		// 校验或 Gate 实测失败也写审计：操作者、目标用户与失败结果。
		s.audit(c, "chain.create", "user:"+itoa(u.ID), "failed")
		failErr(c, 422, err)
		return
	}
	if !jobCommit(c) {
		return
	}
	create := s.Store.Ent.Chain.Create().SetUserID(u.ID).SetEntryNodeID(entry.ID).SetName(name)
	if err = s.saveChainExit(create.Mutation(), req.Exit); err != nil {
		failInternal(c, 500, "encrypt_failed", err)
		return
	}
	ch, err := create.Save(ctx)
	if err != nil {
		failInternal(c, 500, "create_failed", err)
		return
	}
	if err = s.afterChainChange(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	s.saveExitProbe(ctx, "chain:"+itoa(ch.ID), probe)
	s.audit(c, "chain.create", "chain:"+itoa(ch.ID), "ok")
	s.respondChain(c, u, ch.ID)
}

// afterChainChange 维护出口节点上的内部凭据并同步该用户的凭据。
func (s *Server) afterChainChange(ctx context.Context, uid int) error {
	var gates []int
	err := s.Store.Lock.Do(ctx, "cred-sync", func(ctx context.Context) error {
		var err error
		gates, err = s.syncInternalLocked(ctx)
		return err
	})
	if err == nil {
		err = s.bumpGates(ctx, gates)
	}
	if err == nil {
		err = s.SyncUser(ctx, uid)
	}
	// 订阅源节点出口：保存后解析一次（之后随订阅源刷新）。
	if chains, qerr := s.Store.Ent.Chain.Query().Where(chain.UserIDEQ(uid), chain.ExitKindEQ(chain.ExitKindSource)).All(ctx); qerr == nil {
		for _, ch := range chains {
			if n, nerr := s.Store.Ent.Node.Get(ctx, ch.EntryNodeID); nerr == nil {
				_ = s.refreshSourceExit(ctx, "chain:"+itoa(ch.ID), n.GateID, specOf(ch.ExitSource), true)
			}
		}
	}
	return err
}

func (s *Server) respondChain(c *gin.Context, u *ent.User, id int) {
	items, err := s.chainViews(c.Request.Context(), u)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	for _, v := range items {
		if v["id"] == id {
			c.JSON(200, v)
			return
		}
	}
	failure(c, 404, "chain_not_found")
}

func (s *Server) chainParam(c *gin.Context, u *ent.User) (*ent.Chain, bool) {
	id, err := strconv.Atoi(c.Param("cid"))
	if err != nil {
		failure(c, 400, "invalid_id")
		return nil, false
	}
	ch, err := s.Store.Ent.Chain.Query().Where(chain.IDEQ(id), chain.UserIDEQ(u.ID), chain.PolicyTypeEQ("")).Only(c.Request.Context())
	if err != nil {
		failure(c, 404, "chain_not_found")
		return nil, false
	}
	return ch, true
}

func (s *Server) updateChain(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	ch, ok := s.chainParam(c, u)
	if !ok {
		return
	}
	var req struct {
		chainRequest
		Exit *ChainSpec `json:"exit"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	jobTitle(ctx, "修改链式代理「"+ch.Name+"」")
	upd := ch.Update()
	if name := strings.TrimSpace(req.Name); name != "" {
		if len(name) > 100 {
			failure(c, 400, "invalid_name")
			return
		}
		upd.SetName(name)
	}
	if req.Enabled != nil {
		upd.SetEnabled(*req.Enabled)
	}
	if req.Exit != nil || req.EntryNodeID != 0 {
		if !u.AllowChain {
			failure(c, 403, "chain_disabled")
			return
		}
		check := chainRequest{EntryNodeID: ch.EntryNodeID, SkipProbe: req.SkipProbe}
		if req.EntryNodeID != 0 {
			check.EntryNodeID = req.EntryNodeID
		}
		if req.Exit != nil {
			check.Exit = *req.Exit
		} else {
			check.Exit = s.chainSpecOf(ch)
		}
		entry, probe, err := s.checkUserChain(ctx, u, check, admin)
		if err != nil {
			s.audit(c, "chain.update", "chain:"+itoa(ch.ID), "failed")
			failErr(c, 422, err)
			return
		}
		upd.SetEntryNodeID(entry.ID)
		defer s.saveExitProbe(ctx, "chain:"+itoa(ch.ID), probe)
		if err = s.saveChainExit(upd.Mutation(), check.Exit); err != nil {
			failInternal(c, 500, "encrypt_failed", err)
			return
		}
		// 入口变化后旧入口上的凭据由同步删除、新入口重新生成。
	}
	if !jobCommit(c) {
		return
	}
	if _, err := upd.Save(ctx); err != nil {
		failInternal(c, 500, "update_failed", err)
		return
	}
	gates, _ := s.userGates(ctx, u.ID)
	if err := s.afterChainChange(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	_ = s.bumpGates(ctx, gates)
	s.audit(c, "chain.update", "chain:"+itoa(ch.ID), "ok")
	s.respondChain(c, u, ch.ID)
}

func (s *Server) deleteChain(c *gin.Context, admin bool) {
	u, ok := s.subject(c, admin)
	if !ok {
		return
	}
	ch, ok := s.chainParam(c, u)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	gates, _ := s.userGates(ctx, u.ID)
	if _, err := s.Store.Ent.Credential.Delete().Where(credential.ChainIDEQ(ch.ID)).Exec(ctx); err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	if err := s.Store.Ent.Chain.DeleteOne(ch).Exec(ctx); err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	// 链接里引用这条链的记录一并移除。
	links, _ := s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).All(ctx)
	for _, l := range links {
		if contains(l.ChainIds, ch.ID) {
			kept := []int{}
			for _, id := range l.ChainIds {
				if id != ch.ID {
					kept = append(kept, id)
				}
			}
			_ = l.Update().SetChainIds(kept).Exec(ctx)
		}
	}
	if err := s.afterChainChange(ctx, u.ID); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	_ = s.bumpGates(ctx, gates)
	s.audit(c, "chain.delete", "chain:"+itoa(ch.ID), "ok")
	c.Status(204)
}
