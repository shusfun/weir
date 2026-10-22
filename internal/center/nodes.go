package center

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/internal/kernelconf"
	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/chain"
	"github.com/imzyb/MiGate/internal/store/ent/credential"
	"github.com/imzyb/MiGate/internal/store/ent/gate"
	"github.com/imzyb/MiGate/internal/store/ent/gateaction"
	"github.com/imzyb/MiGate/internal/store/ent/node"
	"github.com/imzyb/MiGate/internal/store/ent/route"
	"github.com/imzyb/MiGate/packages/contracts"
)

func (s *Server) nodeRoutes(admin *gin.RouterGroup) {
	admin.GET("/presets", func(c *gin.Context) { c.JSON(200, gin.H{"items": presets}) })
	admin.GET("/nodes", s.listNodes)
	admin.POST("/nodes", s.createNode)
	admin.GET("/nodes/:id", s.getNode)
	admin.PATCH("/nodes/:id", s.updateNode)
	admin.DELETE("/nodes/:id", s.deleteNode)
	admin.POST("/gates/:id/probe-reality", s.probeReality)
}

// ChainSpec 描述链式代理出口：家宽 SOCKS5 或另一台 VPS 上的节点。
type ChainSpec struct {
	Kind     string `json:"kind"` // none / socks5 / node
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	NodeID   int    `json:"node_id"`
}

var errGateOffline = errors.New("gate_offline")

// gateProbe 让 Gate 执行一次探测动作并等待结果；Gate 离线时立即失败。
func (s *Server) gateProbe(ctx context.Context, g *ent.Gate, kind string, payload map[string]any, timeout time.Duration) (map[string]any, error) {
	if g.LastSeenAt == nil || time.Since(*g.LastSeenAt) > 90*time.Second {
		return nil, errGateOffline
	}
	a, err := s.enqueueAction(ctx, g.ID, kind, payload, timeout)
	if err != nil {
		return nil, err
	}
	if a, err = s.waitAction(ctx, a.ID, timeout); err != nil {
		return nil, err
	}
	switch a.Status {
	case gateaction.StatusDone:
		return a.Result, nil
	case gateaction.StatusFailed:
		if a.Error == "" {
			a.Error = "probe_failed"
		}
		return a.Result, errors.New(a.Error)
	default:
		return nil, errors.New("probe_timeout")
	}
}

// probeReality 由 Gate 实测候选伪装站点，返回每个站点的结果与推荐目标。
func (s *Server) probeReality(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	targets := make([]any, 0, len(realityTargets))
	for _, t := range realityTargets {
		targets = append(targets, t)
	}
	res, err := s.gateProbe(c.Request.Context(), g, contracts.ActionRealityProbe, map[string]any{"targets": targets}, 90*time.Second)
	if err != nil {
		failDetail(c, 422, "reality_probe_failed", err.Error(), gin.H{"result": res})
		return
	}
	c.JSON(200, res)
}

func (s *Server) nodeView(ctx context.Context, n *ent.Node) gin.H {
	g, _ := s.Store.Ent.Gate.Get(ctx, n.GateID)
	creds, _ := s.Store.Ent.Credential.Query().Where(credential.NodeIDEQ(n.ID), credential.InternalEQ(false)).Count(ctx)
	view := gin.H{
		"id": n.ID, "gate_id": n.GateID, "name": n.Name, "preset": n.Preset, "kernel": n.Kernel, "protocol": n.Protocol,
		"port": n.Port, "network": n.Network, "security": n.Security, "settings": n.Settings, "cert_id": n.CertID,
		"enabled": n.Enabled, "credentials": creds, "created_at": n.CreatedAt,
		"chain": gin.H{"kind": n.ChainKind, "address": n.ChainAddress, "port": n.ChainPort, "username": n.ChainUsername, "node_id": n.ChainNodeID},
	}
	if g != nil {
		view["gate_name"], view["host"] = g.Name, g.PublicHost
	}
	if n.ChainPassword != "" {
		if pw, err := s.reveal(n.ChainPassword); err == nil {
			view["chain"].(gin.H)["password"] = pw
		}
	}
	return view
}

func (s *Server) listNodes(c *gin.Context) {
	q := s.Store.Ent.Node.Query().Order(ent.Asc(node.FieldGateID), ent.Asc(node.FieldID))
	if gid, err := strconv.Atoi(c.Query("gate_id")); err == nil {
		q = q.Where(node.GateIDEQ(gid))
	}
	items, err := q.All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, n := range items {
		out = append(out, s.nodeView(c.Request.Context(), n))
	}
	c.JSON(200, gin.H{"items": out})
}

