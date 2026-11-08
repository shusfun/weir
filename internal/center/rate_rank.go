package center

import (
	"context"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/ratesample"
	"github.com/shusfun/weir/packages/contracts"
)

// 按速率排序：速率来自各节点自己的统计。Gate 每 30 秒上报每条凭据的累计流量，中心按相邻两次的下载增量与间隔
// 算出速率，按「用户 × 入口节点（× 链式代理）」累计分钟样本。排序只取最近 24 小时已完成且下载至少 1 MiB 的分钟，
// 用有效采样秒数计算速度后取第 90 百分位；没有有效分钟就是「未知」，不当成 0。

const (
	// 过滤心跳和探测流量，否则统计到的是用量而不是下载速度；在分钟累计后判断。
	rateMinMinuteBytes = 1 << 20
	rateWindow         = 24 * time.Hour
	rateMaxGap         = 5 * time.Minute // 两次上报隔太久（中心或 Gate 断过线）时不算速率
	rateMinGap         = 5 * time.Second
)

// recordRate 记下这条凭据这次上报窗口的下载量；纪元变化（内核重启）、间隔异常、没有下载时不记。
func (s *Server) recordRate(ctx context.Context, cred *ent.Credential, epoch string, down int64, now time.Time) error {
	if cred.Internal || cred.UserID == nil || cred.MeterAt == nil || cred.MeterEpoch != epoch || down < cred.MeterDown {
		return nil
	}
	gap := now.Sub(*cred.MeterAt)
	bytes := down - cred.MeterDown
	if bytes <= 0 || gap < rateMinGap || gap > rateMaxGap {
		return nil
	}
	chainID := 0
	if cred.ChainID != nil {
		chainID = *cred.ChainID
	}
	return s.Store.Ent.RateSample.Create().SetUserID(*cred.UserID).SetNodeID(cred.NodeID).SetChainID(chainID).SetAt(minuteOf(now)).
		SetBytes(bytes).SetSeconds(gap.Seconds()).
		OnConflictColumns(ratesample.FieldUserID, ratesample.FieldNodeID, ratesample.FieldChainID, ratesample.FieldAt).
		AddBytes(bytes).AddSeconds(gap.Seconds()).Exec(ctx)
}

func (s *Server) pruneRates(ctx context.Context, now time.Time) error {
	_, err := s.Store.Ent.RateSample.Delete().Where(ratesample.AtLT(now.Add(-sampleKeep))).Exec(ctx)
	return err
}

// rateStat 是一条路径（或一个节点）最近 24 小时的下载速度：P90 字节/秒，Minutes 是有效分钟数。
type rateStat struct {
	P90     float64 `json:"p90_bps"`
	Minutes int     `json:"minutes"`
	From    string  `json:"from"` // self：这个用户自己；all：全部用户（他自己没有数据时补上）
}

func (r rateStat) known() bool { return r.Minutes > 0 }

func p90(rates []float64) float64 {
	if len(rates) == 0 {
		return 0
	}
	sort.Float64s(rates)
	return rates[int(math.Ceil(0.9*float64(len(rates))))-1]
}

func rowRate(r *ent.RateSample) float64 {
	if r.Seconds <= 0 {
		return 0
	}
	return float64(r.Bytes) / r.Seconds
}

func pathKey(nodeID, chainID int) string { return itoa(nodeID) + "/" + itoa(chainID) }

// userRates 是一个用户在各条路径上的速率（自己的数据）。
func (s *Server) userRates(ctx context.Context, uid int) (map[string]rateStat, error) {
	rows, err := s.Store.Ent.RateSample.Query().Where(ratesample.UserIDEQ(uid), ratesample.AtGTE(time.Now().Add(-rateWindow)), ratesample.AtLT(minuteOf(time.Now()))).All(ctx)
	out := map[string]rateStat{}
	if err != nil {
		return nil, err
	}
	by := map[string][]float64{}
	for _, r := range rows {
		if r.Bytes < rateMinMinuteBytes || r.Seconds <= 0 {
			continue
		}
		k := pathKey(r.NodeID, r.ChainID)
		by[k] = append(by[k], rowRate(r))
	}
	for k, v := range by {
		out[k] = rateStat{P90: p90(v), Minutes: len(v), From: "self"}
	}
	return out, nil
}

// nodeRates 是各节点上全部用户直连的速率：每人每分钟各算一个速率再取第 90 百分位，
// 不把同一分钟里几个人的流量加在一起（否则人多的节点显得快）。链式代理的出口因人而异，不混进来。
func (s *Server) nodeRates(ctx context.Context, nodeIDs []int) (map[int]rateStat, error) {
	out := map[int]rateStat{}
	if len(nodeIDs) == 0 {
		return out, nil
	}
	rows, err := s.Store.Ent.RateSample.Query().Where(ratesample.NodeIDIn(nodeIDs...), ratesample.ChainIDEQ(0), ratesample.AtGTE(time.Now().Add(-rateWindow)), ratesample.AtLT(minuteOf(time.Now()))).All(ctx)
	if err != nil {
		return nil, err
	}
	by := map[int][]float64{}
	minutes := map[int]map[time.Time]bool{}
	for _, r := range rows {
		if r.Bytes < rateMinMinuteBytes || r.Seconds <= 0 {
			continue
		}
		by[r.NodeID] = append(by[r.NodeID], rowRate(r))
		if minutes[r.NodeID] == nil {
			minutes[r.NodeID] = map[time.Time]bool{}
		}
		minutes[r.NodeID][r.At] = true
	}
	for id, v := range by {
		out[id] = rateStat{P90: p90(v), Minutes: len(minutes[id]), From: "all"}
	}
	return out, nil
}

