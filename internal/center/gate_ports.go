package center

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/node"
)

const (
	portMin = 20000
	portMax = 60000
)

// reservedPorts 永不分配：SSH、网站、常见面板与中心默认端口。
var reservedPorts = map[int]bool{22: true, 80: true, 443: true, 8080: true, 8443: true, 19810: true}

// gateOwnPorts 是本项目在该 Gate 上已分配的端口（节点入站 + 统计端口）。
func (s *Server) gateOwnPorts(ctx context.Context, g *ent.Gate) (map[int]bool, error) {
	own := map[int]bool{}
	nodes, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		own[n.Port] = true
	}
	if g.XrayStatsPort > 0 {
		own[g.XrayStatsPort] = true
	}
	if g.SingboxStatsPort > 0 {
		own[g.SingboxStatsPort] = true
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
	own, err := s.gateOwnPorts(ctx, g)
	if err != nil {
		return 0, err
	}
	return pickPort(own, foreignPorts(g, own))
}

// ensureStatsPorts 为 Gate 分配两个只监听回环的统计端口；已分配且未与其他程序冲突时保持不变。
func (s *Server) ensureStatsPorts(ctx context.Context, g *ent.Gate) (*ent.Gate, error) {
	own, err := s.gateOwnPorts(ctx, g)
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
		return g, nil
	}
	return g.Update().SetXrayStatsPort(xp).SetSingboxStatsPort(sp).AddConfigRevision(1).Save(ctx)
}

// resolvePortConflict 在 Gate 报告某端口被其他程序占用时，给占用它的节点或统计端口换一个端口。
func (s *Server) resolvePortConflict(ctx context.Context, g *ent.Gate, port int) error {
	own, err := s.gateOwnPorts(ctx, g)
	if err != nil {
		return err
	}
	taken := foreignPorts(g, own)
	taken[port] = true
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
	n, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID), node.PortEQ(port)).Only(ctx)
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
	_, err = s.bumpGate(ctx, g.ID)
	return err
}
