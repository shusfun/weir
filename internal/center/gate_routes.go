package center

import (
	"context"
	"fmt"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/outbound"
	"github.com/shusfun/weir/internal/store/ent/route"
	"github.com/shusfun/weir/packages/contracts"
)

// 生成出口的 ID 区间，避免与管理员出口（数据库 ID）冲突。
const (
	nodeChainBase  = 1_000_000_000
	userChainBase  = 1_500_000_000
	adminRouteBase = 1000
	nodeRouteBase  = 1_000_000
)

// exitFor 生成「经另一节点出去」的出口：出口节点必须启用、所在 Gate 有公网地址、内部凭据已生成。
func (s *Server) exitFor(ctx context.Context, tag string, exitID int, key string) (contracts.Outbound, error) {
	exit, err := s.Store.Ent.Node.Get(ctx, exitID)
	if err != nil || !exit.Enabled {
		return contracts.Outbound{}, fmt.Errorf("出口节点 %d 不可用", exitID)
	}
	eg, err := s.Store.Ent.Gate.Get(ctx, exit.GateID)
	if err != nil {
		return contracts.Outbound{}, err
	}
	cred, err := s.Store.Ent.Credential.Query().Where(credential.StatsKeyEQ(key), credential.NodeIDEQ(exitID)).Only(ctx)
	if err != nil {
		return contracts.Outbound{}, fmt.Errorf("出口节点 %d 的内部凭据尚未生成", exitID)
	}
	return nodeExit(tag, exit, eg.PublicHost, cred)
}

// fixedExit 生成填写的出口（SOCKS5 / HTTP / HTTPS 代理或单条节点链接）；凭据与链接是加密保存的。
func (s *Server) fixedExit(kind, address string, port int, username, sealedPassword, sealedLink string) (contracts.Outbound, error) {
	if kind == "link" {
		raw, err := s.reveal(sealedLink)
		if err != nil {
			return contracts.Outbound{}, err
		}
		o, _, err := parseExitLink(raw)
		return o, err
	}
	if _, ok := proxyKinds[kind]; !ok {
		return contracts.Outbound{}, fmt.Errorf("未知的出口类型 %s", kind)
	}
	password, err := s.reveal(sealedPassword)
	if err != nil {
		return contracts.Outbound{}, err
	}
	return proxyExit(kind, address, port, username, password), nil
}

