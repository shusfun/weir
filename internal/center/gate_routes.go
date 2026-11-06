package center

import (
	"context"
	"fmt"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/credential"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/route"
	"github.com/shusfun/weir/packages/contracts"
)

// 生成出口的 ID 区间，避免与管理员出口（数据库 ID）冲突。
const (
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

// compileRoutes 的优先级是用户链路、管理员选择性分流、节点默认出口。
func (s *Server) compileRoutes(ctx context.Context, g *ent.Gate, cfg *contracts.GateConfig, nodes []*ent.Node, chainKeys map[int][]string) error {
	es, err := s.Store.Ent.Exit.Query().Where(entexit.GateIDEQ(g.ID), entexit.EnabledEQ(true)).Order(ent.Asc(entexit.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	byID := map[int]*ent.Exit{}
	broken := map[int]error{}
	for _, e := range es {
		byID[e.ID] = e
		if e.Tag == "direct" {
			continue
		}
		ob, err := s.compileExit(ctx, e)
		if err != nil {
			broken[e.ID] = err
			cfg.Meta[e.Tag] = err.Error()
			ob = contracts.Outbound{ID: int64(e.ID), Tag: e.Tag, Protocol: "blackhole"}
		}
		cfg.Outbounds = append(cfg.Outbounds, ob)
		if e.Kind == entexit.KindSource {
			if _, ok := s.loadExit(ctx, exitOwner(e.ID)); !ok {
				cfg.Meta[e.Tag] = "订阅源节点出口还没有解析成功，暂时丢弃经这个出口的流量"
			}
		}
	}
	add := func(r contracts.Route) { r.ID = int64(len(cfg.Routes) + 1); cfg.Routes = append(cfg.Routes, r) }
	drop := map[string]bool{}
	for _, id := range sortedKeys(chainKeys) {
		keys := chainKeys[id]
		ch, err := s.Store.Ent.Chain.Get(ctx, id)
		if err != nil {
			return err
		}
		e := byID[ch.ExitID]
		if e == nil || broken[ch.ExitID] != nil {
			cfg.Meta["chain-"+itoa(ch.ID)] = "用户链路出口不可用"
			for _, k := range keys {
				drop[k] = true
			}
			continue
		}
		for _, k := range keys {
			add(contracts.Route{StatsKey: k, OutboundTag: e.Tag, Priority: 0})
		}
	}
	if len(drop) > 0 {
		keep := cfg.Credentials[:0]
		for _, c := range cfg.Credentials {
			if !drop[c.StatsKey] {
				keep = append(keep, c)
			}
		}
		cfg.Credentials = keep
	}
	rules, err := s.Store.Ent.Route.Query().Where(route.GateIDEQ(g.ID), route.EnabledEQ(true)).Order(ent.Asc(route.FieldPriority), ent.Asc(route.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	for _, r := range rules {
		e := byID[r.ExitID]
		if e == nil {
			return fmt.Errorf("分流规则 %d 的出口不可用", r.ID)
		}
		base := contracts.Route{OutboundTag: e.Tag, Domain: r.Domain, IP: r.IP, Priority: adminRouteBase + r.Priority}
		if r.NodeID != nil {
			base.InboundTag = nodeTag(*r.NodeID)
		}
		if r.UserID == nil {
			add(base)
			continue
		}
		for _, c := range cfg.Credentials {
			if c.UserKey == userKey(*r.UserID) && (base.InboundTag == "" || c.InboundID == int64(*r.NodeID)) {
				rr := base
				rr.StatsKey = c.StatsKey
				add(rr)
			}
		}
	}
	for _, n := range nodes {
		e := byID[n.ExitID]
		if e == nil {
			return fmt.Errorf("节点 %d 的出口不可用", n.ID)
		}
		if e.Tag != "direct" {
			add(contracts.Route{InboundTag: nodeTag(n.ID), OutboundTag: e.Tag, Priority: nodeRouteBase})
		}
	}
	return nil
}
