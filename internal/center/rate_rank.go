package center

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/ratesample"
	"github.com/shusfun/weir/packages/contracts"
)

// 按速率排序：速率来自各节点自己的统计。Gate 每 30 秒上报每条凭据的累计流量，中心按相邻两次的下载增量与间隔
// 算出速率，按「用户 × 入口节点（× 链式代理）」每分钟记一条（只记有流量的）。排序用最近 24 小时里有流量的分钟的
// 第 90 百分位：不用平均，因为不用时速率是 0，平均会把「没在用」算成「慢」。没有数据就是「未知」，不当成 0。

const (
	rateWindow     = 24 * time.Hour
	rateMaxGap     = 5 * time.Minute // 两次上报隔太久（中心或 Gate 断过线）时不算速率
	rateMinGap     = 5 * time.Second
	rateKeepFactor = 1.25 // 上一次在「自动选择」里的节点按 1.25 倍算，新节点要明显更快才换上来
)

// recordRate 记下这条凭据这次上报窗口的下载量；纪元变化（内核重启）、间隔异常、没有下载时不记。
func (s *Server) recordRate(ctx context.Context, cred *ent.Credential, epoch string, down int64, now time.Time) {
	if cred.MeterAt == nil || cred.MeterEpoch != epoch || down < cred.MeterDown {
		return
	}
	gap := now.Sub(*cred.MeterAt)
	bytes := down - cred.MeterDown
	if bytes <= 0 || gap < rateMinGap || gap > rateMaxGap {
		return
	}
	chainID := 0
	if cred.ChainID != nil {
		chainID = *cred.ChainID
	}
	_ = s.Store.Ent.RateSample.Create().SetUserID(*cred.UserID).SetNodeID(cred.NodeID).SetChainID(chainID).SetAt(minuteOf(now)).
		SetBytes(bytes).SetSeconds(gap.Seconds()).
		OnConflictColumns(ratesample.FieldUserID, ratesample.FieldNodeID, ratesample.FieldChainID, ratesample.FieldAt).
		AddBytes(bytes).AddSeconds(gap.Seconds()).Exec(ctx)
}

func (s *Server) pruneRates(ctx context.Context, now time.Time) error {
	_, err := s.Store.Ent.RateSample.Delete().Where(ratesample.AtLT(now.Add(-sampleKeep))).Exec(ctx)
	return err
}

// rateStat 是一条路径（或一个节点）最近 24 小时的下载速率：P90 字节/秒，Minutes 是有流量的分钟数。
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
func (s *Server) userRates(ctx context.Context, uid int) map[string]rateStat {
	rows, err := s.Store.Ent.RateSample.Query().Where(ratesample.UserIDEQ(uid), ratesample.AtGTE(time.Now().Add(-rateWindow))).All(ctx)
	out := map[string]rateStat{}
	if err != nil {
		return out
	}
	by := map[string][]float64{}
	for _, r := range rows {
		k := pathKey(r.NodeID, r.ChainID)
		by[k] = append(by[k], rowRate(r))
	}
	for k, v := range by {
		out[k] = rateStat{P90: p90(v), Minutes: len(v), From: "self"}
	}
	return out
}