func (s *Server) nodeParam(c *gin.Context) (*ent.Node, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		failure(c, 400, "invalid_id")
		return nil, false
	}
	n, err := s.Store.Ent.Node.Get(c.Request.Context(), id)
	if err != nil {
		failure(c, 404, "node_not_found")
		return nil, false
	}
	return n, true
}

func (s *Server) getNode(c *gin.Context) {
	if n, ok := s.nodeParam(c); ok {
		c.JSON(200, s.nodeView(c.Request.Context(), n))
	}
}

// checkPort 校验手动指定的端口没有被本项目其他节点或 Gate 上的其他程序占用。
func (s *Server) checkPort(ctx context.Context, g *ent.Gate, port, self int) error {
	if port < 1024 || port > 65535 {
		return errors.New("invalid_port")
	}
	nodes, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID), node.PortEQ(port)).All(ctx)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.ID != self {
			return errors.New("port_in_use")
		}
	}
	own, err := s.gateOwnPorts(ctx, g)
	if err != nil {
		return err
	}
	if port == g.XrayStatsPort || port == g.SingboxStatsPort || foreignPorts(g, own)[port] {
		return errors.New("port_in_use")
	}
	return nil
}

// validateChain 校验链式代理出口并在入口 Gate 上实测（家宽 SOCKS5 必须能握手并经它连出去）。
func (s *Server) validateChain(ctx context.Context, entry *ent.Gate, self int, spec ChainSpec, publicOnly, probe bool) error {
	switch spec.Kind {
	case "", "none":
		return nil
	case "socks5":
		if strings.TrimSpace(spec.Address) == "" || spec.Port <= 0 || spec.Port > 65535 || len(spec.Username) > 255 || len(spec.Password) > 255 {
			return errors.New("invalid_chain")
		}
		if !probe {
			return nil
		}
		_, err := s.gateProbe(ctx, entry, contracts.ActionChainProbe, map[string]any{"kind": "socks5", "address": spec.Address, "port": spec.Port, "username": spec.Username, "password": spec.Password, "public_only": publicOnly}, 30*time.Second)
		if err != nil {
			return fmt.Errorf("chain_probe_failed: %w", err)
		}
		return nil
	case "node":
		if spec.NodeID == self {
			return errors.New("chain_self")
		}
		exit, err := s.Store.Ent.Node.Get(ctx, spec.NodeID)
		if err != nil || !exit.Enabled {
			return errors.New("chain_exit_unavailable")
		}
		if p, ok := presetByID(exit.Preset); ok && !p.ChainExit {
			return errors.New("chain_exit_unsupported")
		}
		// 出口节点自身也走链时可能成环：最多追 8 跳。
		for hop, cur := 0, exit; cur.ChainKind == node.ChainKindNode && cur.ChainNodeID != nil; hop++ {
			if *cur.ChainNodeID == self || hop > 8 {
				return errors.New("chain_cycle")
			}
			if cur, err = s.Store.Ent.Node.Get(ctx, *cur.ChainNodeID); err != nil {
				break
			}
		}
		eg, err := s.Store.Ent.Gate.Get(ctx, exit.GateID)
		if err != nil || eg.PublicHost == "" {
			return errors.New("chain_exit_no_address")
		}
		if probe {
			if _, err = s.gateProbe(ctx, entry, contracts.ActionChainProbe, map[string]any{"kind": "node", "address": eg.PublicHost, "port": exit.Port, "public_only": publicOnly}, 30*time.Second); err != nil {
				return fmt.Errorf("chain_probe_failed: %w", err)
			}
		}
		return nil
	}
	return errors.New("invalid_chain")
}

func (s *Server) applyChain(m *ent.NodeMutation, spec ChainSpec) error {
	m.ClearChainNodeID()
	m.SetChainAddress("")
	m.SetChainPort(0)
	m.SetChainUsername("")
	m.SetChainPassword("")
	switch spec.Kind {
	case "socks5":
		sealed, err := s.Crypto.Encrypt(spec.Password)
		if err != nil {
			return err
		}
		if spec.Password == "" {
			sealed = ""
		}
		m.SetChainKind(node.ChainKindSocks5)
		m.SetChainAddress(strings.TrimSpace(spec.Address))
		m.SetChainPort(spec.Port)
		m.SetChainUsername(spec.Username)
		m.SetChainPassword(sealed)
	case "node":
		m.SetChainKind(node.ChainKindNode)
		m.SetChainNodeID(spec.NodeID)
	default:
		m.SetChainKind(node.ChainKindNone)
	}
	return nil
}

