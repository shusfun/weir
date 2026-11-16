package center

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/resource"
)

const (
	portMin = 20000
	portMax = 60000
)

// reservedPorts 永不分配：SSH、网站、常见面板与中心默认端口。
var reservedPorts = map[int]bool{22: true, 80: true, 443: true, 8080: true, 8443: true, 19810: true}

// gateOwnPorts 是本项目在该 Gate 上已分配的端口（节点入站 + 统计端口）。
func (s *Server) gateOwnPorts(ctx context.Context, g *ent.Gate) (map[int]bool, error) {
	return s.gateOwnPortsDB(ctx, s.Store.Ent, g)
}

func (s *Server) gateOwnPortsDB(ctx context.Context, db *ent.Client, g *ent.Gate) (map[int]bool, error) {
	own := map[int]bool{}
	nodes, err := db.Node.Query().Where(node.GateIDEQ(g.ID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		own[n.Port] = true
	}
	managed, err := s.gateManagedPortsDB(ctx, db, g)
	if err != nil {
		return nil, err
	}
	for port := range managed {
		own[port] = true
	}
	return own, nil
}

func (s *Server) gateManagedPortsDB(ctx context.Context, db *ent.Client, g *ent.Gate) (map[int]bool, error) {
	own := map[int]bool{}
	if g.XrayStatsPort > 0 {
		own[g.XrayStatsPort] = true
	}
	if g.SingboxStatsPort > 0 {
		own[g.SingboxStatsPort] = true
	}
	_, bridges, err := s.loadOutboundBridges(ctx, db, g.ID)
	if err != nil {
		return nil, err
	}
	for _, b := range bridges {
		own[b.Port] = true
	}
	_, observers, err := s.loadDomainObservers(ctx, db, g.ID)
	if err != nil {
		return nil, err
	}
	for _, o := range observers {
		own[o.Port] = true
	}
	probe, err := readBrowserConfig(ctx, db, g.ID)
	if err != nil {
		return nil, err
	}
	if probe != nil {
		own[probe.Plan.Port], own[probe.Plan.RTCPort] = true, true
	}
	rows, err := db.Resource.Query().Where(resource.KindEQ(uotResourceKind), resource.OwnerIDEQ(g.ID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		p, err := decodeUoT(row)
		if err != nil {
			return nil, err
		}
		own[p.Port] = true
	}
	return own, nil
}

// numberOf 读取 JSON 字段里的整数：刚保存的实体是 int，从数据库读回的是 float64。
func numberOf(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// foreignPorts 是 Gate 上报正在监听、但不是本项目分配的端口（x-ui、nginx 等其他程序）。
func foreignPorts(g *ent.Gate, own map[int]bool) map[int]bool {
	out := map[int]bool{}
	for _, p := range g.ListeningPorts {
		if v := numberOf(p["port"]); v > 0 && !own[v] {
			out[v] = true
		}
	}
	return out
}

// pickPort 在 20000–60000 中随机选一个未保留、未被本项目使用、Gate 上也没有其他程序占用的端口。
func pickPort(taken ...map[int]bool) (int, error) {
	span := big.NewInt(portMax - portMin)
	for attempt := 0; attempt < 4000; attempt++ {
		n, err := rand.Int(rand.Reader, span)
		if err != nil {
			return 0, err
		}
		port := portMin + int(n.Int64())
		if reservedPorts[port] {
			continue
		}
		free := true
		for _, set := range taken {
			if set[port] {
				free = false
				break
			}
		}
		if free {
			return port, nil
		}
	}
	return 0, errors.New("no free port on this gate")
}

// AllocateNodePort 为新节点在指定 Gate 上分配端口。
func (s *Server) AllocateNodePort(ctx context.Context, g *ent.Gate) (int, error) {
	return s.allocateNodePort(ctx, g, false)
}

// allocateNodePort：REALITY 节点在 443 空闲时优先用 443（伪装更自然）。是否空闲只看 Gate 上报的实际占用
// （1Panel 的 OpenResty、其他网站、x-ui 等占着时照常随机分配），这台 VPS 上已有本项目节点用 443 时也不用。
// Gate 还没有上报过端口时不知道 443 是否空闲，也照常随机分配。其他协议不分配 443。
func (s *Server) allocateNodePort(ctx context.Context, g *ent.Gate, reality bool) (int, error) {
	own, err := s.gateOwnPorts(ctx, g)
	if err != nil {
		return 0, err
	}
	foreign := foreignPorts(g, own)
	if reality && g.LastSeenAt != nil && g.ListeningPorts != nil && !own[443] && !foreign[443] {
		return 443, nil
	}
	return pickPort(own, foreign)
}

// ensureStatsPorts 为 Gate 分配两个只监听回环的统计端口；已分配且未与其他程序冲突时保持不变。
func (s *Server) ensureStatsPorts(ctx context.Context, g *ent.Gate) (*ent.Gate, error) {
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = lockActiveGates(ctx, tx.Client(), g.ID); err != nil {
		return nil, err
	}
	g, err = tx.Gate.Get(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	own, err := s.gateOwnPortsDB(ctx, tx.Client(), g)
	if err != nil {
		return nil, err
	}
	foreign := foreignPorts(g, own)
	xp, sp := g.XrayStatsPort, g.SingboxStatsPort
	if xp == 0 || foreign[xp] {
		if xp, err = pickPort(own, foreign); err != nil {
			return nil, err
		}
		own[xp] = true
	}
	if sp == 0 || foreign[sp] {
		if sp, err = pickPort(own, foreign); err != nil {
			return nil, err
		}
	}
	if xp == g.XrayStatsPort && sp == g.SingboxStatsPort {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return g.Unwrap(), nil
	}
	g, err = tx.Gate.UpdateOneID(g.ID).SetXrayStatsPort(xp).SetSingboxStatsPort(sp).AddConfigRevision(1).Save(ctx)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return g.Unwrap(), nil
}

// resolvePortConflict 在 Gate 报告某端口被其他程序占用时，给占用它的节点或统计端口换一个端口。
func (s *Server) resolvePortConflict(ctx context.Context, g *ent.Gate, port int) error {
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockActiveGates(ctx, tx.Client(), g.ID); err != nil {
		return err
	}
	current, err := tx.Gate.Get(ctx, g.ID)
	if err != nil {
		return err
	}
	if err = s.resolvePortConflictDB(ctx, tx.Client(), current, port); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) resolvePortConflictDB(ctx context.Context, db *ent.Client, g *ent.Gate, port int) error {
	own, err := s.gateOwnPortsDB(ctx, db, g)
	if err != nil {
		return err
	}
	taken := foreignPorts(g, own)
	taken[port] = true
	uotRows, err := db.Resource.Query().Where(resource.KindEQ(uotResourceKind), resource.OwnerIDEQ(g.ID)).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range uotRows {
		p, err := decodeUoT(row)
		if err != nil {
			return err
		}
		if p.Port != port {
			continue
		}
		if p.Port, err = pickPort(own, taken); err != nil {
			return err
		}
		p.Ready, p.ReceiverRevision = false, g.ConfigRevision+1
		if err = db.Resource.UpdateOneID(row.ID).SetData(toMap(p)).Exec(ctx); err != nil {
			return err
		}
		if err = db.Gate.UpdateOneID(p.EntryID).AddConfigRevision(1).Exec(ctx); err != nil {
			return err
		}
		return db.Gate.UpdateOneID(g.ID).AddConfigRevision(1).Exec(ctx)
	}
	observerRow, observers, err := s.loadDomainObservers(ctx, db, g.ID)
	if err != nil {
		return err
	}
	for engine, o := range observers {
		if o.Port != port {
			continue
		}
		if o.Port, err = pickPort(own, taken); err != nil {
			return err
		}
		observers[engine] = o
		if err = s.saveDomainObservers(ctx, db, g.ID, observerRow, observers); err != nil {
			return err
		}
		return db.Gate.UpdateOneID(g.ID).AddConfigRevision(1).Exec(ctx)
	}
	row, bridges, err := s.loadOutboundBridges(ctx, db, g.ID)
	if err != nil {
		return err
	}
	for i := range bridges {
		if bridges[i].Port != port {
			continue
		}
		next, err := pickPort(own, taken)
		if err != nil {
			return err
		}
		bridges[i].Port = next
		if err := s.saveOutboundBridges(ctx, db, g.ID, row, bridges); err != nil {
			return err
		}
		_, err = db.Gate.UpdateOneID(g.ID).AddConfigRevision(1).Save(ctx)
		return err
	}
	if g.XrayStatsPort == port || g.SingboxStatsPort == port {
		next, err := pickPort(own, taken)
		if err != nil {
			return err
		}
		update := g.Update().AddConfigRevision(1)
		if g.XrayStatsPort == port {
			update.SetXrayStatsPort(next)
		} else {
			update.SetSingboxStatsPort(next)
		}
		_, err = update.Save(ctx)
		return err
	}
	n, err := db.Node.Query().Where(node.GateIDEQ(g.ID), node.PortEQ(port)).Only(ctx)
	if err != nil {
		return err
	}
	next, err := pickPort(own, taken)
	if err != nil {
		return err
	}
	if _, err = n.Update().SetPort(next).Save(ctx); err != nil {
		return err
	}
	_, err = db.Gate.UpdateOneID(g.ID).AddConfigRevision(1).Save(ctx)
	return err
}
