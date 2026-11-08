package center

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/db"
	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/gateaction"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/route"
	"github.com/shusfun/weir/packages/contracts"
)

func (s *Server) nodeRoutes(admin *gin.RouterGroup) {
	admin.GET("/presets", func(c *gin.Context) {
		ctx := c.Request.Context()
		if id, err := strconv.Atoi(c.Query("gate_id")); err == nil && id > 0 {
			g, err := s.Store.Ent.Gate.Get(ctx, id)
			if err != nil {
				failure(c, 404, "gate_not_found")
				return
			}
			c.JSON(200, gin.H{"items": s.presetsForGate(ctx, g)})
			return
		}
		c.JSON(200, gin.H{"items": s.presets(ctx)})
	})
	admin.GET("/nodes", s.listNodes)
	admin.POST("/nodes", s.asJob("新建节点", "已取消，节点没有建出来", s.createNode))
	admin.GET("/nodes/:id", s.getNode)
	admin.PATCH("/nodes/:id", s.updateNode)
	admin.DELETE("/nodes/:id", s.deleteNode)
	admin.POST("/gates/:id/probe-reality", s.probeReality)
}

// ExitSpec 描述链式代理出口：SOCKS5 / HTTP / HTTPS 代理、单条节点链接、另一台 VPS 上的节点或订阅源里的节点。
type ExitSpec struct {
	Remark   string `json:"remark,omitempty"`
	Kind     string `json:"kind"` // freedom / blackhole / socks5 / http / https / link / node / source
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Link     string `json:"link,omitempty"` // link：一条节点链接（不接受订阅地址）
	NodeID   int    `json:"node_id"`
	// source：管理员用 source_id（任意订阅源），用户自建用 profile_id（分配给他的订阅组）；mode node / filter，match 名称或正则。
	SourceID  int    `json:"source_id,omitempty"`
	ProfileID int    `json:"profile_id,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Match     string `json:"match,omitempty"`
}

func (c ExitSpec) source() sourceSpec {
	return sourceSpec{SourceID: c.SourceID, ProfileID: c.ProfileID, Mode: c.Mode, Match: strings.TrimSpace(c.Match)}
}

// chainCtx 是校验链式代理出口时的上下文：入口节点用的内核（判断能否自动切换）、
// 用户自建时是哪个用户（只能选分给他的订阅组），管理员代建可以选任意订阅源。
type chainCtx struct {
	kernel string
	userID int
	admin  bool
	gate   *ent.Gate // 入口 VPS（订阅源出口按它的内核筛成员）
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
	res, err := s.gateProbe(c.Request.Context(), g, contracts.ActionRealityProbe, map[string]any{"targets": targets}, 50*time.Second)
	if err != nil {
		failDetail(c, 422, "reality_probe_failed", err.Error(), gin.H{"result": res})
		return
	}
	c.JSON(200, res)
}

func (s *Server) nodeView(ctx context.Context, n *ent.Node) gin.H {
	g, _ := s.Store.Ent.Gate.Get(ctx, n.GateID)
	choice := s.nodeChoice(ctx, n, g, nil)
	creds, _ := s.Store.Ent.Credential.Query().Where(credential.NodeIDEQ(n.ID), credential.InternalEQ(false), credential.ProbeParentIDIsNil()).Count(ctx)
	view := gin.H{
		"id": n.ID, "gate_id": n.GateID, "name": n.Name, "preset": n.Preset, "kernel": n.Kernel, "protocol": n.Protocol,
		"port": n.Port, "network": n.Network, "security": n.Security, "settings": n.Settings, "cert_id": n.CertID,
		"enabled": n.Enabled, "credentials": creds, "created_at": n.CreatedAt,
		"exit_id": n.ExitID,
	}
	view["display_name"], view["exit_summary"] = choice["display_name"], choice["exit_summary"]
	if e, err := s.Store.Ent.Exit.Get(ctx, n.ExitID); err == nil {
		view["exit"], _ = s.exitResourceView(ctx, e)
	}
	if g != nil {
		state, why := s.nodeReadiness(ctx, n, g)
		view["readiness"] = gin.H{"state": state, "why": why}
		view["gate_name"], view["host"] = g.Name, g.PublicHost
		view["ports"] = nodePorts(n, g)
	}
	if r, ok := s.loadReach(ctx, n.ID); ok {
		view["reach"] = r
	} else {
		view["reach"] = reachResult{Status: "unknown", Why: "还没有检查过"}
	}
	// 全部用户直连这个节点时的下载速率（近 24 小时第 90 百分位）；没有流量时 minutes 为 0，页面显示「未知」
	rates, rateErr := s.nodeRates(ctx, []int{n.ID})
	view["rate"] = rates[n.ID]
	if rateErr != nil {
		s.tasks.note("node-rates", rateErr)
		view["rate_error"] = "下载速度读取失败"
	}
	if test := s.loadNodeTest(ctx, n); test != nil {
		view["test"] = test
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
		failInternal(c, 503, "query_failed", err)
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

// resolveExitSpec 把出口描述解析成具体出口（还不实测）。self 是正在保存的节点（不能以自己为出口）。
func (s *Server) resolveExitSpec(ctx context.Context, self int, spec ExitSpec, publicOnly bool, cc chainCtx) (resolvedExit, error) {
	switch spec.Kind {
	case "socks5", "http", "https":
		if publicOnly && (literalPrivate(spec.Address) || strings.EqualFold(strings.Trim(strings.TrimSpace(spec.Address), "[]"), "localhost")) {
			return resolvedExit{}, errors.New("exit_not_public: 只能填公网地址")
		}
		if !validHostPort(spec.Address, spec.Port) || len(spec.Username) > 255 || len(spec.Password) > 255 {
			return resolvedExit{}, errors.New("invalid_chain: 请填写有效的地址与 1–65535 的端口")
		}
		o := proxyExit(spec.Kind, spec.Address, spec.Port, spec.Username, spec.Password)
		return resolvedExit{Outbound: o, Links: []string{proxyLink(spec.Kind, spec.Address, spec.Port, spec.Username, spec.Password)}}, nil
	case "link":
		o, name, err := parseExitLink(spec.Link)
		if err != nil {
			return resolvedExit{}, fmt.Errorf("invalid_link: %v", err)
		}
		if publicOnly && (literalPrivate(o.Address) || strings.EqualFold(o.Address, "localhost")) {
			return resolvedExit{}, errors.New("exit_not_public: 只能用公网地址的节点")
		}
		return resolvedExit{Outbound: o, Names: []string{name}, Links: []string{strings.TrimSpace(spec.Link)}}, nil
	case "source":
		sp := spec.source()
		if err := sp.check(); err != nil {
			return resolvedExit{}, fmt.Errorf("invalid_chain: %v", err)
		}
		if sp.SourceID > 0 && !cc.admin {
			return resolvedExit{}, errors.New("exit_not_allowed: 只能选择分配给你的订阅组里的节点")
		}
		if sp.SourceID > 0 {
			if _, err := s.Store.Ent.SubSource.Get(ctx, sp.SourceID); err != nil {
				return resolvedExit{}, errors.New("source_not_found")
			}
		} else if cc.userID > 0 {
			granted, err := s.grantedProfiles(ctx, cc.userID)
			if err != nil {
				return resolvedExit{}, err
			}
			if !granted[sp.ProfileID] {
				return resolvedExit{}, errors.New("exit_not_allowed: 这个订阅组没有分配给该用户")
			}
		}
		var kernels []string
		if cc.kernel != "" {
			kernels = []string{cc.kernel}
		} else if cc.gate != nil {
			var err error
			kernels, err = s.gateExitKernels(ctx, cc.gate.ID)
			if err != nil {
				return resolvedExit{}, err
			}
		}
		ex, err := s.resolveSpec(ctx, cc.gate, kernels, sp, publicOnly)
		if err != nil {
			return resolvedExit{}, fmt.Errorf("chain_exit_unavailable: %v", err)
		}
		return ex, nil
	case "node":
		if spec.NodeID == self {
			return resolvedExit{}, errors.New("chain_self")
		}
		exit, err := s.Store.Ent.Node.Get(ctx, spec.NodeID)
		if err != nil || !exit.Enabled {
			return resolvedExit{}, errors.New("chain_exit_unavailable")
		}
		if cc.gate != nil && cc.gate.ID == exit.GateID {
			return resolvedExit{}, errors.New("exit_same_gate")
		}
		if p, ok := s.presetByID(ctx, exit.Preset); ok && !p.ChainExit {
			return resolvedExit{}, errors.New("chain_exit_unsupported")
		}
		// 出口节点自身也走链时可能成环：最多追 8 跳。
		seen := map[int]bool{}
		for hop, cur := 0, exit; cur != nil; hop++ {
			if cur.ID == self || seen[cur.ID] || hop > 8 {
				return resolvedExit{}, errors.New("chain_cycle")
			}
			seen[cur.ID] = true
			e, err := s.Store.Ent.Exit.Get(ctx, cur.ExitID)
			if err != nil {
				return resolvedExit{}, err
			}
			if e.Kind != "node" || e.ExitNodeID == nil {
				break
			}
			cur, err = s.Store.Ent.Node.Get(ctx, *e.ExitNodeID)
			if err != nil {
				return resolvedExit{}, err
			}
		}

		eg, err := s.Store.Ent.Gate.Get(ctx, exit.GateID)
		if err != nil || eg.PublicHost == "" {
			return resolvedExit{}, errors.New("chain_exit_no_address")
		}
		// 先用占位凭据校验协议参数，实测阶段另外签发并下发短时内部凭据。
		o, err := nodeExit("", exit, eg.PublicHost, &ent.Credential{UUID: "00000000-0000-4000-8000-000000000000", Password: "probe"})
		if err != nil {
			return resolvedExit{}, errors.New("chain_exit_unsupported")
		}
		return resolvedExit{Outbound: o, Names: []string{exit.Name}, TestNodeID: exit.ID}, nil
	}
	return resolvedExit{}, errors.New("invalid_chain")
}

// validateExitSpec 校验链式代理出口、确认入口内核能连接它，并在入口 Gate 上实测（probe 为真时）。
// 返回实测结果，由调用方在保存后记到所属对象上。
func (s *Server) validateExitSpec(ctx context.Context, entry *ent.Gate, self int, spec ExitSpec, publicOnly, probe bool, cc chainCtx) (*exitProbe, error) {
	if spec.Kind == "freedom" || spec.Kind == "blackhole" {
		return nil, nil
	}
	cc.gate = entry
	ex, err := s.resolveExitSpec(ctx, self, spec, publicOnly, cc)
	if err != nil {
		return nil, err
	}
	if ex.Outbound.Protocol == "group" {
		if err = s.checkGroupSupport(ctx, entry, cc.kernel); err != nil {
			return nil, fmt.Errorf("group_unsupported: %v", err)
		}
	}
	if err = s.checkOutboundKernel(ctx, entry, cc.kernel, ex.members()); err != nil {
		return nil, fmt.Errorf("exit_unsupported: %v", err)
	}
	if !probe {
		return nil, nil
	}
	result, err := s.probeExit(ctx, entry, ex, publicOnly)
	if err != nil {
		return result, exitVerificationError{err}
	}
	return result, nil
}

// seal 加密保存的出口凭据；空值不加密。
func (s *Server) seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	return s.Crypto.Encrypt(plain)
}

// editableSettings 是高级编辑可改的入站字段；身份、证书文件路径与客户端列表由系统管理。
var lockedSettings = map[string]bool{"id": true, "uuid": true, "remark": true, "protocol": true, "core": true, "port": true, "network": true, "security": true, "enabled": true, "clients": true, "tls_cert_file": true, "tls_key_file": true, "certificate_id": true}

// checkSettings 拒绝未知或系统管理的字段，并用构建器实际生成一次，确保保存后的节点能下发。
// 已下载该内核时按 Gate 实际使用的版本档案检查（协议 / 传输 / 安全层是否受支持）。
// kernelUnavailableError：节点要用的内核在中心还拿不到（未下载、正在下载、没有驱动），与参数无关。
type kernelUnavailableError struct{ err error }

func (e kernelUnavailableError) Error() string { return e.err.Error() }
func (e kernelUnavailableError) Unwrap() error { return e.err }

// failSettings 按 checkSettings 的错误回应：内核拿不到返回 409 kernel_unavailable，其余是参数无效。
func failSettings(c *gin.Context, err error) {
	var ku kernelUnavailableError
	if errors.As(err, &ku) {
		failDetail(c, 409, "kernel_unavailable", ku.Error(), nil)
		return
	}
	failDetail(c, 400, "invalid_node_settings", err.Error(), nil)
}

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
	// 内核未下载或正在下载时没有驱动，配置必然生成失败；返回真实原因，不当作参数错误。
	_, profiles, err := s.gateKernels(ctx, g, map[string]bool{kernel: true})
	if err != nil {
		return kernelUnavailableError{err}
	}
	cfg.Drivers = profiles
	if security == "tls" {
		cfg.Inbounds[0].CertID = 1
	}
	dir, err := os.MkdirTemp("", "weir-check-")
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

// createNode 建节点：协议（预设）、VPS、出口。伪装参数由预设与 Gate 实测决定。
func (s *Server) createNodeNow(c *gin.Context) {
	var req struct {
		GateID        int            `json:"gate_id"`
		Preset        string         `json:"preset"`
		Name          string         `json:"name"`
		Port          int            `json:"port"`
		CertID        int            `json:"cert_id"`
		RealityTarget string         `json:"reality_target"`
		ExitID        int            `json:"exit_id"`
		Exit          *ExitSpec      `json:"exit"`
		Settings      map[string]any `json:"settings"` // 高级：只能改预设列出的字段
		SkipProbe     bool           `json:"skip_probe"`
		// UserIDs 是建节点时勾选的用户：把这个节点分给他们，保存后返回他们的节点链接。
		UserIDs []int `json:"user_ids"`
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
	p, ok := s.presetByID(ctx, req.Preset)
	if !ok {
		failure(c, 400, "unknown_preset")
		return
	}
	jobTitle(ctx, "在 "+g.Name+" 上新建 "+p.Name+" 节点")
	// 没有公网地址时生成不了节点链接：先在 VPS 详情里填好，不建一个用不了的节点。
	if g.PublicHost == "" {
		failDetail(c, 422, "gate_no_public_host", "这台 VPS 还没有公网地址，请先在 VPS 详情里填写（客户端用它连接节点）", gin.H{"hint": publicHostHint(g)})
		return
	}
	st, err := presetSettings(p)
	if err != nil {
		failInternal(c, 500, "preset_failed", err)
		return
	}
	for k, v := range req.Settings {
		if !p.advancedAllowed(k) {
			failDetail(c, 400, "invalid_node_settings", "这个方案不能修改字段 "+k, nil)
			return
		}
		st[k] = v
	}
	if p.ID == "hysteria2" && st["hy2_obfs"] == "" {
		delete(st, "hy2_obfs")
		delete(st, "hy2_obfs_password")
	}
	var certID *int
	switch {
	case req.CertID > 0:
		// 高级：手动选择的证书（上传的或已有的）。
		ct, err := s.Store.Ent.Certificate.Get(ctx, req.CertID)
		if err != nil {
			failure(c, 404, "cert_not_found")
			return
		}
		if p.Cert == "required" && ct.Source == "self_signed" {
			failDetail(c, 422, "cert_untrusted", "这个协议的通用订阅需要可信证书，不能使用自签证书；请选择面板托管或上传的可信证书", nil)
			return
		}
		certID, st["tls_sni"] = &ct.ID, ct.Domain
	case p.Cert == "required" || p.Cert == "self_signed_ok":
		// 不选证书时由中心为这台 VPS 准备：自签共用一张；Trojan / WS + TLS 用这台 VPS 公网 IP 的证书。
		jobStep(ctx, "正在准备证书（"+g.Name+"）", 0)
		ct, sni, err := s.certForNode(ctx, g, p.Cert)
		if err != nil {
			var why errCertUnavailable
			if errors.As(err, &why) {
				failDetail(c, 422, "cert_unavailable", err.Error(), nil)
			} else {
				failInternal(c, 500, "cert_failed", err)
			}
			return
		}
		certID, st["tls_sni"] = &ct.ID, sni
	}
	// REALITY 目标：手动填写的只用它；否则按 VPS 实测的合适站点从快到慢，保存前的真实连接测试不通就换下一个。
	var realityCandidates []string
	if p.Security == "reality" && nodeSetting(st, "reality_dest") == "" {
		target := strings.TrimSpace(req.RealityTarget)
		switch {
		case target != "":
			realityCandidates = []string{target}
		case req.SkipProbe:
			realityCandidates = []string{realityTargets[0]}
		default:
			targets := make([]any, 0, len(realityTargets))
			for _, t := range realityTargets {
				targets = append(targets, t)
			}
			jobStep(ctx, "正在让 "+g.Name+" 实测候选伪装站点", 50*time.Second)
			res, err := s.gateProbe(ctx, g, contracts.ActionRealityProbe, map[string]any{"targets": targets}, 50*time.Second)
			if realityCandidates = suitableTargets(res); err != nil || len(realityCandidates) == 0 {
				failDetail(c, 422, "reality_probe_failed", fmt.Sprint(err), gin.H{"result": res})
				return
			}
		}
		for _, t := range realityCandidates {
			if !probeHostPattern(t) {
				failure(c, 400, "invalid_reality_target")
				return
			}
		}
		st["reality_dest"], st["reality_server_names"] = realityCandidates[0]+":443", realityCandidates[0]
	}
	autoPort := req.Port == 0
	if autoPort {
		if req.Port, err = s.allocateNodePort(ctx, g, p.Security == "reality"); err != nil {
			failure(c, 409, "no_free_port")
			return
		}
	} else if err = s.checkPort(ctx, g, req.Port, 0); err != nil {
		failure(c, 409, err.Error())
		return
	}
	if err = s.checkSettings(ctx, g, p.Kernel, p.Protocol, p.Network, p.Security, req.Port, st); err != nil {
		failSettings(c, err)
		return
	}
	selected, spec, probe, err := s.validateExitSelection(ctx, g, 0, nil, req.ExitID, req.Exit, false, true, p.Kernel)
	if err != nil {
		if s.deferExitChange(c, err) {
			return
		}
		failErr(c, 422, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf("%s %s", g.Name, p.Name)
	}
	// 保存前由 VPS 用这份参数实测真实连接（内核自检 + 本机回环下载）；
	// 例如 REALITY 目标在该网络下握手不通时直接拒绝，而不是建出一个连不上的节点。
	tested := gin.H{"done": false}
	if !req.SkipProbe {
		candidate := &ent.Node{GateID: g.ID, Kernel: p.Kernel, Protocol: p.Protocol, Port: req.Port, Network: p.Network, Security: p.Security, Settings: st, CertID: certID}
		tries := realityCandidates
		if len(tries) == 0 {
			tries = []string{""}
		} else if len(tries) > 3 {
			tries = tries[:3]
		}
		var res map[string]any
		var failed []string
		for i, t := range tries {
			if t != "" {
				st["reality_dest"], st["reality_server_names"] = t+":443", t
				jobStep(ctx, fmt.Sprintf("正在准备内核并实测节点（伪装站点 %s，第 %d/%d 个）", t, i+1, len(tries)), nodeTestTimeout)
			} else {
				jobStep(ctx, "正在准备内核并实测节点（"+g.Name+"）", nodeTestTimeout)
			}
			if res, err = s.testNode(ctx, candidate, st); err == nil {
				break
			}
			if missingMihomo(err) {
				break
			}
			failed = append(failed, strings.TrimSpace(t+" "+err.Error()))
		}
		if missingMihomo(err) {
			tested = gin.H{"done": false, "pending": true, "status": "unknown", "why": "未执行：mihomo 正在下载，完成后自动实测"}
		} else if err != nil {
			failDetail(c, 422, "node_test_failed", strings.Join(failed, "；"), gin.H{"result": res})
			return
		} else {
			tested = gin.H{"done": true, "latency_ms": res["latency_ms"], "skipped_targets": failed, "stage": res["stage"]}
		}
	}
	if c.GetBool(dryRunKey) {
		r := dryResult{Pin: map[string]any{"port": req.Port}, Preview: []string{fmt.Sprintf("在 %s 上新建 %s 节点「%s」，端口 %d", g.Name, p.Name, name, req.Port)}}
		// 自动选中的 443 不能作为手动端口写回（手动端口要 ≥ 1024）：确认时它仍空闲就再次选中，否则换一个随机端口。
		if autoPort && req.Port < 1024 {
			delete(r.Pin, "port")
			r.Preview[0] += "（确认时 443 已被占用就自动换一个端口）"
		}
		if len(realityCandidates) > 0 {
			r.Pin["reality_target"] = nodeSetting(st, "reality_server_names")
			if strings.TrimSpace(req.RealityTarget) == "" {
				r.Preview = append(r.Preview, "伪装目标 "+nodeSetting(st, "reality_server_names")+"（这台 VPS 实测候选站点后选中）")
			} else {
				r.Preview = append(r.Preview, "伪装目标 "+nodeSetting(st, "reality_server_names"))
			}
		}
		if tested["done"] == true {
			r.Result = map[string]any{"latency_ms": tested["latency_ms"], "stage": tested["stage"]}
			r.Preview = append(r.Preview, "已在这台 VPS 上用真实内核实测通过")
		}
		if certID != nil {
			r.Preview = append(r.Preview, "使用证书 "+nodeSetting(st, "tls_sni"))
		}
		dryRun(c, r)
		return
	}
	if !jobCommit(c) {
		return
	}
	jobStep(ctx, "正在保存并下发给 "+g.Name, 0)
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		failInternal(c, 500, "create_failed", err)
		return
	}
	defer tx.Rollback()
	newExit := selected == nil
	if selected == nil {
		selected, err = s.persistExit(ctx, tx.Client(), g.ID, nil, "", spec, nil)
	}
	if err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	n, err := tx.Node.Create().SetGateID(g.ID).SetName(name).SetPreset(p.ID).SetKernel(p.Kernel).SetProtocol(p.Protocol).SetPort(req.Port).SetNetwork(p.Network).SetSecurity(p.Security).SetSettings(st).SetNillableCertID(certID).SetExitID(selected.ID).Save(ctx)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failInternal(c, 409, "node_conflict", err)
		return
	}
	if newExit || probe != nil {
		if err = s.refreshExit(ctx, selected, probe); err != nil {
			failInternal(c, 500, "source_exit_failed", err)
			return
		}
	}

	// 请求里指定的用户：没有「全部节点」的用户把这个节点加进可用节点（「全部节点」的用户本来就有）。
	for _, uid := range dedupe(req.UserIDs) {
		u, err := s.Store.Ent.User.Get(ctx, uid)
		if err != nil || u.AllNodes || contains(u.NodeIds, n.ID) {
			continue
		}
		_ = u.Update().SetNodeIds(append(append([]int{}, u.NodeIds...), n.ID)).Exec(ctx)
	}
	if err = s.afterNodeChange(ctx, n); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	if tested["pending"] == true {
		if err := s.queueNodeProbe(ctx, n, c.GetInt("user_id")); err != nil {
			failInternal(c, 500, "save_failed", err)
			return
		}
	}
	go s.checkAfterApplied(context.WithoutCancel(ctx), n.ID, g.ID)
	s.audit(c, "node.create", "node:"+itoa(n.ID), "ok")
	view := s.nodeView(ctx, n)
	view["tested"] = tested
	view["shares"] = s.nodeShares(ctx, n, req.UserIDs)
	c.JSON(200, view)
}

// nodeShares 是指定用户在这个节点上的节点链接（每条包含该节点的链接一条）；拿不到时说明原因。
func (s *Server) nodeShares(ctx context.Context, n *ent.Node, uids []int) []gin.H {
	out := []gin.H{}
	for _, uid := range dedupe(uids) {
		u, err := s.Store.Ent.User.Get(ctx, uid)
		if err != nil {
			continue
		}
		item := gin.H{"user_id": u.ID, "email": u.Email, "name": u.Name, "links": []gin.H{}}
		if n.Protocol == "wireguard" && u.Quota > 0 {
			item["error"] = "WireGuard 只给不限额度的用户"
			out = append(out, item)
			continue
		}
		links, _ := s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID), link.EnabledEQ(true)).All(ctx)
		var got []gin.H
		for _, l := range links {
			entries, err := s.linkEntries(ctx, u, l)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.NodeID == n.ID && e.ChainID == 0 {
					got = append(got, gin.H{"link": firstNonEmpty(l.Name, "订阅"), "uri": e.URI, "error": e.Error})
				}
			}
		}
		if len(got) == 0 {
			item["error"] = "这个用户的链接都只选了部分节点，没有包含新节点；可以在用户详情里给链接加上"
		} else {
			item["links"] = got
		}
		out = append(out, item)
	}
	return out
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
	deps, err := s.Store.Ent.Exit.Query().Where(entexit.ExitNodeIDEQ(n.ID)).All(ctx)
	if err != nil {
		return err
	}
	for _, e := range deps {
		gates = append(gates, e.GateID)
	}
	return s.bumpGates(ctx, gates)
}

// An exit-IP observation belongs to the tested path, including downstream nodes.
// A changed downstream configuration invalidates observations of every path that uses it.
func (s *Server) invalidateExitDependents(ctx context.Context, exitID int) error {
	ns, err := s.Store.Ent.Node.Query().Where(node.ExitIDEQ(exitID)).All(ctx)
	if err != nil {
		return err
	}
	for _, n := range ns {
		if err := s.invalidateDependentExitProbes(ctx, n.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) invalidateDependentExitProbes(ctx context.Context, nodeID int) error {
	queue, seen := []int{nodeID}, map[int]bool{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		deps, err := s.Store.Ent.Exit.Query().Where(entexit.ExitNodeIDEQ(id)).All(ctx)
		if err != nil {
			return err
		}
		for _, e := range deps {
			if err := s.Store.KV.Del(ctx, exitProbeKey(exitOwner(e.ID))); err != nil {
				return err
			}
			ns, err := s.Store.Ent.Node.Query().Where(node.ExitIDEQ(e.ID)).All(ctx)
			if err != nil {
				return err
			}
			for _, n := range ns {
				queue = append(queue, n.ID)
			}
		}
	}
	return nil
}

func (s *Server) updateNodeNow(c *gin.Context) {
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
		ExitID    *int           `json:"exit_id"`
		Exit      *ExitSpec      `json:"exit"`
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
	previous := n
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
		failSettings(c, err)
		return
	}
	upd.SetSettings(st)
	var selected *ent.Exit
	var spec ExitSpec
	var probe *exitProbe
	if req.ExitID != nil || req.Exit != nil {
		id := 0
		if req.ExitID != nil {
			if *req.ExitID <= 0 {
				failure(c, 400, "invalid_exit_id")
				return
			}
			if req.Exit != nil {
				failure(c, 400, "ambiguous_exit")
				return
			}
			id = *req.ExitID
		}
		selected, spec, probe, err = s.validateExitSelection(ctx, g, n.ID, nil, id, req.Exit, id == n.ExitID, true, n.Kernel)
		if err != nil {
			if s.deferExitChange(c, err) {
				return
			}
			failErr(c, 422, err)
			return
		}
	}
	if req.Enabled != nil && *req.Enabled && req.ExitID == nil && req.Exit == nil {
		e, eerr := s.Store.Ent.Exit.Get(ctx, n.ExitID)
		if eerr != nil || !e.Enabled {
			failure(c, 409, "exit_not_allowed")
			return
		}
	}
	if dryRun(c, dryResult{Preview: []string{"修改节点「" + n.Name + "」"}}) {
		return
	}
	if !jobCommit(c) {
		return
	}
	if selected != nil || req.Exit != nil || req.ExitID != nil {
		tx, e := s.Store.Ent.Tx(ctx)
		if e != nil {
			failInternal(c, 500, "save_failed", e)
			return
		}
		defer tx.Rollback()
		newExit := selected == nil
		if selected == nil {
			selected, err = s.persistExit(ctx, tx.Client(), g.ID, nil, "", spec, nil)
		}
		if err == nil {
			upd.Mutation().SetExitID(selected.ID)
		}
		if err == nil {
			m := tx.Node.UpdateOneID(n.ID).SetName(n.Name).SetPort(port).SetSettings(st).SetExitID(selected.ID)
			if req.Name != nil {
				m.SetName(strings.TrimSpace(*req.Name))
			}
			if req.Enabled != nil {
				m.SetEnabled(*req.Enabled)
			}
			if req.CertID != nil && *req.CertID > 0 {
				m.SetCertID(*req.CertID)
			} else if req.CertID != nil {
				m.ClearCertID()
			}
			n, err = m.Save(ctx)
		}
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			failInternal(c, 500, "save_failed", err)
			return
		}
		if newExit || probe != nil {
			if err = s.refreshExit(ctx, selected, probe); err != nil {
				failInternal(c, 500, "source_exit_failed", err)
				return
			}
		}
	} else if n, err = upd.Save(ctx); err != nil {
		failure(c, 409, "node_conflict")
		return
	}
	if n.Port != previous.Port || n.ExitID != previous.ExitID || n.Enabled != previous.Enabled || !reflect.DeepEqual(n.Settings, previous.Settings) || !reflect.DeepEqual(n.CertID, previous.CertID) {
		if err = s.invalidateDependentExitProbes(ctx, n.ID); err != nil {
			failInternal(c, 500, "save_failed", err)
			return
		}
	}
	if err = s.afterNodeChange(ctx, n); err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	s.audit(c, "node.update", "node:"+itoa(n.ID), "ok")
	if n.Port != previous.Port || n.ExitID != previous.ExitID || n.Enabled != previous.Enabled || !reflect.DeepEqual(n.Settings, previous.Settings) || !reflect.DeepEqual(n.CertID, previous.CertID) {
		if _, _, e := s.gateKernels(ctx, g, map[string]bool{"mihomo": true}); e != nil && missingMihomo(e) {
			if e = s.queueNodeProbe(ctx, n, c.GetInt("user_id")); e != nil {
				failInternal(c, 500, "save_failed", e)
				return
			}
		}
		go s.checkAfterApplied(context.WithoutCancel(ctx), n.ID, g.ID)
	}
	c.JSON(200, s.nodeView(ctx, n))
}

// deleteNode 删除节点及其凭据、路由；被其他节点或用户链式代理当作出口时拒绝（force 时一并删除用户链）。
func (s *Server) deleteNode(c *gin.Context) {
	n, ok := s.nodeParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if deps, err := s.Store.Ent.Exit.Query().Where(entexit.ExitNodeIDEQ(n.ID)).Count(ctx); err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	} else if deps > 0 {
		failDetail(c, 409, "node_in_use", "先修改引用此节点的出口", gin.H{"exits": deps})
		return
	}
	chains, err := s.Store.Ent.Chain.Query().Where(chain.EntryNodeIDEQ(n.ID)).All(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	if c.GetBool(dryRunKey) {
		creds, _ := s.Store.Ent.Credential.Query().Where(credential.NodeIDEQ(n.ID), credential.InternalEQ(false), credential.ProbeParentIDIsNil()).Count(ctx)
		preview := []string{fmt.Sprintf("删除节点「%s」：%d 份用户凭据随之删除，已导入客户端的这个节点随即失效", n.Name, creds)}
		if len(chains) > 0 {
			preview = append(preview, fmt.Sprintf("一并删除以它为入口或出口的 %d 条用户自建链式代理", len(chains)))
		}
		dryRun(c, dryResult{Preview: preview})
		return
	}
	for _, ch := range chains {
		if _, err = s.Store.Ent.Credential.Delete().Where(credential.ChainIDEQ(ch.ID)).Exec(ctx); err == nil {
			err = s.Store.Ent.Chain.DeleteOne(ch).Exec(ctx)
		}
		if err != nil {
			failInternal(c, 500, "delete_failed", err)
			return
		}
	}
	if _, err = s.Store.Ent.Credential.Delete().Where(credential.NodeIDEQ(n.ID)).Exec(ctx); err == nil {
		if _, err = s.Store.Ent.Route.Delete().Where(route.NodeIDEQ(n.ID)).Exec(ctx); err == nil {
			err = s.Store.Ent.Node.DeleteOne(n).Exec(ctx)
		}
	}
	if err != nil {
		failInternal(c, 500, "delete_failed", err)
		return
	}
	if err = s.SyncAll(ctx); err == nil {
		err = s.bumpGates(ctx, []int{n.GateID})
	}
	if err != nil {
		failInternal(c, 500, "sync_failed", err)
		return
	}
	s.audit(c, "node.delete", "node:"+itoa(n.ID), fmt.Sprintf("ok chains=%d", len(chains)))
	c.Status(204)
}

// suitableTargets 取 REALITY 实测结果中合适的站点，按延迟从低到高。
func suitableTargets(res map[string]any) []string {
	list, _ := res["results"].([]any)
	type item struct {
		host string
		ms   float64
	}
	var ok []item
	for _, v := range list {
		m, _ := v.(map[string]any)
		if m["suitable"] == true {
			h, _ := m["host"].(string)
			ms, _ := m["latency_ms"].(float64)
			ok = append(ok, item{h, ms})
		}
	}
	sort.Slice(ok, func(i, j int) bool { return ok[i].ms < ok[j].ms })
	out := make([]string, 0, len(ok))
	for _, it := range ok {
		out = append(out, it.host)
	}
	return out
}
