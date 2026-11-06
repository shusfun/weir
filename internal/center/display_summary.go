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
		if g, err := s.Store.Ent.Gate.Get(ctx, e.GateID); err == nil {
			return s.locate(ctx, g.PublicHost)
		}
		return geoPlace{Why: "VPS 不存在"}
	}
	if p, ok := s.loadExitProbe(ctx, exitOwner(e.ID)); ok && p.OK && p.Mode == "proxy" && !p.At.IsZero() && net.ParseIP(p.ExitIP) != nil {
		return s.locate(ctx, p.ExitIP)
	}
	return geoPlace{Why: "还没有有效的实际出口 IP"}
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

func (s *Server) nodeDisplayName(ctx context.Context, n *ent.Node, g *ent.Gate, e *ent.Exit, ch *ent.Chain) string {
	if e != nil && e.Kind != "freedom" {
		fallback := ""
		if ch != nil && e.PolicyType == "" {
			fallback = ch.Name
		}
		return s.exitSummary(ctx, e, fallback).Name
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
		v["exit_summary"] = s.exitSummary(ctx, e, fallback)
	}
	return v
}