// compileRoutes 生成出口与路由。优先级：用户自建链式代理（用户明确选择的出口）→ 管理员规则 → 节点链式代理（兜底）。
// 某条链式代理出口暂不可用时，只撤下该链的凭据并记入 Meta 提示，其余配置照常下发。
func (s *Server) compileRoutes(ctx context.Context, g *ent.Gate, cfg *contracts.GateConfig, nodes []*ent.Node, chainKeys map[int][]string) error {
	routeID := int64(1)
	addRoute := func(r contracts.Route) {
		r.ID = routeID
		routeID++
		cfg.Routes = append(cfg.Routes, r)
	}
	outs, err := s.Store.Ent.Outbound.Query().Where(outbound.GateIDEQ(g.ID), outbound.EnabledEQ(true)).Order(ent.Asc(outbound.FieldPosition), ent.Asc(outbound.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	for _, o := range outs {
		if o.Protocol == "source" {
			ob, warn := s.exitFromStore(ctx, "out:"+itoa(o.ID), o.Tag, int64(o.ID))
			if warn != "" {
				cfg.Meta[o.Tag] = warn
			}
			cfg.Outbounds = append(cfg.Outbounds, ob)
			continue
		}
		if o.Protocol == "link" || o.Protocol == "https" {
			ob, err := s.fixedExit(o.Protocol, o.Address, o.Port, o.Username, o.Password, o.Password)
			if err != nil {
				cfg.Meta[o.Tag] = err.Error()
				ob = contracts.Outbound{Protocol: "blackhole"}
			}
			ob.ID, ob.Tag = int64(o.ID), o.Tag
			cfg.Outbounds = append(cfg.Outbounds, ob)
			continue
		}
		password, err := s.reveal(o.Password)
		if err != nil {
			return err
		}
		cfg.Outbounds = append(cfg.Outbounds, contracts.Outbound{ID: int64(o.ID), Tag: o.Tag, Protocol: o.Protocol, Address: o.Address, Port: o.Port, Username: o.Username, Password: password, Settings: o.Settings})
	}
	drop := map[string]bool{}
	for chainID, keys := range chainKeys {
		ch, err := s.Store.Ent.Chain.Query().Where(chain.IDEQ(chainID)).Only(ctx)
		if err != nil {
			return err
		}
		tag := fmt.Sprintf("chain-%d", ch.ID)
		var ob contracts.Outbound
		switch {
		case ch.ExitKind == chain.ExitKindSource:
			var warn string
			if ob, warn = s.exitFromStore(ctx, "chain:"+itoa(ch.ID), tag, 0); warn != "" {
				cfg.Meta[tag] = warn
			}
		case ch.ExitNodeID != nil:
			ob, err = s.exitFor(ctx, tag, *ch.ExitNodeID, chainExitKey(ch.ID))
		case ch.ExitKind == chain.ExitKindNode:
			err = fmt.Errorf("链式代理 %d 没有出口", ch.ID)
		default:
			ob, err = s.fixedExit(string(ch.ExitKind), ch.ExitAddress, ch.ExitPort, ch.ExitUsername, ch.ExitPassword, ch.ExitLink)
			ob.Tag = tag
		}
		if err != nil {
			cfg.Meta[tag] = err.Error()
			for _, k := range keys {
				drop[k] = true
			}
			continue
		}
		ob.ID = int64(userChainBase + ch.ID)
		cfg.Outbounds = append(cfg.Outbounds, ob)
		for _, k := range keys {
			addRoute(contracts.Route{StatsKey: k, OutboundTag: tag, Priority: 0})
		}
	}
	if len(drop) > 0 {
		kept := cfg.Credentials[:0]
		for _, c := range cfg.Credentials {
			if !drop[c.StatsKey] {
				kept = append(kept, c)
			}
		}
		cfg.Credentials = kept
	}
	rules, err := s.Store.Ent.Route.Query().Where(route.GateIDEQ(g.ID), route.EnabledEQ(true)).All(ctx)
	if err != nil {
		return err
	}
	for _, r := range rules {
		base := contracts.Route{OutboundTag: r.OutboundTag, Domain: r.Domain, IP: r.IP, Priority: adminRouteBase + r.Priority}
		if r.NodeID != nil {
			base.InboundTag = nodeTag(*r.NodeID)
		}
		if r.UserID == nil {
			addRoute(base)
			continue
		}
		for _, c := range cfg.Credentials {
			if c.UserKey == userKey(*r.UserID) && (base.InboundTag == "" || c.InboundID == int64(*r.NodeID)) {
				rr := base
				rr.StatsKey = c.StatsKey
				addRoute(rr)
			}
		}
	}
	for _, n := range nodes {
		if n.ChainKind == "none" {
			continue
		}
		tag := fmt.Sprintf("nodechain-%d", n.ID)
		var ob contracts.Outbound
		switch {
		case n.ChainKind == "source":
			var warn string
			if ob, warn = s.exitFromStore(ctx, "node:"+itoa(n.ID), tag, 0); warn != "" {
				cfg.Meta[tag] = warn
			}
		case n.ChainNodeID != nil:
			ob, err = s.exitFor(ctx, tag, *n.ChainNodeID, nodeExitKey(n.ID))
		case n.ChainKind == "node":
			err = fmt.Errorf("节点 %d 的链式代理没有出口", n.ID)
		default:
			ob, err = s.fixedExit(string(n.ChainKind), n.ChainAddress, n.ChainPort, n.ChainUsername, n.ChainPassword, n.ChainLink)
			ob.Tag = tag
		}
		if err != nil {
			cfg.Meta[tag] = err.Error()
			continue
		}
		ob.ID = int64(nodeChainBase + n.ID)
		cfg.Outbounds = append(cfg.Outbounds, ob)
		addRoute(contracts.Route{InboundTag: nodeTag(n.ID), OutboundTag: tag, Priority: nodeRouteBase})
	}
	return nil
}
