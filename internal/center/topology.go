package center

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	entnode "github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/outbound"
	"github.com/shusfun/weir/internal/store/ent/route"
)

// 拓扑：Gate 详情里的只读关系图：用户 → 入口节点 → 路由（用户自建链式代理、分流规则）→ 出口。点击跳到对应资源，不在图上编辑。

type topoNode struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // user / node / route / exit
	Label string `json:"label"`
	Sub   string `json:"sub,omitempty"`
	// Link 是点击后去的地方：users?user=、gates?gate=、gates?node=（节点所在行）、subscriptions、routing（出口与分流）。
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
	direct := topoNode{ID: "exit-direct", Kind: "exit", Label: "直连", Sub: "从这台 VPS 直接出去"}
	nodeExit := func(id int) topoNode {
		x, err := s.Store.Ent.Node.Get(ctx, id)
		if err != nil {
			return topoNode{ID: "exit-node-" + itoa(id), Kind: "exit", Label: "节点 " + itoa(id), Sub: "已不存在"}
		}
		sub := "另一台 VPS 的节点"
		if x.GateID == g.ID {
			sub = "这台 VPS 的节点"
		}
		return topoNode{ID: "exit-node-" + itoa(x.ID), Kind: "exit", Label: x.Name, Sub: sub, Link: "/gates?node=" + itoa(x.ID)}
	}
	// sourceExit：订阅源节点出口，显示最近一次解析到的节点（还没解析成功时流量被丢弃）。
	sourceExit := func(id, owner string) topoNode {
		n := topoNode{ID: id, Kind: "exit", Label: "订阅源节点", Sub: "还没有解析成功（流量丢弃）", Link: "/subscriptions"}
		if v, ok := s.loadExit(ctx, owner); ok && len(v.Names) > 0 {
			n.Sub = v.Names[0]
			if len(v.Names) > 1 {
				n.Label, n.Sub = "订阅源节点（一组）", fmt.Sprintf("%s 等 %d 个，内核自动切换", v.Names[0], len(v.Names))
			}
		}
		return n
	}
	exitOf := func(n *ent.Node) topoNode {
		switch n.ChainKind {
		case entnode.ChainKindSocks5:
			return topoNode{ID: "exit-socks-" + itoa(n.ID), Kind: "exit", Label: "家宽 SOCKS5", Sub: fmt.Sprintf("%s:%d", n.ChainAddress, n.ChainPort)}
		case entnode.ChainKindNode:
			if n.ChainNodeID != nil {
				return nodeExit(*n.ChainNodeID)
			}
		case entnode.ChainKindSource:
			return sourceExit("exit-nodesrc-"+itoa(n.ID), "node:"+itoa(n.ID))
		}
		return direct
	}
	for _, n := range nodes {
		nid := "node-" + itoa(n.ID)
		sub := n.Protocol + " :" + itoa(n.Port)
		if !n.Enabled {
			sub += " · 停用"
		}
		add(topoNode{ID: nid, Kind: "node", Label: n.Name, Sub: sub, Link: "/gates?node=" + itoa(n.ID)})
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
	// 用户自建链式代理：只有这个用户经这个入口的流量走它，优先于分流规则与节点自己的出口。
	ids := make([]int, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	chains, _ := s.Store.Ent.Chain.Query().Where(chain.EntryNodeIDIn(ids...)).Order(ent.Asc(chain.FieldID)).All(ctx)
	for _, ch := range chains {
		u, err := s.Store.Ent.User.Get(ctx, ch.UserID)
		if err != nil || !u.AllowChain {
			continue
		}
		cid, uid := "chain-"+itoa(ch.ID), "user-"+itoa(ch.UserID)
		add(topoNode{ID: uid, Kind: "user", Label: firstNonEmpty(u.Name, u.Email), Link: "/users?user=" + itoa(ch.UserID)})
		add(topoNode{ID: cid, Kind: "route", Label: "链式代理 " + ch.Name, Sub: "只对 " + firstNonEmpty(u.Name, u.Email) + "，优先", Link: "/users?user=" + itoa(ch.UserID)})
		var ex topoNode
		switch ch.ExitKind {
		case chain.ExitKindSocks5:
			ex = topoNode{ID: "exit-chain-" + itoa(ch.ID), Kind: "exit", Label: "家宽 SOCKS5", Sub: fmt.Sprintf("%s:%d", ch.ExitAddress, ch.ExitPort), Link: "/users?user=" + itoa(ch.UserID)}
		case chain.ExitKindSource:
			ex = sourceExit("exit-chainsrc-"+itoa(ch.ID), "chain:"+itoa(ch.ID))
		default:
			if ch.ExitNodeID == nil {
				continue
			}
			ex = nodeExit(*ch.ExitNodeID)
		}
		add(ex)
		edges = append(edges, topoEdge{"node-" + itoa(ch.EntryNodeID), cid}, topoEdge{cid, ex.ID})
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
		sub := fmt.Sprintf("优先级 %d", r.Priority)
		if r.UserID != nil {
			if u, err := s.Store.Ent.User.Get(ctx, *r.UserID); err == nil {
				sub += " · 只对 " + firstNonEmpty(u.Name, u.Email)
			}
		}
		add(topoNode{ID: rid, Kind: "route", Label: label, Sub: sub, Link: "routing"})
		exit := direct
		if r.OutboundTag != "direct" {
			if o, err := s.Store.Ent.Outbound.Query().Where(outbound.GateIDEQ(g.ID), outbound.TagEQ(r.OutboundTag)).Only(ctx); err == nil {
				exit = topoNode{ID: "exit-out-" + itoa(o.ID), Kind: "exit", Label: firstNonEmpty(o.Name, o.Tag), Sub: o.Protocol, Link: "routing"}
				if o.Protocol == "source" {
					exit = sourceExit("exit-out-"+itoa(o.ID), "out:"+itoa(o.ID))
					exit.Label, exit.Link = firstNonEmpty(o.Name, o.Tag)+" · "+exit.Label, "routing"
				}
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
