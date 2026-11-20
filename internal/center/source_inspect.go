package center

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/subgroup"
	"github.com/shusfun/weir/internal/subscription"
)

// 拉取后检测：在拉到订阅的那台 Gate 上用 mihomo 逐节点测一次 204 地址（复用订阅组实测的 subscription.probe 任务，
// 每批 8 个），得到可用、延迟；地区与倍率从节点名解析。中心兜底拉取时不检测，全部记为未检测。
// 检测不能挤占 Gate 的代理：那台 Gate 任务排满或负载过高时本轮让路，记下原因，留到下次刷新或补测。

// 每核负载超过这个值就让路（Gate 上报的 load 是每核平均负载）。
const probeYieldLoad = 0.85

// sourceHealthSummary 是订阅源一行上显示的检测摘要，存进 SubSource.health。
type sourceHealthSummary struct {
	Total     int            `json:"total"`
	Alive     int            `json:"alive"`
	Failed    int            `json:"failed"`
	Unchecked int            `json:"unchecked"`
	Regions   map[string]int `json:"regions"`
	// RegionAlive 是各地区的可用数；Multipliers 按倍率（"未标注" 或数值字符串）计数。
	RegionAlive map[string]int `json:"region_alive"`
	Multipliers map[string]int `json:"multipliers"`
	CheckedAt   *time.Time     `json:"checked_at,omitempty"`
	Location    string         `json:"location"`
	Reason      string         `json:"reason,omitempty"`
}

// sourceChange 是本次相对上次的节点变化，按连接身份比对，改名不算增删。
type sourceChange struct {
	Added   int      `json:"added"`
	Removed int      `json:"removed"`
	Renamed int      `json:"renamed"`
	Samples []string `json:"samples,omitempty"`
}

type inspectedNode struct {
	Identity   string   `json:"identity"`
	Name       string   `json:"name"`
	Region     string   `json:"region"`
	Multiplier *float64 `json:"multiplier,omitempty"`
	Status     string   `json:"status"`
	LatencyMS  *int     `json:"latency_ms,omitempty"`
	Why        string   `json:"why,omitempty"`
}

type sourceInspection struct {
	Health  sourceHealth
	Summary sourceHealthSummary
	Change  sourceChange
	Nodes   []inspectedNode
}

// probeSourceNodes 对一次拉取结果做检测，返回逐节点结果与摘要；不写库。
func (s *Server) probeSourceNodes(ctx context.Context, sourceID int, raw []string, location string, previous *sourceHealth) (out sourceInspection, err error) {
	finish := operationStage(ctx, "subscription.source.probe", "source_id", sourceID, "nodes", len(raw), "location", location)
	defer func() { finish(err) }()
	now := time.Now().UTC()
	h := sourceHealth{SourceID: sourceID, Generation: sourceGeneration(raw), Location: location, TraceID: taskTrace(ctx), Nodes: map[string]sourceNodeHealth{}}
	identities := make([]string, len(raw))
	for i, link := range raw {
		identities[i] = subscription.InventoryIdentity(link)
		h.Nodes[identities[i]] = sourceNodeHealth{Name: subgroup.AttrsOf(link).Name, Status: nodeUnchecked}
	}
	gid := 0
	if id, ok := strings.CutPrefix(location, "gate:"); ok {
		gid = max(atoiOr(id), 0)
	}
	switch {
	case gid == 0:
		h.Reason = "由中心直接下载，未做节点检测"
	case previous != nil && previous.Generation == h.Generation && previous.CheckedAt != nil && now.Sub(*previous.CheckedAt) < 10*time.Minute:
		// 内容没变且刚测过：沿用，不重复占用 Gate。
		h.Nodes, h.CheckedAt, h.Reason = previous.Nodes, previous.CheckedAt, previous.Reason
	default:
		if why := s.probeYield(ctx, gid); why != "" {
			h.Reason = why
			break
		}
		checks, e := s.probeSubscriptionNodes(ctx, gid, strings.Join(raw, "\n"))
		if e != nil {
			h.Reason = "检测未完成：" + e.Error()
		}
		for _, c := range checks {
			i := c.Index - 1
			if i < 0 || i >= len(raw) {
				continue
			}
			n := h.Nodes[identities[i]]
			switch c.Check.Status {
			case "passed":
				n.Status = nodeAlive
				if c.Check.LatencyMS > 0 {
					ms := int(c.Check.LatencyMS)
					n.LatencyMS = &ms
				}
			case "failed":
				n.Status, n.Why = nodeFailed, c.Check.Why
			default:
				n.Why = c.Check.Why
			}
			h.Nodes[identities[i]] = n
		}
		h.CheckedAt = &now
	}
	out.Health = h
	out.Summary = sourceHealthSummary{Regions: map[string]int{}, RegionAlive: map[string]int{}, Multipliers: map[string]int{}, CheckedAt: h.CheckedAt, Location: location, Reason: h.Reason}
	for i, link := range raw {
		a := subgroup.AttrsOf(link)
		n := h.Nodes[identities[i]]
		node := inspectedNode{Identity: identities[i], Name: a.Name, Region: a.Region, Multiplier: a.Multiplier, Status: n.Status, LatencyMS: n.LatencyMS, Why: n.Why}
		out.Nodes = append(out.Nodes, node)
		out.Summary.Total++
		out.Summary.Regions[a.Region]++
		mult := "未标注"
		if a.Multiplier != nil {
			mult = strconv.FormatFloat(*a.Multiplier, 'f', -1, 64)
		}
		out.Summary.Multipliers[mult]++
		switch n.Status {
		case nodeAlive:
			out.Summary.Alive++
			out.Summary.RegionAlive[a.Region]++
		case nodeFailed:
			out.Summary.Failed++
		default:
			out.Summary.Unchecked++
		}
	}
	out.Change = diffSourceNodes(previous, h)
	return out, nil
}

