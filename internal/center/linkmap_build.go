package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/credential"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/outbound"
	"github.com/shusfun/weir/internal/store/ent/route"
)

// buildLinkMap 生成链路页的快照。viewer 为 0 时是管理员视图（全部），否则只含这个用户的路径。
func (s *Server) buildLinkMap(ctx context.Context, viewer int) (*linkMap, error) {
	admin := viewer == 0
	m := &linkMap{Admin: admin, Geo: s.geoDB().Status(), Gates: []*mapGate{}, Exits: []*mapExit{}, Paths: []*mapPath{},
		index: liveIndex{keys: map[int]map[string]string{}, tags: map[int]map[string]string{}, members: map[int]map[string][]string{}}}
	gs, err := s.Store.Ent.Gate.Query().Where(gate.StatusEQ(gate.StatusActive)).Order(ent.Asc(gate.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	ns, err := s.Store.Ent.Node.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	nodes := map[int]*ent.Node{}
	for _, n := range ns {
		nodes[n.ID] = n
	}
	gates, entGates := map[int]*mapGate{}, map[int]*ent.Gate{}
	for _, g := range gs {
		entGates[g.ID] = g
		mg := &mapGate{ID: g.ID, Name: g.Name, Online: g.LastSeenAt != nil && time.Since(*g.LastSeenAt) < 90*time.Second, Place: s.locate(ctx, g.PublicHost), Nodes: []mapNode{}}
		if admin {
			v := gateView(g)
			mg.Host, mg.State, mg.LastError, mg.Kernels, mg.Version, mg.LastSeen = g.Host, v["state"].(string), g.LastError, g.KernelsRunning, g.Version, g.LastSeenAt
		}
		gates[g.ID] = mg
	}
	q := s.Store.Ent.Credential.Query().Where(credential.InternalEQ(false), credential.UserIDNotNil())
	if !admin {
		q = q.Where(credential.UserIDEQ(viewer))
	}
	creds, err := q.Order(ent.Asc(credential.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	users, links, chains := map[int]*ent.User{}, map[int]*ent.Link{}, map[int]*ent.Chain{}
	exits := map[string]*mapExit{}
	addExit := func(e *mapExit) *mapExit {
		if old, ok := exits[e.ID]; ok {
			return old
		}
		exits[e.ID] = e
		m.Exits = append(m.Exits, e)
		return e
	}
	paths := map[string]*mapPath{}
	pathCreds := map[string][]int{}
	nodeUsers := map[int]map[int]bool{}
	for _, c := range creds {
		n := nodes[c.NodeID]
		if n == nil || !n.Enabled || gates[n.GateID] == nil {
			continue
		}
		u, ok := users[*c.UserID]
		if !ok {
			if u, err = s.Store.Ent.User.Get(ctx, *c.UserID); err != nil {
				continue
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
					continue
				}
				links[l.ID] = l
			}
			if !l.Enabled {
				continue
			}
		}
		var ch *ent.Chain
		if c.ChainID != nil {
			if ch = chains[*c.ChainID]; ch == nil {
				if ch, err = s.Store.Ent.Chain.Get(ctx, *c.ChainID); err != nil {
					continue
				}
				chains[ch.ID] = ch
			}
			if !ch.Enabled || !u.AllowChain {
				continue
			}
		}
		pid := fmt.Sprintf("p:%d:%d", u.ID, n.ID)
		if ch != nil {
			pid += ":" + itoa(ch.ID)
		}
		p := paths[pid]
		if p == nil {
			p = &mapPath{ID: pid, UserID: u.ID, User: firstNonEmpty(u.Name, u.Email), NodeID: n.ID, Node: n.Name, GateID: n.GateID, Route: "direct"}
			var e *mapExit
			if ch != nil {
				p.ChainID, p.Chain, p.Route = ch.ID, ch.Name, "chain"
				e = addExit(s.chainExit(ctx, ch, n.GateID, gates, nodes))
			} else {
				if n.ChainKind != "none" {
					p.Route = "nodechain"
				}
				e = addExit(s.nodeChainExit(ctx, n, gates, nodes))
			}
			p.ExitID = e.ID
			paths[pid] = p
			m.Paths = append(m.Paths, p)
		}
		pathCreds[pid] = append(pathCreds[pid], c.ID)
		if m.index.keys[n.GateID] == nil {
			m.index.keys[n.GateID] = map[string]string{}
		}
		m.index.keys[n.GateID][c.StatsKey] = pid
		if nodeUsers[n.ID] == nil {
			nodeUsers[n.ID] = map[int]bool{}
		}
		nodeUsers[n.ID][u.ID] = true
	}
	entry := map[int]bool{}
	for _, p := range m.Paths {
		entry[p.GateID] = true
	}
	if admin {
		// 管理员：没有人用的节点也画出来（出口照常），以及分流规则指向的出口。
		for _, n := range ns {
			if n.Enabled && gates[n.GateID] != nil {
				entry[n.GateID] = true
				addExit(s.nodeChainExit(ctx, n, gates, nodes))
			}
		}
		if err = s.addRules(ctx, m, gates, addExit); err != nil {
			return nil, err
		}
	} else {
		m.Hidden, _ = s.Store.Ent.Route.Query().Where(route.EnabledEQ(true), route.Or(route.UserIDIsNil(), route.UserIDEQ(viewer)), route.GateIDIn(keysOf(entry)...)).Count(ctx)
	}
	s.finishLinkMap(ctx, m, admin, gates, entGates, ns, entry, nodeUsers, pathCreds)
	return m, nil
}

func keysOf(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// addRules 加上管理员分流规则与它们指向的出口（direct 指向这台 VPS 的直连）。
func (s *Server) addRules(ctx context.Context, m *linkMap, gates map[int]*mapGate, addExit func(*mapExit) *mapExit) error {
	rules, err := s.Store.Ent.Route.Query().Where(route.EnabledEQ(true)).Order(ent.Asc(route.FieldPriority)).All(ctx)
	if err != nil {
		return err
	}
	outs, err := s.Store.Ent.Outbound.Query().Where(outbound.EnabledEQ(true)).All(ctx)
	if err != nil {
		return err
	}
	byTag := map[string]*ent.Outbound{}
	for _, o := range outs {
		byTag[itoa(o.GateID)+"/"+o.Tag] = o
	}
	for _, r := range rules {
		if gates[r.GateID] == nil {
			continue
		}
		label := "全部流量"
		if r.Domain != "" {
			label = "域名 " + r.Domain
		} else if r.IP != "" {
			label = "IP " + r.IP
		}
		exitID := "direct:" + itoa(r.GateID)
		if o := byTag[itoa(r.GateID)+"/"+r.OutboundTag]; o != nil {
			exitID = addExit(s.outExit(ctx, o, gates)).ID
		} else if r.OutboundTag == "direct" {
			e := &mapExit{ID: exitID, Kind: "direct", Label: exitLabels["direct"], FromGate: r.GateID, Owner: "admin", Place: gates[r.GateID].Place, tag: "direct"}
			exitID = addExit(e).ID
		}
		m.Rules = append(m.Rules, mapRule{ID: r.ID, GateID: r.GateID, Label: label, ExitID: exitID, NodeID: r.NodeID, UserID: r.UserID})
	}
	return nil
}

// finishLinkMap：节点列表、出口标签索引、用户版的脱敏、今天的流量、用户列表与版本号。
func (s *Server) finishLinkMap(ctx context.Context, m *linkMap, admin bool, gates map[int]*mapGate, entGates map[int]*ent.Gate, ns []*ent.Node, entry map[int]bool, nodeUsers map[int]map[int]bool, pathCreds map[string][]int) {
	mine := map[int]bool{}
	for _, p := range m.Paths {
		mine[p.NodeID] = true
	}
	for _, n := range ns {
		g := gates[n.GateID]
		if g == nil || !admin && !mine[n.ID] || admin && !n.Enabled && !entry[n.GateID] {
			continue
		}
		mn := mapNode{ID: n.ID, Name: n.Name, Protocol: n.Protocol, Network: n.Network, Security: n.Security, Port: n.Port, Enabled: n.Enabled}
		if admin {
			mn.Kernel, mn.Ports, mn.Users = n.Kernel, nodePorts(n, entGates[n.GateID]), len(nodeUsers[n.ID])
			if r, ok := s.loadReach(ctx, n.ID); ok {
				mn.Reach = &r
			}
		}
		g.Nodes = append(g.Nodes, mn)
		g.Entry = true
	}
	need := map[int]bool{}
	for k := range entry {
		need[k] = true
	}
	if admin {
		for id := range gates {
			need[id] = true // 管理员：还没有节点的 VPS 也画出来
		}
	}
	for _, e := range m.Exits {
		if !admin && e.Owner == "admin" && e.Kind != "direct" {
			redactExit(e)
		}
		if e.ToGate > 0 {
			need[e.ToGate] = true
		}
		if admin {
			s.indexExit(m, e)
		}
	}
	for _, id := range keysOf(need) {
		g := gates[id]
		if g == nil {
			continue
		}
		if !admin && !entry[id] {
			// 只是出口落点：不显示这台 VPS 的地址与运营商。
			g.Place.IP, g.Place.Org, g.Place.ASN = "", "", 0
		}
		m.Gates = append(m.Gates, g)
	}
	m.index.gates = keysOf(entry)
	today := time.Now().Format("20060102")
	for _, p := range m.Paths {
		var sum int64
		known := true
		for _, cid := range pathCreds[p.ID] {
			raw, ok, err := s.Store.KV.Get(ctx, "traffic-day:"+today+":"+itoa(cid))
			if err != nil {
				known = false
				break
			}
			if ok {
				var v int64
				fmt.Sscan(raw, &v)
				sum += v
			}
		}
		if known {
			p.TodayBytes = &sum
		}
	}
	if admin {
		byUser := map[int]*mapUser{}
		for _, p := range m.Paths {
			u := byUser[p.UserID]
			if u == nil {
				u = &mapUser{ID: p.UserID, Name: p.User}
				byUser[p.UserID] = u
			}
			u.Paths = append(u.Paths, p.ID)
		}
		for _, id := range sortedKeys(byUser) {
			m.Users = append(m.Users, *byUser[id])
		}
	}
	h := sha256.New()
	for _, g := range m.Gates {
		fmt.Fprintf(h, "g%d:%s:%d;", g.ID, g.Name, len(g.Nodes))
	}
	for _, e := range m.Exits {
		fmt.Fprintf(h, "e%s:%s:%d;", e.ID, e.Label, len(e.Members))
	}
	for _, p := range m.Paths {
		fmt.Fprintf(h, "p%s:%s;", p.ID, p.ExitID)
	}
	for _, r := range m.Rules {
		fmt.Fprintf(h, "r%d:%s;", r.ID, r.ExitID)
	}
	m.Version = hex.EncodeToString(h.Sum(nil))[:16]
}

func sortedKeys[T any](m map[int]T) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// indexExit 记下出口在 Gate 配置里的标签，实时计数据此对应到这条线；一组的成员是「标签-m序号」。
func (s *Server) indexExit(m *linkMap, e *mapExit) {
	if e.tag == "" {
		return
	}
	g := e.FromGate
	if m.index.tags[g] == nil {
		m.index.tags[g] = map[string]string{}
		m.index.members[g] = map[string][]string{}
	}
	m.index.tags[g][e.tag] = e.ID
	if e.Group {
		for i := range e.Members {
			m.index.members[g][e.ID] = append(m.index.members[g][e.ID], fmt.Sprintf("%s-m%d", e.tag, i+1))
		}
	}
}
