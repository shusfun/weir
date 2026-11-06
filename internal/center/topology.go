package center

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/chain"
	"github.com/shusfun/weir/internal/store/ent/credential"
	entnode "github.com/shusfun/weir/internal/store/ent/node"
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
		failInternal(c, 503, "query_failed", err)
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
	exitOf := func(id int) topoNode {
		e, err := s.Store.Ent.Exit.Get(ctx, id)
		if err != nil {
			return topoNode{ID: exitOwner(id), Kind: "exit", Label: "出口未知"}
		}
		return topoNode{ID: exitOwner(id), Kind: "exit", Label: firstNonEmpty(e.Remark, exitKindLabel(string(e.Kind))), Sub: exitKindLabel(string(e.Kind)), Link: "/exits?gate=" + itoa(g.ID) + "&focus=exit:" + itoa(id)}
	}
	for _, n := range nodes {
		nid := "node-" + itoa(n.ID)
		sub := n.Protocol + " :" + itoa(n.Port)
		if !n.Enabled {
			sub += " · 停用"
		}
		add(topoNode{ID: nid, Kind: "node", Label: n.Name, Sub: sub, Link: "/gates?node=" + itoa(n.ID)})
		creds, _ := s.Store.Ent.Credential.Query().Where(credential.NodeIDEQ(n.ID), credential.InternalEQ(false), credential.UserIDNotNil(), credential.ProbeParentIDIsNil()).All(ctx)
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
		ex := exitOf(n.ExitID)
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
		ex := exitOf(ch.ExitID)
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
		add(topoNode{ID: rid, Kind: "route", Label: label, Sub: sub, Link: "/exits?gate=" + itoa(g.ID)})
		exit := exitOf(r.ExitID)
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