// nodeRates 是各节点上全部用户直连的速率：每人每分钟各算一个速率再取第 90 百分位，
// 不把同一分钟里几个人的流量加在一起（否则人多的节点显得快）。链式代理的出口因人而异，不混进来。
func (s *Server) nodeRates(ctx context.Context, nodeIDs []int) map[int]rateStat {
	out := map[int]rateStat{}
	if len(nodeIDs) == 0 {
		return out
	}
	rows, err := s.Store.Ent.RateSample.Query().Where(ratesample.NodeIDIn(nodeIDs...), ratesample.ChainIDEQ(0), ratesample.AtGTE(time.Now().Add(-rateWindow))).All(ctx)
	if err != nil {
		return out
	}
	by := map[int][]float64{}
	minutes := map[int]map[time.Time]bool{}
	for _, r := range rows {
		by[r.NodeID] = append(by[r.NodeID], rowRate(r))
		if minutes[r.NodeID] == nil {
			minutes[r.NodeID] = map[time.Time]bool{}
		}
		minutes[r.NodeID][r.At] = true
	}
	for id, v := range by {
		out[id] = rateStat{P90: p90(v), Minutes: len(minutes[id]), From: "all"}
	}
	return out
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

// rankedEntry 是排序后的一个节点：Rank 从 1 起（不可用的为 0）；Auto 是否放进「自动选择」；Exploring 是不是轮换位。
type rankedEntry struct {
	Index     int
	Health    nodeHealth
	Rate      rateStat
	Rank      int
	Auto      bool
	Exploring bool
}

// rankInput 是排序要用的：每个节点的状况与速率、上一次在「自动选择」里的、这次轮到第几个没数据的节点。
type rankInput struct {
	keys   []string
	health []nodeHealth
	rates  []rateStat
	prev   map[string]bool
	turn   int
}

// rankEntries 排名次并选出「自动选择」的成员。
//   - 不可用的（VPS 离线、配置失败、外部连不上、出口实测失败）不排。
//   - 有速率的按速率从高到低：CPU 超过 80% 打八折，上一次在自动选择里的按 1.25 倍算；没速率的排在后面，保持原顺序。
//   - ranked：前 top−1 个按速率，留 1 个轮换位给还没有速率数据的节点（每次拉取轮到下一个）；没有「没数据」的节点时
//     全部按速率；top 为 1 时不留；有速率的不够时由没数据的补上。所有节点都没有速率数据时放全部可用节点。
//   - latency：全部可用节点都放进去，由客户端测延迟。
func rankEntries(in rankInput, mode string, top int) []rankedEntry {
	out := make([]rankedEntry, len(in.keys))
	var known, unknown []int
	for i := range in.keys {
		out[i] = rankedEntry{Index: i, Health: in.health[i], Rate: in.rates[i]}
		switch {
		case in.health[i].Status == "down":
		case in.rates[i].known():
			known = append(known, i)
		default:
			unknown = append(unknown, i)
		}
	}
	score := func(i int) float64 {
		v := in.rates[i].P90
		if c := in.health[i].CPU; c != nil && *c > 80 {
			v *= 0.8
		}
		if in.prev[in.keys[i]] {
			v *= rateKeepFactor
		}
		return v
	}
	sort.SliceStable(known, func(a, b int) bool { return score(known[a]) > score(known[b]) })
	for r, i := range append(append([]int{}, known...), unknown...) {
		out[i].Rank = r + 1
	}
	if mode != contracts.AutoRanked || len(known) == 0 {
		for _, i := range append(append([]int{}, known...), unknown...) {
			out[i].Auto = true
		}
		return out
	}
	slots := top
	explore := -1
	if top > 1 && len(unknown) > 0 {
		explore = unknown[in.turn%len(unknown)]
		slots = top - 1
	}
	picked := 0
	for _, i := range known {
		if picked == slots {
			break
		}
		out[i].Auto = true
		picked++
	}
	if explore >= 0 {
		out[explore].Auto, out[explore].Exploring = true, true
	}
	// 有速率的不够 top−1 个：按轮换顺序用没数据的补满
	for k := 0; picked < slots && k < len(unknown); k++ {
		i := unknown[(in.turn+1+k)%len(unknown)]
		if !out[i].Auto {
			out[i].Auto = true
			picked++
		}
	}
	return out
}

func (s *Server) loadRankPrev(ctx context.Context, uid int) map[string]bool {
	raw, ok, _ := s.Store.KV.Get(ctx, "rank:user:"+itoa(uid))
	out := map[string]bool{}
	if ok {
		for _, k := range strings.Split(raw, ",") {
			if k != "" {
				out[k] = true
			}
		}
	}
	return out
}

func (s *Server) rankTurn(ctx context.Context, uid int) int {
	raw, ok, _ := s.Store.KV.Get(ctx, "rank-turn:user:"+itoa(uid))
	if !ok {
		return 0
	}
	n, _ := strconv.Atoi(raw)
	return n
}

// planned 是一条链接按代理设置排好的节点：Entries 已按名次重排（ranked 模式），Ranked 与 Entries 一一对应。
type planned struct {
	Entries []Entry
	Ranked  []rankedEntry
	Policy  contracts.ProxyPolicy
}

// planEntries 计算节点状况、速率、名次与「自动选择」成员。remember 为真时（客户端拉取）记下这次在自动选择里的
// 节点（轮换位除外）并把轮换往前推一个；打开页面查看不影响下一次。
func (s *Server) planEntries(ctx context.Context, u *ent.User, entries []Entry, remember bool) planned {
	pol := loadPolicy(u)
	in := rankInput{prev: s.loadRankPrev(ctx, u.ID), turn: s.rankTurn(ctx, u.ID)}
	var kept []Entry
	gates := map[int]*ent.Gate{}
	var nodeIDs []int
	for _, e := range entries {
		if e.URI != "" {
			nodeIDs = append(nodeIDs, e.NodeID)
		}
	}
	own, all := s.userRates(ctx, u.ID), s.nodeRates(ctx, nodeIDs)
	for _, e := range entries {
		if e.URI == "" {
			continue
		}
		n, err := s.Store.Ent.Node.Get(ctx, e.NodeID)
		if err != nil {
			continue
		}
		g, ok := gates[n.GateID]
		if !ok {
			g, _ = s.Store.Ent.Gate.Get(ctx, n.GateID)
			gates[n.GateID] = g
		}
		h := s.healthOf(ctx, n, g)
		if e.ChainID != 0 && h.Status != "down" {
			if pr, ok := s.loadExitProbe(ctx, "chain:"+itoa(e.ChainID)); ok && !pr.OK {
				h = nodeHealth{Status: "down", Why: "出口实测失败：" + firstNonEmpty(pr.Error, pr.Note)}
			}
		}
		kept = append(kept, e)
		in.keys = append(in.keys, rankKey(e))
		in.health = append(in.health, h)
		in.rates = append(in.rates, pathRate(own, all, e.NodeID, e.ChainID))
	}
	ranked := rankEntries(in, pol.Auto.Mode, pol.Auto.Top)
	p := planned{Policy: pol}
	order := make([]int, len(ranked))
	for i := range order {
		order[i] = i
	}
	if pol.Auto.Mode == contracts.AutoRanked {
		// 按名次重排：可用的在前，按名次；不可用的在后，保持原顺序
		sort.SliceStable(order, func(a, b int) bool {
			ra, rb := ranked[order[a]].Rank, ranked[order[b]].Rank
			if (ra == 0) != (rb == 0) {
				return ra != 0
			}
			return ra < rb
		})
	}
	var keep []string
	for _, i := range order {
		p.Entries = append(p.Entries, kept[i])
		p.Ranked = append(p.Ranked, ranked[i])
		if ranked[i].Auto && !ranked[i].Exploring && ranked[i].Rate.known() {
			keep = append(keep, in.keys[i])
		}
	}
	if remember {
		_ = s.Store.KV.Set(ctx, "rank:user:"+itoa(u.ID), strings.Join(keep, ","), 7*24*time.Hour)
		_ = s.Store.KV.Set(ctx, "rank-turn:user:"+itoa(u.ID), strconv.Itoa(in.turn+1), 7*24*time.Hour)
	}
	return p
}

func rankKey(e Entry) string { return pathKey(e.NodeID, e.ChainID) }
