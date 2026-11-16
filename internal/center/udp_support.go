package center

import (
	"context"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/route"
	"github.com/shusfun/weir/packages/contracts"
)

func typeUDP(s *Server, ctx context.Context, nodes []*ent.Node) string {
	if len(nodes) == 0 {
		return "未知"
	}
	knownAll, anySupport, anyReject := true, false, false
	for _, n := range nodes {
		supported, known := s.nodeUDP(ctx, n)
		if !known {
			knownAll = false
			continue
		}
		if supported {
			anySupport = true
		} else {
			anyReject = true
		}
	}
	switch {
	case anySupport && anyReject:
		return "部分支持"
	case anySupport && knownAll:
		return "支持"
	case anyReject && knownAll && !anySupport:
		return "不支持"
	default:
		return "未知"
	}
}

// A node-wide decision must include credential-specific routes; otherwise a
// TCP-only default would disable UDP on a user's working chain as well.
func (s *Server) nodeUDP(ctx context.Context, n *ent.Node) (supported, known bool) {
	if n != nil && (n.Protocol == "http" || n.Protocol == "shadowtls") {
		return false, true
	}
	return s.nodeRouteUDP(ctx, n, nil, true, map[int]bool{})
}

// uid nil includes all users for a node-wide summary; uid 0 is the internal
// transport identity, which does not match administrator rules for a user.
func (s *Server) nodeRouteUDP(ctx context.Context, n *ent.Node, uid *int, includeChains bool, seen map[int]bool) (bool, bool) {
	if n == nil || n.ExitID == 0 {
		return false, false
	}
	ids := map[int]bool{n.ExitID: true}
	rules, err := s.Store.Ent.Route.Query().Where(route.GateIDEQ(n.GateID), route.EnabledEQ(true), route.Or(route.NodeIDIsNil(), route.NodeIDEQ(n.ID))).All(ctx)
	if err != nil {
		return false, false
	}
	for _, r := range rules {
		if r.UserID != nil && uid != nil && *r.UserID != *uid {
			continue
		}
		e, err := s.Store.Ent.Exit.Get(ctx, r.ExitID)
		if err != nil {
			return false, false
		}
		// Intentional destination blocking applies to TCP too; it is not a
		// transport capability limit on otherwise usable destinations.
		if e.Kind != entexit.KindBlackhole {
			ids[r.ExitID] = true
		}
	}
	if includeChains {
		chains, err := s.Store.Ent.Chain.Query().Where(chain.EntryNodeIDEQ(n.ID), chain.EnabledEQ(true)).All(ctx)
		if err != nil {
			return false, false
		}
		for _, ch := range chains {
			ids[ch.ExitID] = true
		}
	}
	first, support := true, false
	for id := range ids {
		v, known := s.exitUDP(ctx, id, seen)
		if !known || !first && v != support {
			return false, false
		}
		first, support = false, v
	}
	return support, true
}

func (s *Server) exitUDP(ctx context.Context, id int, seen map[int]bool) (bool, bool) {
	if id == 0 || seen[id] || len(seen) > 8 {
		return false, false
	}
	seen[id] = true
	defer delete(seen, id)
	e, err := s.Store.Ent.Exit.Get(ctx, id)
	if err != nil || !e.Enabled {
		return false, false
	}
	switch e.Kind {
	case entexit.KindFreedom, entexit.KindHTTP, entexit.KindHTTPS, entexit.KindBlackhole:
		return udpKind(string(e.Kind), nil)
	case entexit.KindSocks5:
		p, ok := s.loadExitProbe(ctx, exitOwner(e.ID))
		var measured *bool
		if ok {
			measured = p.UDP
		}
		return udpKind(string(e.Kind), measured)
	case entexit.KindSource:
		stored, ok := s.loadExit(ctx, exitOwner(id))
		if !ok {
			return false, false
		}
		return groupExitUDP(stored.Outbound)
	case entexit.KindNode:
		if _, err := s.uotOutbound(ctx, e); err != nil {
			return false, false
		}
		if e.ExitNodeID == nil {
			return false, false
		}
		next, err := s.Store.Ent.Node.Get(ctx, *e.ExitNodeID)
		if err != nil {
			return false, false
		}
		internalUser := 0
		return s.nodeRouteUDP(ctx, next, &internalUser, false, seen)
	default:
		p, ok := s.loadExitProbe(ctx, exitOwner(e.ID))
		var measured *bool
		if ok {
			measured = p.UDP
		}
		return udpKind(string(e.Kind), measured)
	}
}

// udpMembers 只留下已知支持 UDP 的分组成员。未知成员不选入，避免把没测过的出口当成支持。
func udpMembers(members []contracts.Outbound) []contracts.Outbound {
	out := make([]contracts.Outbound, 0, len(members))
	for _, m := range members {
		supported, known := outboundKnownUDP(m)
		if known && supported {
			out = append(out, m)
		}
	}
	return out
}

func allUDPKnown(members []contracts.Outbound) bool {
	if len(members) == 0 {
		return false
	}
	for _, m := range members {
		if _, known := outboundKnownUDP(m); !known {
			return false
		}
	}
	return true
}

// groupExitUDP 读分组上记下的筛选结果。有支持的成员才算支持；全部已知且都不支持才算不支持。
func groupExitUDP(o contracts.Outbound) (supported, known bool) {
	if o.Protocol != "group" {
		return udpKind(o.Protocol, nil)
	}
	if o.Settings != nil {
		if v, ok := o.Settings["udp_supported"].(bool); ok && !v {
			return false, true
		}
		switch n := o.Settings["udp_members"].(type) {
		case int:
			if n > 0 {
				return true, true
			}
		case float64:
			if n > 0 {
				return true, true
			}
		}
	}
	return false, false
}

func udpKind(kind string, measured *bool) (supported, known bool) {
	switch kind {
	case "freedom":
		return true, true
	case "http", "https", "blackhole":
		return false, true
	case "socks", "socks5", "link", "source":
		if measured == nil {
			return false, false
		}
		return *measured, true
	default:
		return false, false
	}
}
