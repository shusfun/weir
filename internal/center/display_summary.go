package center

import (
	"context"
	"net"
	"strings"

	"github.com/shusfun/weir/internal/store/ent"
)

// DisplaySummary is safe to embed in user-facing choices. It contains no
// credentials, source members, or administrator exit endpoints.
type DisplaySummary struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Region string `json:"region"`
}

func (s *Server) exitPlace(ctx context.Context, e *ent.Exit) geoPlace {
	if e == nil {
		return geoPlace{Why: "没有出口"}
	}
	if e.Kind == "freedom" {
		if g, err := s.Store.Ent.Gate.Get(ctx, exitGateID(e)); err == nil {
			return s.locate(ctx, g.PublicHost)
		}
		return geoPlace{Why: "VPS 不存在"}
	}
	return geoPlace{Why: "落地位置按测试位置分别记录"}
}

// observedExitPlace 只读取指定入口 VPS 的实测落地，不用中心或其他 VPS 的结果填。
func (s *Server) observedExitPlace(ctx context.Context, e *ent.Exit, gateID int) (geoPlace, bool) {
	if e == nil || gateID == 0 {
		return geoPlace{}, false
	}
	p, ok := s.loadLocatedProbe(ctx, exitOwner(e.ID), "gate", gateID)
	if !ok || !p.OK || p.Mode != "proxy" || net.ParseIP(p.ExitIP) == nil {
		return geoPlace{}, false
	}
	if p.Place != nil {
		return *p.Place, true
	}
	return s.locate(ctx, p.ExitIP), true
}

func (s *Server) exitSummary(ctx context.Context, e *ent.Exit, chainName string) DisplaySummary {
	p := s.exitPlace(ctx, e)
	name := strings.TrimSpace(chainName)
	kind := "unknown"
	if e != nil {
		kind = string(e.Kind)
		if v := strings.TrimSpace(e.Remark); v != "" {
			name = v
		}
	}
	if name == "" {
		name = subscriptionPlace(p)
	}
	return DisplaySummary{Name: name, Kind: kind, Region: subscriptionPlace(p)}
}

func (s *Server) exitSummaryOnGate(ctx context.Context, e *ent.Exit, chainName string, gateID int) DisplaySummary {
	summary := s.exitSummary(ctx, e, chainName)
	if e == nil || e.Kind == "freedom" || gateID == 0 {
		return summary
	}
	place, ok := s.observedExitPlace(ctx, e, gateID)
	if !ok {
		return summary
	}
	region := subscriptionPlace(place)
	summary.Region = region
	if strings.TrimSpace(summary.Name) == "" || summary.Name == "地区未知" {
		summary.Name = region
	}
	return summary
}

func (s *Server) nodeDisplayName(ctx context.Context, n *ent.Node, g *ent.Gate, e *ent.Exit, ch *ent.Chain) string {
	if e != nil && e.Kind != "freedom" {
		fallback := ""
		if ch != nil && e.PolicyType == "" {
			fallback = ch.Name
		}
		gid := 0
		if g != nil {
			gid = g.ID
		}
		name := s.exitSummaryOnGate(ctx, e, fallback, gid).Name
		// 链式线路写成「入口 VPS > 代理」：同一个代理挂在多台 VPS 上时，订阅和分组里能分清走哪台。
		if g != nil && strings.TrimSpace(g.Name) != "" {
			return strings.TrimSpace(g.Name) + " > " + name
		}
		return name
	}
	if n != nil && strings.TrimSpace(n.Name) != "" {
		return strings.TrimSpace(n.Name)
	}
	if g != nil {
		return subscriptionPlace(s.locate(ctx, g.PublicHost))
	}
	return "地区未知"
}

func (s *Server) nodeChoice(ctx context.Context, n *ent.Node, g *ent.Gate, ch *ent.Chain) map[string]any {
	eid := n.ExitID
	if ch != nil {
		eid = ch.ExitID
	}
	e, _ := s.Store.Ent.Exit.Get(ctx, eid)
	v := map[string]any{"id": n.ID, "name": n.Name, "display_name": s.nodeDisplayName(ctx, n, g, e, ch), "protocol": n.Protocol, "network": n.Network, "security": n.Security, "preset": n.Preset, "gate_id": n.GateID, "port": n.Port}
	if g != nil {
		v["gate_name"], v["host"] = g.Name, g.PublicHost
	}
	if e != nil {
		fallback := ""
		if ch != nil && e.PolicyType == "" {
			fallback = ch.Name
		}
		gid := 0
		if g != nil {
			gid = g.ID
		}
		v["exit_summary"] = s.exitSummaryOnGate(ctx, e, fallback, gid)
	}
	return v
}
