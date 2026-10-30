package center

import (
	"context"
	"fmt"
	"strconv"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/node"
)

var exitLabels = map[string]string{
	"direct": "直接出去", "socks5": "SOCKS5 代理", "http": "HTTP 代理", "https": "HTTPS 代理", "link": "节点链接",
	"node": "另一台 VPS", "source": "订阅源节点", "block": "丢弃",
}

// fillExit 补上出口的显示信息：地址（填写的或订阅源里第一个成员）、位置（优先用实测到的出口 IP）、实测结果、成员。
func (s *Server) fillExit(ctx context.Context, e *mapExit, address, probeOwner, sourceOwner string) {
	if e.Label == "" {
		e.Label = exitLabels[e.Kind]
	}
	if sourceOwner != "" {
		if v, ok := s.loadExit(ctx, sourceOwner); ok {
			e.Members, e.Skipped, e.Group = v.Names, len(v.Skipped), v.Outbound.Protocol == "group"
			if ms := v.Outbound.Members; len(ms) > 0 {
				address = ms[0].Address
			} else {
				address = v.Outbound.Address
			}
			if len(v.Names) > 1 {
				e.Sub = fmt.Sprintf("%s 等 %d 个，内核自动切换", v.Names[0], len(v.Names))
			} else if len(v.Names) == 1 {
				e.Sub = v.Names[0]
			}
		} else {
			e.Sub = "还没有解析成功（流量丢弃）"
		}
	}
	e.Address = address
	if p, ok := s.loadExitProbe(ctx, probeOwner); ok && probeOwner != "" {
		e.Probe = &p
		if p.ExitIP != "" {
			address = p.ExitIP
		}
	}
	e.Place = s.locate(ctx, address)
}

// redactExit：普通用户看到管理员配的出口时，只留类型与地区（地图上的点仍按城市放），不显示地址与运营商。
func redactExit(e *mapExit) {
	e.Address, e.Members, e.Link = "", nil, ""
	e.Place.IP, e.Place.Org, e.Place.ASN = "", "", 0
	if e.Probe != nil {
		p := *e.Probe
		p.ExitIP, p.Error = "", ""
		e.Probe = &p
	}
	e.Sub = "管理员配置的出口"
}

// chainExit 是用户自建链式代理的出口（ID 含入口 Gate）。
func (s *Server) chainExit(ctx context.Context, ch *ent.Chain, entryGate int, gates map[int]*mapGate, nodes map[int]*ent.Node) *mapExit {
	e := &mapExit{ID: "chain:" + itoa(ch.ID), Kind: string(ch.ExitKind), FromGate: entryGate, Owner: "self", Link: "/users?user=" + itoa(ch.UserID), tag: "chain-" + itoa(ch.ID)}
	owner := "chain:" + itoa(ch.ID)
	switch ch.ExitKind {
	case chain.ExitKindSource:
		s.fillExit(ctx, e, "", owner, owner)
	case chain.ExitKindNode:
		s.nodeTarget(ctx, e, ch.ExitNodeID, gates, nodes)
	default:
		s.fillExit(ctx, e, ch.ExitAddress, owner, "")
		if e.Address != "" {
			e.Sub = e.Address + ":" + strconv.Itoa(ch.ExitPort)
		}
	}
	e.Label = ch.Name + " · " + exitLabels[e.Kind]
	return e
}

// nodeChainExit 是节点自己的出口：直接出去，或管理员在建节点第 ③ 步配的链式代理。
func (s *Server) nodeChainExit(ctx context.Context, n *ent.Node, gates map[int]*mapGate, nodes map[int]*ent.Node) *mapExit {
	if n.ChainKind == node.ChainKindNone {
		e := &mapExit{ID: "direct:" + itoa(n.GateID), Kind: "direct", FromGate: n.GateID, Owner: "admin", Sub: "从这台 VPS 直接访问目标", tag: "direct"}
		e.Label = exitLabels["direct"]
		if g := gates[n.GateID]; g != nil {
			e.Place = g.Place
		}
		return e
	}
	e := &mapExit{ID: "nodechain:" + itoa(n.ID), Kind: string(n.ChainKind), FromGate: n.GateID, Owner: "admin", Link: "/gates?node=" + itoa(n.ID), tag: "nodechain-" + itoa(n.ID)}
	owner := "node:" + itoa(n.ID)
	switch n.ChainKind {
	case node.ChainKindSource:
		s.fillExit(ctx, e, "", owner, owner)
	case node.ChainKindNode:
		s.nodeTarget(ctx, e, n.ChainNodeID, gates, nodes)
	default:
		s.fillExit(ctx, e, n.ChainAddress, owner, "")
		if e.Address != "" {
			e.Sub = e.Address + ":" + strconv.Itoa(n.ChainPort)
		}
	}
	e.Label = n.Name + " · " + exitLabels[e.Kind]
	return e
}

// nodeTarget：出口是另一台 VPS 上的节点，线连到那台 VPS。
func (s *Server) nodeTarget(ctx context.Context, e *mapExit, id *int, gates map[int]*mapGate, nodes map[int]*ent.Node) {
	e.Kind, e.Label = "node", exitLabels["node"]
	if id == nil || nodes[*id] == nil {
		e.Sub, e.Place = "出口节点已不存在", geoPlace{Why: "出口节点已不存在"}
		return
	}
	x := nodes[*id]
	e.ToGate, e.Sub, e.Link = x.GateID, x.Name, "/gates?node="+itoa(x.ID)
	if g := gates[x.GateID]; g != nil {
		e.Place, e.Address = g.Place, g.Place.IP
	}
}

// outExit 是管理员在「出口与分流」里建的出口（分流规则指向它）。
func (s *Server) outExit(ctx context.Context, o *ent.Outbound, gates map[int]*mapGate) *mapExit {
	e := &mapExit{ID: "out:" + itoa(o.ID), FromGate: o.GateID, Owner: "admin", Link: "routing", Label: firstNonEmpty(o.Name, o.Tag), tag: o.Tag}
	switch o.Protocol {
	case "socks":
		e.Kind = "socks5"
	case "freedom":
		e.Kind, e.Sub = "direct", "直接出去"
		if g := gates[o.GateID]; g != nil {
			e.Place = g.Place
		}
		return e
	case "blackhole":
		e.Kind, e.Sub, e.Place = "block", "丢弃", geoPlace{Why: "丢弃流量，不出去"}
		return e
	default:
		e.Kind = o.Protocol
	}
	owner := "out:" + itoa(o.ID)
	if o.Protocol == "source" {
		s.fillExit(ctx, e, "", owner, owner)
	} else {
		s.fillExit(ctx, e, o.Address, owner, "")
	}
	e.Label = firstNonEmpty(o.Name, o.Tag) + " · " + exitLabels[e.Kind]
	return e
}
