package center

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	entnode "github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/route"
	"strconv"
	"strings"
)

func (s *Server) routingRoutes(admin *gin.RouterGroup) {
	admin.GET("/gates/:id/routes", s.listRoutes)
	admin.POST("/gates/:id/routes", s.saveRoute)
	admin.PATCH("/gates/:id/routes/:rid", s.saveRoute)
	admin.DELETE("/gates/:id/routes/:rid", s.deleteRoute)
}
func subParam(c *gin.Context, name string) (int, bool) {
	if c.Param(name) == "" {
		return 0, true
	}
	id, err := strconv.Atoi(c.Param(name))
	if err != nil || id <= 0 {
		failure(c, 400, "invalid_id")
		return 0, false
	}
	return id, true
}

func (s *Server) checkGateKernels(ctx context.Context, g *ent.Gate, spec ExitSpec) error {
	if spec.Kind == "freedom" || spec.Kind == "blackhole" {
		return nil
	}
	ex, err := s.resolveExitSpec(ctx, 0, spec, false, chainCtx{admin: true, gate: g})
	if err != nil {
		_, detail, _ := strings.Cut(err.Error(), ": ")
		return errors.New(firstNonEmpty(detail, err.Error()))
	}
	nodes, err := s.Store.Ent.Node.Query().Where(entnode.GateIDEQ(g.ID), entnode.EnabledEQ(true)).All(ctx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		if seen[n.Kernel] {
			continue
		}
		seen[n.Kernel] = true
		if err = s.checkOutboundKernel(ctx, g, n.Kernel, ex.members()); err != nil {
			return err
		}
	}
	return nil
}

func routeView(r *ent.Route) gin.H {
	return gin.H{"id": r.ID, "gate_id": r.GateID, "node_id": r.NodeID, "user_id": r.UserID, "exit_id": r.ExitID, "domain": r.Domain, "ip": r.IP, "enabled": r.Enabled, "priority": r.Priority}
}

func (s *Server) listRoutes(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	items, err := s.Store.Ent.Route.Query().Where(route.GateIDEQ(g.ID)).Order(ent.Asc(route.FieldPriority), ent.Asc(route.FieldID)).All(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, r := range items {
		out = append(out, routeView(r))
	}
	c.JSON(200, gin.H{"items": out})
}

// cleanList 规范化逗号 / 换行分隔的域名或 IP 规则；每条最长 255，最多 2000 条。
func cleanList(raw string) (string, bool) {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if len(p) > 255 || strings.ContainsAny(p, " \t\"'") {
			return "", false
		}
		out = append(out, p)
	}
	return strings.Join(out, ","), len(out) <= 2000
}

func (s *Server) saveRoute(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	rid, ok := subParam(c, "rid")
	if !ok {
		return
	}
	var req struct {
		NodeID   *int   `json:"node_id"`
		UserID   *int   `json:"user_id"`
		ExitID   int    `json:"exit_id"`
		Domain   string `json:"domain"`
		IP       string `json:"ip"`
		Enabled  *bool  `json:"enabled"`
		Priority int    `json:"priority"`
	}
	ctx := c.Request.Context()
	if rid != 0 {
		old, err := s.Store.Ent.Route.Query().Where(route.IDEQ(rid), route.GateIDEQ(g.ID)).Only(ctx)
		if err != nil {
			failure(c, 404, "route_not_found")
			return
		}
		req.NodeID, req.UserID, req.ExitID, req.Domain, req.IP, req.Priority, req.Enabled = old.NodeID, old.UserID, old.ExitID, old.Domain, old.IP, old.Priority, &old.Enabled
	}
	var raw json.RawMessage
	if !bind(c, &raw) {
		return
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		failure(c, 400, "invalid_route_match")
		return
	}
	e, err := s.Store.Ent.Exit.Get(ctx, req.ExitID)
	if err != nil || e.GateID != g.ID || e.OwnerUserID != nil || !e.Enabled {
		failure(c, 400, "exit_not_allowed")
		return
	}
	domain, ok1 := cleanList(req.Domain)
	ip, ok2 := cleanList(req.IP)
	if !ok1 || !ok2 || domain == "" && ip == "" {
		failure(c, 400, "invalid_route_match")
		return
	}
	if req.Priority < 0 || req.Priority > 100000 {
		failure(c, 400, "invalid_priority")
		return
	}
	if req.NodeID != nil {
		if n, err := s.Store.Ent.Node.Get(ctx, *req.NodeID); err != nil || n.GateID != g.ID {
			failure(c, 400, "unknown_node")
			return
		}
	}
	if req.UserID != nil {
		if _, err := s.Store.Ent.User.Get(ctx, *req.UserID); err != nil {
			failure(c, 400, "unknown_user")
			return
		}
	}
	var saved *ent.Route
	err = nil
	if rid == 0 {
		if dryRun(c, dryResult{Preview: []string{"在 " + g.Name + " 上新建分流规则：匹配的流量走 " + itoa(req.ExitID)}}) {
			return
		}
		saved, err = s.Store.Ent.Route.Create().SetGateID(g.ID).SetNillableNodeID(req.NodeID).SetNillableUserID(req.UserID).
			SetExitID(req.ExitID).SetDomain(domain).SetIP(ip).SetPriority(req.Priority).SetNillableEnabled(req.Enabled).Save(ctx)
	} else {
		old, gerr := s.Store.Ent.Route.Query().Where(route.IDEQ(rid), route.GateIDEQ(g.ID)).Only(ctx)
		if gerr != nil {
			failure(c, 404, "route_not_found")
			return
		}
		upd := old.Update().SetExitID(req.ExitID).SetDomain(domain).SetIP(ip).SetPriority(req.Priority).ClearNodeID().ClearUserID().
			SetNillableNodeID(req.NodeID).SetNillableUserID(req.UserID)
		if req.Enabled != nil {
			upd.SetEnabled(*req.Enabled)
		}
		if dryRun(c, dryResult{Preview: []string{"保存后 " + g.Name + " 重新生成配置"}}) {
			return
		}
		saved, err = upd.Save(ctx)
	}
	if err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	_, _ = s.bumpGate(ctx, g.ID)
	s.audit(c, "route.save", "route:"+itoa(saved.ID), "ok")
	c.JSON(200, routeView(saved))
}

func (s *Server) deleteRoute(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	rid, ok := subParam(c, "rid")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if c.GetBool(dryRunKey) {
		if n, _ := s.Store.Ent.Route.Query().Where(route.IDEQ(rid), route.GateIDEQ(g.ID)).Count(ctx); n == 0 {
			failure(c, 404, "route_not_found")
			return
		}
		dryRun(c, dryResult{Preview: []string{"删除 " + g.Name + " 上的这条分流规则"}})
		return
	}
	n, err := s.Store.Ent.Route.Delete().Where(route.IDEQ(rid), route.GateIDEQ(g.ID)).Exec(ctx)
	if err != nil || n == 0 {
		failure(c, 404, "route_not_found")
		return
	}
	_, _ = s.bumpGate(ctx, g.ID)
	s.audit(c, "route.delete", "route:"+itoa(rid), "ok")
	c.Status(204)
}