// probeYield 判断这台 Gate 现在是否该让路；返回空表示可以检测。
func (s *Server) probeYield(ctx context.Context, gid int) string {
	active, err := s.Store.Tasks.ActiveCount(ctx, gid)
	if err != nil {
		return ""
	}
	res, err := s.workerResources(ctx, gid)
	if err != nil || res == nil {
		return ""
	}
	if c := res.Resources.Concurrency; c > 0 && active >= c {
		return "「" + s.gateLabel(ctx, gid) + "」任务已满，本轮检测推迟"
	}
	if l := res.Resources.Load; l != nil && *l >= probeYieldLoad {
		return "「" + s.gateLabel(ctx, gid) + "」负载较高，本轮检测推迟，避免影响代理"
	}
	return ""
}

func diffSourceNodes(previous *sourceHealth, next sourceHealth) sourceChange {
	var ch sourceChange
	if previous == nil {
		return ch
	}
	for id, n := range next.Nodes {
		old, ok := previous.Nodes[id]
		switch {
		case !ok:
			ch.Added++
			ch.Samples = appendSample(ch.Samples, "+ "+n.Name)
		case old.Name != n.Name:
			ch.Renamed++
		}
	}
	for id, n := range previous.Nodes {
		if _, ok := next.Nodes[id]; !ok {
			ch.Removed++
			ch.Samples = appendSample(ch.Samples, "− "+n.Name)
		}
	}
	sort.Strings(ch.Samples)
	return ch
}

func appendSample(list []string, v string) []string {
	if len(list) >= 20 {
		return list
	}
	return append(list, v)
}

// inspectAfterFetch 在一次成功拉取之后检测并写回：逐节点结果、源上的摘要与变化。
func (s *Server) inspectAfterFetch(ctx context.Context, r subgroup.SourceResult) (sourceInspection, error) {
	previous, err := s.loadSourceHealth(ctx, r.SourceID)
	if err != nil {
		return sourceInspection{}, err
	}
	out, err := s.probeSourceNodes(ctx, r.SourceID, r.RawNodes, r.Location, previous)
	if err != nil {
		return out, err
	}
	return out, s.Store.Lock.Do(ctx, "subscription-source:"+itoa(r.SourceID), func(ctx context.Context) error {
		current, err := s.Store.Ent.SubSource.Get(ctx, r.SourceID)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil || toSource(current).ConfigVersion != r.ConfigVersion {
			return err
		}
		last, ok, err := s.catalogRefreshStarted(ctx, r.SourceID)
		if err != nil || !ok || !r.Started.Equal(last) {
			return err
		}
		return s.saveInspection(ctx, r.SourceID, out)
	})
}

func (s *Server) saveInspection(ctx context.Context, sourceID int, in sourceInspection) error {
	if err := s.saveSourceHealth(ctx, in.Health); err != nil {
		return err
	}
	err := s.Store.Ent.SubSource.UpdateOneID(sourceID).SetHealth(toMap(in.Summary)).SetChange(toMap(in.Change)).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

// reprobeDue：上次在 Gate 上拉取成功、检测已过刷新间隔的一半（至少 30 分钟）。
func (s *Server) reprobeDue(src *ent.SubSource, hours int, now time.Time) bool {
	if locationGate(src.LastLocation) == 0 || src.LastError != "" {
		return false
	}
	at, _ := src.Health["checked_at"].(string)
	checked, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return true
	}
	return now.Sub(checked) >= max(time.Duration(hours)*time.Hour/2, 30*time.Minute)
}

// reprobeSource 用缓存里的节点在上次拉取的 Gate 上重新检测。
func (s *Server) reprobeSource(ctx context.Context, src *ent.SubSource) error {
	source := toSource(src)
	raw, ok := kvCache{s}.ForSource(source, subgroup.SourceUserAgent(source, "clash-meta/2.5.0")).Load(ctx, src.ID)
	if !ok || len(raw) == 0 {
		return nil
	}
	previous, err := s.loadSourceHealth(ctx, src.ID)
	if err != nil {
		return err
	}
	if previous != nil {
		// 内容没变也要重测：清掉「刚测过」的判断依据。
		previous.CheckedAt = nil
	}
	in, err := s.probeSourceNodes(ctx, src.ID, raw, src.LastLocation, previous)
	if err != nil {
		return err
	}
	return s.Store.Lock.Do(ctx, "subscription-source:"+itoa(src.ID), func(ctx context.Context) error {
		current, err := s.Store.Ent.SubSource.Get(ctx, src.ID)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil || toSource(current).ConfigVersion != source.ConfigVersion ||
			current.LastAttemptAt == nil || src.LastAttemptAt == nil || !current.LastAttemptAt.Equal(*src.LastAttemptAt) {
			return err
		}
		if err = s.saveSourceHealth(ctx, in.Health); err != nil {
			return err
		}
		return current.Update().SetHealth(toMap(in.Summary)).Exec(ctx)
	})
}
