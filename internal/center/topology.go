package center

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/credential"
	entnode "github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/outbound"
	"github.com/shusfun/weir/internal/store/ent/route"
)

// 拓扑：Gate 详情里的只读关系图：用户 → 入口节点 → 路由 → 出口。点击跳到对应资源，不在图上编辑。

type topoNode struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // user / node / route / exit
	Label string `json:"label"`
	Sub   string `json:"sub,omitempty"`
	// Link 是点击后去的地方：users?user=、gates?gate=、routing（出口与分流）。
	Link string `json:"link,omitempty"`
}

type topoEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (s *Server) gateTopology(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	nodes, err := s.Store.Ent.Node.Query().Where(entnode.GateIDEQ(g.ID)).Order(ent.Asc(entnode.FieldID)).All(ctx)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	var out []topoNode
	var edges []topoEdge
	seen := map[string]bool{}
	add := func(n topoNode) {
		if !seen[n.ID] {
			seen[n.ID] = true
			out = append(out, n)
		}
	}
	exitOf := func(n *ent.Node) topoNode {
		switch n.ChainKind {
		case entnode.ChainKindSocks5:
			return topoNode{ID: "exit-socks-" + itoa(n.ID), Kind: "exit", Label: "家宽 SOCKS5", Sub: fmt.Sprintf("%s:%d", n.ChainAddress, n.ChainPort)}
		case entnode.ChainKindNode:
			if n.ChainNodeID != nil {
				if x, err := s.Store.Ent.Node.Get(ctx, *n.ChainNodeID); err == nil {
					return topoNode{ID: "exit-node-" + itoa(x.ID), Kind: "exit", Label: x.Name, Sub: "另一台 VPS 的节点", Link: "/gates?gate=" + itoa(x.GateID)}
				}
			}
		}
		return topoNode{ID: "exit-direct", Kind: "exit", Label: "直连", Sub: "从这台 VPS 直接出去"}
	}
	for _, n := range nodes {
		nid := "node-" + itoa(n.ID)
		sub := n.Protocol + " :" + itoa(n.Port)
		if !n.Enabled {
			sub += " · 停用"
		}
		add(topoNode{ID: nid, Kind: "node", Label: n.Name, Sub: sub})
		creds, _ := s.Store.Ent.Credential.Query().Where(credential.NodeIDEQ(n.ID), credential.InternalEQ(false), credential.UserIDNotNil()).All(ctx)
		users := map[int]int{}
		for _, cr := range creds {
			users[*cr.UserID]++
		}
		for uid := range users {
			if u, err := s.Store.Ent.User.Get(ctx, uid); err == nil {
				id := "user-" + itoa(uid)
				add(topoNode{ID: id, Kind: "user", Label: firstNonEmpty(u.Name, u.Email), Link: "/users?user=" + itoa(uid)})
				edges = append(edges, topoEdge{id, nid})
			}
		}
		ex := exitOf(n)
		add(ex)
		edges = append(edges, topoEdge{nid, ex.ID})
	}
	rules, _ := s.Store.Ent.Route.Query().Where(route.GateIDEQ(g.ID), route.EnabledEQ(true)).Order(ent.Asc(route.FieldPriority)).All(ctx)
	for _, r := range rules {
		rid := "route-" + itoa(r.ID)
		label := "全部流量"
		if r.Domain != "" {
			label = "域名 " + r.Domain
		} else if r.IP != "" {
			label = "IP " + r.IP
		}
		add(topoNode{ID: rid, Kind: "route", Label: label, Sub: fmt.Sprintf("优先级 %d", r.Priority), Link: "routing"})
		exit := topoNode{ID: "exit-direct", Kind: "exit", Label: "直连", Sub: "从这台 VPS 直接出去"}
		if r.OutboundTag != "direct" {
			if o, err := s.Store.Ent.Outbound.Query().Where(outbound.GateIDEQ(g.ID), outbound.TagEQ(r.OutboundTag)).Only(ctx); err == nil {
				exit = topoNode{ID: "exit-out-" + itoa(o.ID), Kind: "exit", Label: firstNonEmpty(o.Name, o.Tag), Sub: o.Protocol, Link: "routing"}
			}
		}
		add(exit)
		edges = append(edges, topoEdge{rid, exit.ID})
		for _, n := range nodes {
			if r.NodeID == nil || *r.NodeID == n.ID {
				edges = append(edges, topoEdge{"node-" + itoa(n.ID), rid})
			}
		}
	}
	if out == nil {
		out = []topoNode{}
	}
	if edges == nil {
		edges = []topoEdge{}
	}
	c.JSON(200, gin.H{"nodes": out, "edges": edges})
}
