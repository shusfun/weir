package center

import (
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/outbound"
	"github.com/shusfun/weir/internal/store/ent/route"
	"github.com/shusfun/weir/packages/contracts"
)

// 管理员出口与分流规则（按 Gate）：例如把流媒体域名经家宽 SOCKS5 出去。
// 规则优先级在用户自建链之后、节点链兜底之前；引用不存在的出口时整份配置不下发，所以这里先校验。

func (s *Server) routingRoutes(admin *gin.RouterGroup) {
	admin.GET("/gates/:id/outbounds", s.listOutbounds)
	admin.POST("/gates/:id/outbounds", s.saveOutbound)
	admin.PATCH("/gates/:id/outbounds/:oid", s.saveOutbound)
	admin.DELETE("/gates/:id/outbounds/:oid", s.deleteOutbound)
	admin.POST("/gates/:id/outbounds/:oid/probe", s.probeOutbound)
	admin.GET("/gates/:id/routes", s.listRoutes)
	admin.POST("/gates/:id/routes", s.saveRoute)
	admin.PATCH("/gates/:id/routes/:rid", s.saveRoute)
	admin.DELETE("/gates/:id/routes/:rid", s.deleteRoute)
}

var outboundProtocols = map[string]bool{"socks": true, "http": true, "freedom": true, "blackhole": true}

var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)

// 系统生成的出口标签前缀，管理员不能占用。
func reservedTag(tag string) bool {
	return tag == "direct" || strings.HasPrefix(tag, "chain-") || strings.HasPrefix(tag, "nodechain-") || strings.HasPrefix(tag, "node-")
}

func (s *Server) outboundView(o *ent.Outbound) gin.H {
	v := gin.H{"id": o.ID, "gate_id": o.GateID, "tag": o.Tag, "name": o.Name, "protocol": o.Protocol, "address": o.Address, "port": o.Port,
		"username": o.Username, "enabled": o.Enabled, "position": o.Position, "settings": o.Settings}
	if pw, err := s.reveal(o.Password); err == nil {
		v["password"] = pw
	}
	return v
}