// editableSettings 是高级编辑可改的入站字段；身份、证书文件路径与客户端列表由系统管理。
var lockedSettings = map[string]bool{"id": true, "uuid": true, "remark": true, "protocol": true, "core": true, "port": true, "network": true, "security": true, "enabled": true, "clients": true, "tls_cert_file": true, "tls_key_file": true, "certificate_id": true}

// checkSettings 拒绝未知或系统管理的字段，并用构建器实际生成一次，确保保存后的节点能下发。
// 已下载该内核时按 Gate 实际使用的版本档案检查（协议 / 传输 / 安全层是否受支持）。
func (s *Server) checkSettings(ctx context.Context, g *ent.Gate, kernel, protocol, network, security string, port int, settings map[string]any) error {
	for k := range settings {
		if lockedSettings[k] {
			return fmt.Errorf("setting %s is managed by the system", k)
		}
	}
	b, _ := json.Marshal(settings)
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var probe db.Inbound
	if err := dec.Decode(&probe); err != nil {
		return fmt.Errorf("invalid settings: %w", err)
	}
	id, password := newSecret(protocol)
	cfg := contracts.GateConfig{
		Ports:       contracts.PortPlan{XrayStats: 1, SingboxStats: 2},
		Inbounds:    []contracts.Inbound{{ID: 1, Tag: "check", Kernel: kernel, Protocol: protocol, Port: port, Network: network, Security: security, Settings: settings}},
		Credentials: []contracts.Credential{{ID: 1, InboundID: 1, StatsKey: "check", UserKey: "u0", UUID: id, Password: password}},
	}
	if _, profiles, err := s.gateKernels(ctx, g, map[string]bool{kernel: true}); err == nil {
		cfg.Profiles = profiles
	}
	if security == "tls" {
		cfg.Inbounds[0].CertID = 1
	}
	dir, err := os.MkdirTemp("", "migate-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	// 构建器只在证书文件存在时使用托管证书；Gate 上证书先于构建安装，这里放占位文件模拟。
	for _, f := range []string{"1.crt", "1.key"} {
		if err = os.WriteFile(filepath.Join(dir, f), []byte("placeholder"), 0600); err != nil {
			return err
		}
	}
	if _, err = kernelconf.Build(cfg, kernelconf.Options{CertDir: dir, Now: time.Now()}); err != nil {
		return fmt.Errorf("config rejected: %w", err)
	}
	return nil
}

// createNode 是四步建节点：协议（预设）→ VPS → 链式代理 → 完成。伪装参数由预设与 Gate 实测决定。
func (s *Server) createNode(c *gin.Context) {
	var req struct {
		GateID        int            `json:"gate_id"`
		Preset        string         `json:"preset"`
		Name          string         `json:"name"`
		Port          int            `json:"port"`
		CertID        int            `json:"cert_id"`
		RealityTarget string         `json:"reality_target"`
		Chain         ChainSpec      `json:"chain"`
		Settings      map[string]any `json:"settings"` // 高级：覆盖预设参数
		SkipProbe     bool           `json:"skip_probe"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	g, err := s.Store.Ent.Gate.Get(ctx, req.GateID)
	if err != nil || g.Status != gate.StatusActive {
		failure(c, 404, "gate_not_found")
		return
	}
	p, ok := presetByID(req.Preset)
	if !ok {
		failure(c, 400, "unknown_preset")
		return
	}
	st, err := presetSettings(p)
	if err != nil {
		failure(c, 500, "preset_failed")
		return
	}
	for k, v := range req.Settings {
		st[k] = v
	}
	var certID *int
	switch {
	case req.CertID > 0:
		ct, err := s.Store.Ent.Certificate.Get(ctx, req.CertID)
		if err != nil {
			failure(c, 404, "cert_not_found")
			return
		}
		certID, st["tls_sni"] = &ct.ID, ct.Domain
	case p.Cert == "required":
		failure(c, 400, "cert_required")
		return
	case p.Cert == "self_signed_ok":
		domain := g.PublicHost
		if domain == "" {
			domain = "gate-" + itoa(g.ID) + ".invalid"
		}
		ct, err := s.saveCert(c, fmt.Sprintf("%s 自签", g.Name), domain, true, "", "")
		if err != nil {
			failure(c, 500, err.Error())
			return
		}
		certID, st["tls_sni"] = &ct.ID, ct.Domain
	}
	if p.Security == "reality" && nodeSetting(st, "reality_dest") == "" {
		target := strings.TrimSpace(req.RealityTarget)
		if target == "" {
			if req.SkipProbe {
				target = realityTargets[0]
			} else {
				targets := make([]any, 0, len(realityTargets))
				for _, t := range realityTargets {
					targets = append(targets, t)
				}
				res, err := s.gateProbe(ctx, g, contracts.ActionRealityProbe, map[string]any{"targets": targets}, 90*time.Second)
				best, _ := res["best"].(string)
				if err != nil || best == "" {
					failDetail(c, 422, "reality_probe_failed", fmt.Sprint(err), gin.H{"result": res})
					return
				}
				target = best
			}
		}
		if !probeHostPattern(target) {
			failure(c, 400, "invalid_reality_target")
			return
		}
		st["reality_dest"], st["reality_server_names"] = target+":443", target
	}
	if req.Port == 0 {
		if req.Port, err = s.AllocateNodePort(ctx, g); err != nil {
			failure(c, 409, "no_free_port")
			return
		}
	} else if err = s.checkPort(ctx, g, req.Port, 0); err != nil {
		failure(c, 409, err.Error())
		return
	}
	if err = s.checkSettings(ctx, g, p.Kernel, p.Protocol, p.Network, p.Security, req.Port, st); err != nil {
		failDetail(c, 400, "invalid_node_settings", err.Error(), nil)
		return
	}
	if err = s.validateChain(ctx, g, 0, req.Chain, false, !req.SkipProbe); err != nil {
		failErr(c, 422, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf("%s %s", g.Name, p.Name)
	}
	create := s.Store.Ent.Node.Create().SetGateID(g.ID).SetName(name).SetPreset(p.ID).SetKernel(p.Kernel).
		SetProtocol(p.Protocol).SetPort(req.Port).SetNetwork(p.Network).SetSecurity(p.Security).SetSettings(st).SetNillableCertID(certID)
	if err = s.applyChain(create.Mutation(), req.Chain); err != nil {
		failure(c, 500, "encrypt_failed")
		return
	}
	n, err := create.Save(ctx)
	if err != nil {
		failure(c, 409, "node_conflict")
		return
	}
	if err = s.afterNodeChange(ctx, n); err != nil {
		failure(c, 500, "sync_failed")
		return
	}
	s.audit(c, "node.create", "node:"+itoa(n.ID), "ok")
	c.JSON(200, s.nodeView(ctx, n))
}

func probeHostPattern(host string) bool {
	if len(host) == 0 || len(host) > 253 || strings.ContainsAny(host, "/:@ ") {
		return false
	}
	return strings.Contains(host, ".")
}

// afterNodeChange 同步凭据，并让引用此节点作出口的入口 Gate 重新生成出口配置。
func (s *Server) afterNodeChange(ctx context.Context, n *ent.Node) error {
	if err := s.SyncAll(ctx); err != nil {
		return err
	}
	gates := []int{n.GateID}
	deps, err := s.Store.Ent.Node.Query().Where(node.ChainNodeIDEQ(n.ID)).All(ctx)
	if err != nil {
		return err
	}
	for _, d := range deps {
		gates = append(gates, d.GateID)
	}
	chains, err := s.Store.Ent.Chain.Query().Where(chain.ExitNodeIDEQ(n.ID)).All(ctx)
	if err != nil {
		return err
	}
	for _, ch := range chains {
		if entry, err := s.Store.Ent.Node.Get(ctx, ch.EntryNodeID); err == nil {
			gates = append(gates, entry.GateID)
		}
	}
	return s.bumpGates(ctx, gates)
}

func (s *Server) updateNode(c *gin.Context) {
	n, ok := s.nodeParam(c)
	if !ok {
		return
	}
	var req struct {
		Name      *string        `json:"name"`
		Enabled   *bool          `json:"enabled"`
		Port      *int           `json:"port"`
		CertID    *int           `json:"cert_id"`
		Settings  map[string]any `json:"settings"`
		Chain     *ChainSpec     `json:"chain"`
		SkipProbe bool           `json:"skip_probe"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
	if err != nil {
		failure(c, 404, "gate_not_found")
		return
	}
	upd := n.Update()
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 100 {
			failure(c, 400, "invalid_name")
			return
		}
		upd.SetName(name)
	}
	if req.Enabled != nil {
		upd.SetEnabled(*req.Enabled)
	}
	port := n.Port
	if req.Port != nil && *req.Port != n.Port {
		if err = s.checkPort(ctx, g, *req.Port, n.ID); err != nil {
			failure(c, 409, err.Error())
			return
		}
		port = *req.Port
		upd.SetPort(port)
	}
	st := map[string]any{}
	for k, v := range n.Settings {
		st[k] = v
	}
	for k, v := range req.Settings {
		if v == nil {
			delete(st, k)
		} else {
			st[k] = v
		}
	}
	if req.CertID != nil {
		if *req.CertID == 0 {
			if n.Security == "tls" {
				failure(c, 400, "cert_required")
				return
			}
			upd.ClearCertID()
		} else {
			ct, err := s.Store.Ent.Certificate.Get(ctx, *req.CertID)
			if err != nil {
				failure(c, 404, "cert_not_found")
				return
			}
			upd.SetCertID(ct.ID)
			st["tls_sni"] = ct.Domain
		}
	}
	if err = s.checkSettings(ctx, g, n.Kernel, n.Protocol, n.Network, n.Security, port, st); err != nil {
		failDetail(c, 400, "invalid_node_settings", err.Error(), nil)
		return
	}
	upd.SetSettings(st)
	if req.Chain != nil {
		if err = s.validateChain(ctx, g, n.ID, *req.Chain, false, !req.SkipProbe); err != nil {
			failErr(c, 422, err)
			return
		}
		if err = s.applyChain(upd.Mutation(), *req.Chain); err != nil {
			failure(c, 500, "encrypt_failed")
			return
		}
	}
	if n, err = upd.Save(ctx); err != nil {
		failure(c, 409, "node_conflict")
		return
	}
	if err = s.afterNodeChange(ctx, n); err != nil {
		failure(c, 500, "sync_failed")
		return
	}
	s.audit(c, "node.update", "node:"+itoa(n.ID), "ok")
	c.JSON(200, s.nodeView(ctx, n))
}

// deleteNode 删除节点及其凭据、路由；被其他节点或用户链式代理当作出口时拒绝（force 时一并删除用户链）。
func (s *Server) deleteNode(c *gin.Context) {
	n, ok := s.nodeParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if deps, _ := s.Store.Ent.Node.Query().Where(node.ChainNodeIDEQ(n.ID)).Count(ctx); deps > 0 {
		failDetail(c, 409, "node_in_use", "", gin.H{"nodes": deps})
		return
	}
	exits, _ := s.Store.Ent.Chain.Query().Where(chain.ExitNodeIDEQ(n.ID)).Count(ctx)
	if exits > 0 && c.Query("force") != "1" {
		failDetail(c, 409, "node_in_use", "", gin.H{"chains": exits})
		return
	}
	affected := map[int]bool{}
	chains, err := s.Store.Ent.Chain.Query().Where(chain.Or(chain.EntryNodeIDEQ(n.ID), chain.ExitNodeIDEQ(n.ID))).All(ctx)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	for _, ch := range chains {
		affected[ch.UserID] = true
		if _, err = s.Store.Ent.Credential.Delete().Where(credential.ChainIDEQ(ch.ID)).Exec(ctx); err == nil {
			err = s.Store.Ent.Chain.DeleteOne(ch).Exec(ctx)
		}
		if err != nil {
			failure(c, 500, "delete_failed")
			return
		}
	}
	if _, err = s.Store.Ent.Credential.Delete().Where(credential.NodeIDEQ(n.ID)).Exec(ctx); err == nil {
		if _, err = s.Store.Ent.Route.Delete().Where(route.NodeIDEQ(n.ID)).Exec(ctx); err == nil {
			err = s.Store.Ent.Node.DeleteOne(n).Exec(ctx)
		}
	}
	if err != nil {
		failure(c, 500, "delete_failed")
		return
	}
	if err = s.SyncAll(ctx); err == nil {
		err = s.bumpGates(ctx, []int{n.GateID})
	}
	if err != nil {
		failure(c, 500, "sync_failed")
		return
	}
	s.audit(c, "node.delete", "node:"+itoa(n.ID), fmt.Sprintf("ok chains=%d", len(chains)))
	c.Status(204)
}