// pathRate：先用这个用户自己的；没有时用全部用户在这个节点上的（只对直连那条）。
func pathRate(own map[string]rateStat, all map[int]rateStat, nodeID, chainID int) rateStat {
	if r, ok := own[pathKey(nodeID, chainID)]; ok && r.known() {
		return r
	}
	if chainID == 0 {
		if r, ok := all[nodeID]; ok && r.known() {
			return r
		}
	}
	return rateStat{}
}

// rankedEntry 保存展示名次和节点状况，与订阅成员顺序一一对应。
type rankedEntry struct {
	Index  int
	Health nodeHealth
	Rate   rateStat
	Rank   int
	Auto   bool
}

// rankInput 是展示名次所需的线路、状况与速率。
type rankInput struct {
	keys   []string
	health []nodeHealth
	rates  []rateStat
}

// 名次只供展示；订阅成员顺序和选路不依赖速度。
func rankEntries(in rankInput) []rankedEntry {
	out := make([]rankedEntry, len(in.keys))
	var order []int
	for i := range in.keys {
		out[i] = rankedEntry{Index: i, Health: in.health[i], Rate: in.rates[i]}
		if in.health[i].Status != "down" {
			order = append(order, i)
			out[i].Auto = true
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := in.rates[order[a]], in.rates[order[b]]
		if x.known() != y.known() {
			return x.known()
		}
		return x.P90 > y.P90
	})
	for rank, i := range order {
		out[i].Rank = rank + 1
	}
	return out
}

// planned 保留线路的稳定顺序，Ranked 与 Entries 一一对应。
type planned struct {
	Entries []Entry
	Ranked  []rankedEntry
	Policy  contracts.ProxyPolicy
}

// planEntries 在输出前复核当前授权，速度只决定展示名次。
func (s *Server) planEntries(ctx context.Context, u *ent.User, entries []Entry, _ bool) (planned, error) {
	u, err := s.Store.Ent.User.Get(ctx, u.ID)
	if err != nil {
		return planned{}, err
	}
	pol := loadPolicy(u)
	if !u.Enabled {
		return planned{Policy: pol}, nil
	}
	authorized, err := s.userNodeIDs(ctx, u)
	if err != nil {
		return planned{}, err
	}
	in := rankInput{}
	var kept []Entry
	gates := map[int]*ent.Gate{}
	var nodeIDs []int
	for _, e := range entries {
		if e.URI != "" {
			nodeIDs = append(nodeIDs, e.NodeID)
		}
	}
	own, err := s.userRates(ctx, u.ID)
	if err != nil {
		return planned{}, err
	}
	all, err := s.nodeRates(ctx, nodeIDs)
	if err != nil {
		return planned{}, err
	}
	for _, e := range entries {
		if e.URI == "" {
			continue
		}
		n, err := s.Store.Ent.Node.Get(ctx, e.NodeID)
		if ent.IsNotFound(err) {
			continue
		}
		if err != nil {
			return planned{}, err
		}
		if !n.Enabled || authorized[n.ID] == nil || slices.Contains(pol.Disabled, e.Type) {
			continue
		}
		g, ok := gates[n.GateID]
		if !ok {
			g, err = s.Store.Ent.Gate.Get(ctx, n.GateID)
			if ent.IsNotFound(err) {
				continue
			}
			if err != nil {
				return planned{}, err
			}
			gates[n.GateID] = g
		}
		if g.Status != "active" {
			continue
		}
		h := s.healthOf(ctx, n, g)
		exitID := n.ExitID
		if e.ChainID != 0 {
			ch, err := s.Store.Ent.Chain.Get(ctx, e.ChainID)
			if ent.IsNotFound(err) {
				continue
			}
			if err != nil {
				return planned{}, err
			}
			if !u.AllowChain || !ch.Enabled || ch.UserID != u.ID || ch.EntryNodeID != n.ID {
				continue
			}
			exitID = ch.ExitID
		}
		ex, err := s.Store.Ent.Exit.Get(ctx, exitID)
		if ent.IsNotFound(err) {
			continue
		}
		if err != nil {
			return planned{}, err
		}
		if !ex.Enabled {
			continue
		}
		e.Chained = ex.Kind != "freedom"
		if h.Status != "down" {
			if pr, ok := s.loadExitProbe(ctx, exitOwner(exitID)); ok && !pr.OK {
				h = nodeHealth{Status: "down", Why: "出口实测失败：" + firstNonEmpty(pr.Error, pr.Note)}
			}
		}
		kept = append(kept, e)
		in.keys = append(in.keys, rankKey(e))
		in.health = append(in.health, h)
		in.rates = append(in.rates, pathRate(own, all, e.NodeID, e.ChainID))
	}
	ranked := rankEntries(in)
	p := planned{Policy: pol}
	for i, entry := range kept {
		ranked[i].Auto = !entry.Chained && ranked[i].Health.Status != "down"
		p.Entries = append(p.Entries, entry)
		p.Ranked = append(p.Ranked, ranked[i])
	}
	return p, nil
}

func rankKey(e Entry) string { return pathKey(e.NodeID, e.ChainID) }