func (s *Server) listOutbounds(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	items, err := s.Store.Ent.Outbound.Query().Where(outbound.GateIDEQ(g.ID)).Order(ent.Asc(outbound.FieldPosition), ent.Asc(outbound.FieldID)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, o := range items {
		out = append(out, s.outboundView(o))
	}
	c.JSON(200, gin.H{"items": out})
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

func (s *Server) saveOutbound(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	oid, ok := subParam(c, "oid")
	if !ok {
		return
	}
	var req struct {
		Tag      string `json:"tag"`
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Address  string `json:"address"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		Enabled  *bool  `json:"enabled"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	req.Tag = strings.TrimSpace(req.Tag)
	if !tagPattern.MatchString(req.Tag) || reservedTag(req.Tag) {
		failure(c, 400, "invalid_tag")
		return
	}
	if !outboundProtocols[req.Protocol] {
		failure(c, 400, "unsupported_outbound_protocol")
		return
	}
	if req.Protocol == "socks" || req.Protocol == "http" {
		host := strings.TrimSpace(req.Address)
		if host == "" || len(host) > 255 || strings.ContainsAny(host, "/ @") || req.Port < 1 || req.Port > 65535 {
			failure(c, 400, "invalid_outbound_address")
			return
		}
		if net.ParseIP(host) == nil && !strings.Contains(host, ".") {
			failure(c, 400, "invalid_outbound_address")
			return
		}
	} else {
		req.Address, req.Port, req.Username, req.Password = "", 0, "", ""
	}
	if len(req.Username) > 255 || len(req.Password) > 255 {
		failure(c, 400, "invalid_outbound_credentials")
		return
	}
	sealed := ""
	if req.Password != "" {
		var err error
		if sealed, err = s.Crypto.Encrypt(req.Password); err != nil {
			failure(c, 500, "encrypt_failed")
			return
		}
	}
	var saved *ent.Outbound
	var err error
	if oid == 0 {
		n, _ := s.Store.Ent.Outbound.Query().Where(outbound.GateIDEQ(g.ID)).Count(ctx)
		saved, err = s.Store.Ent.Outbound.Create().SetGateID(g.ID).SetTag(req.Tag).SetName(strings.TrimSpace(req.Name)).SetProtocol(req.Protocol).
			SetAddress(strings.TrimSpace(req.Address)).SetPort(req.Port).SetUsername(req.Username).SetPassword(sealed).SetPosition(n).Save(ctx)
	} else {
		old, gerr := s.Store.Ent.Outbound.Query().Where(outbound.IDEQ(oid), outbound.GateIDEQ(g.ID)).Only(ctx)
		if gerr != nil {
			failure(c, 404, "outbound_not_found")
			return
		}
		// 改名或停用时，仍被启用规则引用的出口不允许（避免规则静默失效或改成直连）。
		disabling := req.Enabled != nil && !*req.Enabled
		if old.Tag != req.Tag || disabling {
			if n, _ := s.Store.Ent.Route.Query().Where(route.GateIDEQ(g.ID), route.OutboundTagEQ(old.Tag), route.EnabledEQ(true)).Count(ctx); n > 0 {
				failure(c, 409, "outbound_in_use")
				return
			}
		}
		upd := old.Update().SetTag(req.Tag).SetName(strings.TrimSpace(req.Name)).SetProtocol(req.Protocol).SetAddress(strings.TrimSpace(req.Address)).
			SetPort(req.Port).SetUsername(req.Username).SetPassword(sealed)
		if req.Enabled != nil {
			upd.SetEnabled(*req.Enabled)
		}
		saved, err = upd.Save(ctx)
	}
	if err != nil {
		failure(c, 409, "tag_conflict")
		return
	}
	_, _ = s.bumpGate(ctx, g.ID)
	s.audit(c, "outbound.save", "outbound:"+itoa(saved.ID), "ok")
	c.JSON(200, s.outboundView(saved))
}

func (s *Server) deleteOutbound(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	oid, ok := subParam(c, "oid")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	o, err := s.Store.Ent.Outbound.Query().Where(outbound.IDEQ(oid), outbound.GateIDEQ(g.ID)).Only(ctx)
	if err != nil {
		failure(c, 404, "outbound_not_found")
		return
	}
	// 任何规则（含停用的）引用都阻止删除：先改规则。
	if n, _ := s.Store.Ent.Route.Query().Where(route.GateIDEQ(g.ID), route.OutboundTagEQ(o.Tag)).Count(ctx); n > 0 {
		failDetail(c, 409, "outbound_in_use", "", gin.H{"routes": n})
		return
	}
	if err = s.Store.Ent.Outbound.DeleteOne(o).Exec(ctx); err != nil {
		failure(c, 500, "delete_failed")
		return
	}
	_, _ = s.bumpGate(ctx, g.ID)
	s.audit(c, "outbound.delete", "outbound:"+itoa(oid), "ok")
	c.Status(204)
}

// probeOutbound 由该 Gate 实测出口（SOCKS5 握手并经它连接一个站点；HTTP 只测 TCP 可达）。
func (s *Server) probeOutbound(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	oid, ok := subParam(c, "oid")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	o, err := s.Store.Ent.Outbound.Query().Where(outbound.IDEQ(oid), outbound.GateIDEQ(g.ID)).Only(ctx)
	if err != nil || o.Address == "" {
		failure(c, 404, "outbound_not_found")
		return
	}
	pw, _ := s.reveal(o.Password)
	kind := "node"
	if o.Protocol == "socks" {
		kind = "socks5"
	}
	res, err := s.gateProbe(ctx, g, contracts.ActionOutboundProbe, map[string]any{"kind": kind, "address": o.Address, "port": o.Port, "username": o.Username, "password": pw}, 30*time.Second)
	if err != nil {
		failErr(c, 422, err)
		return
	}
	c.JSON(200, res)
}

func routeView(r *ent.Route) gin.H {
	return gin.H{"id": r.ID, "gate_id": r.GateID, "node_id": r.NodeID, "user_id": r.UserID, "outbound_tag": r.OutboundTag, "domain": r.Domain, "ip": r.IP, "enabled": r.Enabled, "priority": r.Priority}
}

func (s *Server) listRoutes(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	items, err := s.Store.Ent.Route.Query().Where(route.GateIDEQ(g.ID)).Order(ent.Asc(route.FieldPriority), ent.Asc(route.FieldID)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
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
		NodeID      *int   `json:"node_id"`
		UserID      *int   `json:"user_id"`
		OutboundTag string `json:"outbound_tag"`
		Domain      string `json:"domain"`
		IP          string `json:"ip"`
		Enabled     *bool  `json:"enabled"`
		Priority    int    `json:"priority"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	if req.OutboundTag != "direct" {
		if n, _ := s.Store.Ent.Outbound.Query().Where(outbound.GateIDEQ(g.ID), outbound.TagEQ(req.OutboundTag), outbound.EnabledEQ(true)).Count(ctx); n == 0 {
			failure(c, 400, "unknown_outbound")
			return
		}
	}
	domain, ok1 := cleanList(req.Domain)
	ip, ok2 := cleanList(req.IP)
	if !ok1 || !ok2 {
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
	var err error
	if rid == 0 {
		saved, err = s.Store.Ent.Route.Create().SetGateID(g.ID).SetNillableNodeID(req.NodeID).SetNillableUserID(req.UserID).
			SetOutboundTag(req.OutboundTag).SetDomain(domain).SetIP(ip).SetPriority(req.Priority).Save(ctx)
	} else {
		old, gerr := s.Store.Ent.Route.Query().Where(route.IDEQ(rid), route.GateIDEQ(g.ID)).Only(ctx)
		if gerr != nil {
			failure(c, 404, "route_not_found")
			return
		}
		upd := old.Update().SetOutboundTag(req.OutboundTag).SetDomain(domain).SetIP(ip).SetPriority(req.Priority).ClearNodeID().ClearUserID().
			SetNillableNodeID(req.NodeID).SetNillableUserID(req.UserID)
		if req.Enabled != nil {
			upd.SetEnabled(*req.Enabled)
		}
		saved, err = upd.Save(ctx)
	}
	if err != nil {
		failure(c, 500, "save_failed")
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
	n, err := s.Store.Ent.Route.Delete().Where(route.IDEQ(rid), route.GateIDEQ(g.ID)).Exec(ctx)
	if err != nil || n == 0 {
		failure(c, 404, "route_not_found")
		return
	}
	_, _ = s.bumpGate(ctx, g.ID)
	s.audit(c, "route.delete", "route:"+itoa(rid), "ok")
	c.Status(204)
}
