package center

import (
	"context"
	"fmt"

	"github.com/imzyb/MiGate/internal/buildinfo"
	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/budget"
	"github.com/imzyb/MiGate/internal/store/ent/credential"
	"github.com/imzyb/MiGate/internal/store/ent/node"
	"github.com/imzyb/MiGate/packages/contracts"
)

func userKey(uid int) string { return fmt.Sprintf("u%d", uid) }

// chainExitKey / nodeExitKey 是链式代理出口节点上的系统内部凭据标识（不计入任何用户额度）。
func chainExitKey(chainID int) string { return fmt.Sprintf("chainx-%d", chainID) }
func nodeExitKey(nodeID int) string   { return fmt.Sprintf("nodex-%d", nodeID) }

// compileGateConfig 把数据库状态编译成某台 Gate 的完整期望配置。
// 停用的用户、链接、链式代理不下发凭据；到期与额度由 Gate 依据配置在本地执行。
func (s *Server) compileGateConfig(ctx context.Context, g *ent.Gate) (contracts.GateConfig, error) {
	g, err := s.ensureStatsPorts(ctx, g)
	if err != nil {
		return contracts.GateConfig{}, err
	}
	cfg := contracts.GateConfig{
		Revision: g.ConfigRevision, Version: buildinfo.Version,
		Ports:   contracts.PortPlan{XrayStats: g.XrayStatsPort, SingboxStats: g.SingboxStatsPort},
		Budgets: map[string]contracts.GateBudget{}, Meta: map[string]string{},
	}
	nodes, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID), node.EnabledEQ(true)).Order(ent.Asc(node.FieldID)).All(ctx)
	if err != nil {
		return cfg, err
	}
	need, byID, ids := map[string]bool{}, map[int]*ent.Node{}, []int{}
	for _, n := range nodes {
		in := contracts.Inbound{ID: int64(n.ID), Tag: nodeTag(n.ID), Kernel: n.Kernel, Protocol: n.Protocol, Port: n.Port, Network: n.Network, Security: n.Security, Settings: n.Settings}
		if n.CertID != nil {
			in.CertID = int64(*n.CertID)
			cfg.Certs = append(cfg.Certs, contracts.CertRef{ID: int64(*n.CertID)})
		}
		cfg.Inbounds = append(cfg.Inbounds, in)
		need[n.Kernel], byID[n.ID] = true, n
		ids = append(ids, n.ID)
	}
	chainKeys := map[int][]string{}
	if len(ids) > 0 {
		if chainKeys, err = s.addCredentials(ctx, g, &cfg, ids); err != nil {
			return cfg, err
		}
	}
	if err = s.compileRoutes(ctx, g, &cfg, nodes, chainKeys); err != nil {
		return cfg, err
	}
	if cfg.Kernels, cfg.Profiles, err = s.gateKernels(ctx, g, need); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// addCredentials 下发本 Gate 节点上的凭据与相关用户的额度；返回各自建链式代理的凭据，供生成路由。
func (s *Server) addCredentials(ctx context.Context, g *ent.Gate, cfg *contracts.GateConfig, nodeIDs []int) (map[int][]string, error) {
	creds, err := s.Store.Ent.Credential.Query().Where(credential.NodeIDIn(nodeIDs...)).Order(ent.Asc(credential.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	users, links, chains := map[int]*ent.User{}, map[int]*ent.Link{}, map[int]*ent.Chain{}
	chainKeys := map[int][]string{}
	for _, c := range creds {
		item := contracts.Credential{ID: int64(c.ID), InboundID: int64(c.NodeID), StatsKey: c.StatsKey, UUID: c.UUID, Password: c.Password}
		if c.Internal {
			item.UserKey, item.Internal = "internal", true
			cfg.Credentials = append(cfg.Credentials, item)
			continue
		}
		if c.UserID == nil {
			continue
		}
		u, ok := users[*c.UserID]
		if !ok {
			if u, err = s.Store.Ent.User.Get(ctx, *c.UserID); err != nil {
				return nil, err
			}
			users[u.ID] = u
		}
		if !u.Enabled {
			continue
		}
		if c.LinkID != nil {
			l, ok := links[*c.LinkID]
			if !ok {
				if l, err = s.Store.Ent.Link.Get(ctx, *c.LinkID); err != nil {
					return nil, err
				}
				links[l.ID] = l
			}
			if !l.Enabled {
				continue
			}
		}
		if c.ChainID != nil {
			ch, ok := chains[*c.ChainID]
			if !ok {
				if ch, err = s.Store.Ent.Chain.Get(ctx, *c.ChainID); err != nil {
					return nil, err
				}
				chains[ch.ID] = ch
			}
			if !ch.Enabled || !u.AllowChain {
				continue
			}
			chainKeys[ch.ID] = append(chainKeys[ch.ID], c.StatsKey)
		}
		key := userKey(u.ID)
		item.UserKey, item.ExpiresAt = key, u.ExpiresAt
		cfg.Credentials = append(cfg.Credentials, item)
		if _, done := cfg.Budgets[key]; done {
			continue
		}
		if u.Quota == 0 {
			cfg.Budgets[key] = contracts.GateBudget{Unlimited: true}
			continue
		}
		// 尚无额度记录时按 0 下发（该用户在本 Gate 暂不可用），由额度重新分配补上。
		b, err := s.Store.Ent.Budget.Query().Where(budget.UserIDEQ(u.ID), budget.GateIDEQ(g.ID)).Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
		if b != nil {
			cfg.Budgets[key] = contracts.GateBudget{Allocated: b.Allocated, Used: b.Used}
		} else {
			cfg.Budgets[key] = contracts.GateBudget{}
		}
	}
	return chainKeys, nil
}
