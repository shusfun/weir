package center

import (
	"context"
	"fmt"
	"strconv"

	"github.com/shusfun/weir/internal/store/ent"
)

// fillExit 补上出口的显示信息：地址（填写的或订阅源里第一个成员）、位置（优先用实测到的出口 IP）、实测结果、成员。
func (s *Server) fillExit(ctx context.Context, e *mapExit, address, probeOwner, sourceOwner string) {
	if e.Label == "" {
		e.Label = exitKindLabel(map[string]string{"direct": "freedom", "block": "blackhole"}[e.Kind])
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

func (s *Server) chainExit(ctx context.Context, ch *ent.Chain, entryGate int, gates map[int]*mapGate, nodes map[int]*ent.Node) *mapExit {
	out := s.mapStoredExit(ctx, ch.ExitID, entryGate, gates, nodes)
	if e, err := s.Store.Ent.Exit.Get(ctx, ch.ExitID); err == nil && e.PolicyType == "" {
		out.Label = s.exitSummary(ctx, e, ch.Name).Name
	}
	return out
}
func (s *Server) nodeChainExit(ctx context.Context, n *ent.Node, gates map[int]*mapGate, nodes map[int]*ent.Node) *mapExit {
	return s.mapStoredExit(ctx, n.ExitID, n.GateID, gates, nodes)
}

func (s *Server) mapStoredExit(ctx context.Context, id, gid int, gates map[int]*mapGate, nodes map[int]*ent.Node) *mapExit {
	e, err := s.Store.Ent.Exit.Get(ctx, id)
	if err != nil {
		return &mapExit{ID: exitOwner(id), Kind: "unknown", Label: "出口未知", FromGate: gid, Place: geoPlace{Why: "出口不可用"}}
	}
	out := &mapExit{ID: exitOwner(id), Kind: string(e.Kind), Label: s.exitSummary(ctx, e, "").Name, FromGate: gid, Owner: "admin", Link: "/exits?gate=" + itoa(gid) + "&focus=exit:" + itoa(id), tag: e.Tag}
	if e.OwnerUserID != nil {
		out.Owner = "self"
	}
	switch e.Kind {
	case "freedom":
		out.Kind = "direct"
		if g := gates[gid]; g != nil {
			out.Place = g.Place
		}
		return out
	case "blackhole":
		out.Kind = "block"
		out.Place = geoPlace{Why: "丢弃流量，不出去"}
		return out
	case "node":
		if e.ExitNodeID != nil && nodes[*e.ExitNodeID] == nil {
			if n, err := s.Store.Ent.Node.Get(ctx, *e.ExitNodeID); err == nil {
				nodes[n.ID] = n
			}
		}
		if e.ExitNodeID == nil || nodes[*e.ExitNodeID] == nil {
			out.Sub = "出口节点已不存在"
			out.Place = geoPlace{Why: out.Sub}
			return out
		}
		n := nodes[*e.ExitNodeID]
		out.ToGate, out.Sub = n.GateID, n.Name
		if g := gates[n.GateID]; g != nil {
			out.Place, out.Address = s.exitPlace(ctx, e), g.Place.IP
		}
		out.Place = s.exitPlace(ctx, e)
		return out
	case "source":
		s.fillExit(ctx, out, "", exitOwner(id), exitOwner(id))
	default:
		s.fillExit(ctx, out, e.Address, exitOwner(id), "")
		out.Sub = e.Address + ":" + strconv.Itoa(e.Port)
	}
	out.Place = s.exitPlace(ctx, e)
	return out
}
