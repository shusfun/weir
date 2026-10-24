package center

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
)

func (s *Server) gateAdminRoutes(admin *gin.RouterGroup) {
	admin.GET("/gates", s.listGates)
	admin.POST("/gates/invite", s.inviteGate)
	admin.GET("/gates/:id", s.getGate)
	admin.PATCH("/gates/:id", s.updateGate)
	admin.POST("/gates/:id/revoke", s.revokeGate)
	admin.GET("/gates/:id/config", s.previewGateConfig)
}

// gateView 区分「已生效 / 待生效 / 失败 / 离线」，离线以 90 秒未收到心跳为准。
func gateView(g *ent.Gate) gin.H {
	online := g.LastSeenAt != nil && time.Since(*g.LastSeenAt) < 90*time.Second
	state := "applied"
	switch {
	case g.Status == gate.StatusRevoked:
		state = "revoked"
	case g.FailedRevision >= g.ConfigRevision:
		state = "failed"
	case g.AppliedRevision < g.ConfigRevision:
		state = "pending"
	}
	return gin.H{
		"id": g.ID, "name": g.Name, "status": g.Status, "online": online, "last_seen_at": g.LastSeenAt,
		"public_host": g.PublicHost, "version": g.Version, "hostname": g.Hostname, "arch": g.Arch,
		"config_revision": g.ConfigRevision, "applied_revision": g.AppliedRevision, "state": state,
		"last_error": g.LastError, "kernels_running": g.KernelsRunning, "kernel_pins": g.KernelPins,
		"listening_ports": g.ListeningPorts, "host": g.Host, "created_at": g.CreatedAt,
		"xray_stats_port": g.XrayStatsPort, "singbox_stats_port": g.SingboxStatsPort,
	}
}

func (s *Server) gateParam(c *gin.Context) (*ent.Gate, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		failure(c, 400, "invalid_id")
		return nil, false
	}
	g, err := s.Store.Ent.Gate.Get(c.Request.Context(), id)
	if err != nil {
		failure(c, 404, "gate_not_found")
		return nil, false
	}
	return g, true
}

func (s *Server) listGates(c *gin.Context) {
	items, err := s.Store.Ent.Gate.Query().Order(ent.Asc(gate.FieldID)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, g := range items {
		out = append(out, gateView(g))
	}
	c.JSON(200, gin.H{"items": out})
}

func (s *Server) getGate(c *gin.Context) {
	if g, ok := s.gateParam(c); ok {
		c.JSON(200, gateView(g))
	}
}

// inviteGate 生成一次性配对凭证（1 小时）与可直接粘贴到 VPS 执行的一行命令。
func (s *Server) inviteGate(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if !bind(c, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		failure(c, 400, "invalid_name")
		return
	}
	token := randomToken(32)
	b, _ := json.Marshal(pairInvite{Name: name})
	if err := s.Store.KV.Set(c.Request.Context(), "pair:"+digest(token), string(b), time.Hour); err != nil {
		failure(c, 503, "invite_unavailable")
		return
	}
	s.audit(c, "gate.invite", name, "ok")
	command := "curl -fsSL " + publicLink(c.Request, "/install/gate.sh") + " | sh -s -- --token " + token
	c.JSON(201, gin.H{"token": token, "command": command, "expires_in": 3600})
}

func (s *Server) updateGate(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	var req struct {
		Name       *string           `json:"name"`
		PublicHost *string           `json:"public_host"`
		KernelPins map[string]string `json:"kernel_pins"`
	}
	if !bind(c, &req) {
		return
	}
	upd := g.Update().AddConfigRevision(1)
	if req.Name != nil {
		if n := strings.TrimSpace(*req.Name); n != "" && len(n) <= 100 {
			upd.SetName(n)
		}
	}
	if req.PublicHost != nil {
		h := strings.TrimSpace(*req.PublicHost)
		if h != "" && !validHost(h) {
			failure(c, 400, "invalid_public_host")
			return
		}
		upd.SetPublicHost(h)
	}
	if req.KernelPins != nil {
		pins := map[string]string{}
		for k, v := range req.KernelPins {
			if (k == "xray" || k == "sing-box") && strings.TrimSpace(v) != "" {
				pins[k] = strings.TrimSpace(v)
			}
		}
		upd.SetKernelPins(pins)
	}
	g, err := upd.Save(c.Request.Context())
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.waiters.notify(g.ID)
	s.audit(c, "gate.update", strconv.Itoa(g.ID), "ok")
	c.JSON(200, gateView(g))
}

// revokeGate 立即吊销设备身份：已签发的访问令牌在下一次请求时失效。
func (s *Server) revokeGate(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	g, err := g.Update().SetStatus(gate.StatusRevoked).Save(c.Request.Context())
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.waiters.notify(g.ID)
	s.audit(c, "gate.revoke", strconv.Itoa(g.ID), "ok")
	c.JSON(200, gateView(g))
}

// previewGateConfig 返回将下发给该 Gate 的配置（排查用）；编译失败时给出原因。
func (s *Server) previewGateConfig(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	cfg, err := s.compileGateConfig(c.Request.Context(), g)
	if err != nil {
		c.AbortWithStatusJSON(409, gin.H{"error": gin.H{"code": "config_unavailable", "message": err.Error()}})
		return
	}
	c.JSON(200, cfg)
}
