package subgroup

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/shusfun/weir/internal/subscription"
)

// Pick 是订阅组的挑选条件。机场节点经常变，订阅组只保存条件，每次按最新节点重新挑选；
// 它是处理链的第一步，在合并去重之后、算子链之前执行。各项为空表示不限制。
type Pick struct {
	Regions       []string `json:"regions,omitempty"`
	MaxMultiplier *float64 `json:"max_multiplier,omitempty"`
	// IncludeUnlabeledMultiplier 只在设了倍率上限时有意义：名字里没标倍率的节点是否保留。
	IncludeUnlabeledMultiplier bool     `json:"include_unlabeled_multiplier,omitempty"`
	Keywords                   []string `json:"keywords,omitempty"`
	// OnlyAlive 去掉检测为不可用的订阅源节点；未检测的节点、手动节点和嵌套订阅组的节点保留。
	OnlyAlive bool `json:"only_alive,omitempty"`
}

// Active 判断是否设置了任何挑选条件。
func (p Pick) Active() bool {
	return len(p.Regions) > 0 || p.MaxMultiplier != nil || len(p.Keywords) > 0 || p.OnlyAlive
}

// Validate 校验挑选条件；入口在保存订阅组设置与预览草稿时调用。
func (p Pick) Validate() error {
	if len(p.Regions) > 100 || len(p.Keywords) > 50 {
		return errors.New("pick_too_many_values")
	}
	for _, list := range [][]string{p.Regions, p.Keywords} {
		for _, v := range list {
			if v = strings.TrimSpace(v); v == "" || len([]rune(v)) > 50 {
				return errors.New("pick_value_invalid")
			}
		}
	}
	if p.MaxMultiplier != nil && (*p.MaxMultiplier < 0.01 || *p.MaxMultiplier > 100) {
		return errors.New("pick_multiplier_invalid")
	}
	return nil
}

// 节点检测状态，取值与中心的订阅源检测结果一致。
const (
	HealthAlive     = "alive"
	HealthFailed    = "failed"
	HealthUnchecked = "unchecked"
)

// NodeHealth 是一个节点在本次所用内容版本下的检测结果。
type NodeHealth struct {
	Status    string `json:"status"`
	LatencyMS *int   `json:"latency_ms,omitempty"`
}

// HealthSource 给出一个订阅源本次所用原始节点对应的检测结果查询；raw 是这次实际使用的原始节点
// （新拉取或缓存回退），调用方据此判断检测结果是否仍属于这份内容。返回 nil 表示没有检测结果。
type HealthSource func(ctx context.Context, sourceID int, raw []string) func(identity string) NodeHealth

// 处理链里每一步的名称，预览按它显示「这一步去掉了几个」。
const (
	StepDedupe     = "dedupe"
	StepRegion     = "pick_region"
	StepMultiplier = "pick_multiplier"
	StepKeyword    = "pick_keyword"
	StepAlive      = "pick_alive"
	StepOperators  = "operators"
	StepFilter     = "filter"
)

// StepCount 记录处理链一步之前的节点数与去掉的数量。
type StepCount struct {
	Step    string `json:"step"`
	Before  int    `json:"before"`
	Removed int    `json:"removed"`
}

// nodeEntry 是合并后的一条节点及其来源，挑选与预览都要知道节点从哪来。
type nodeEntry struct {
	line     string
	kind     string // source / node / profile
	sourceID int
	health   NodeHealth
}

func unchecked() NodeHealth { return NodeHealth{Status: HealthUnchecked} }

// applyPick 依次按地区、倍率、关键词、可用性挑选，返回剩余节点与每一步去掉的数量。
func applyPick(entries []nodeEntry, p Pick) ([]nodeEntry, []StepCount) {
	var steps []StepCount
	keep := func(step string, ok func(nodeEntry, NodeAttrs) bool) {
		next := entries[:0:0]
		for _, e := range entries {
			if ok(e, AttrsOf(e.line)) {
				next = append(next, e)
			}
		}
		steps = append(steps, StepCount{Step: step, Before: len(entries), Removed: len(entries) - len(next)})
		entries = next
	}
	if len(p.Regions) > 0 {
		set := map[string]bool{}
		for _, r := range p.Regions {
			set[strings.TrimSpace(r)] = true
		}
		keep(StepRegion, func(_ nodeEntry, a NodeAttrs) bool { return set[a.Region] })
	}
	if p.MaxMultiplier != nil {
		limit := *p.MaxMultiplier
		keep(StepMultiplier, func(_ nodeEntry, a NodeAttrs) bool {
			if a.Multiplier == nil {
				return p.IncludeUnlabeledMultiplier
			}
			return *a.Multiplier <= limit+1e-9
		})
	}
	if len(p.Keywords) > 0 {
		words := make([]string, 0, len(p.Keywords))
		for _, w := range p.Keywords {
			words = append(words, strings.ToLower(strings.TrimSpace(w)))
		}
		keep(StepKeyword, func(_ nodeEntry, a NodeAttrs) bool {
			name := strings.ToLower(a.Name)
			for _, w := range words {
				if strings.Contains(name, w) {
					return true
				}
			}
			return false
		})
	}
	if p.OnlyAlive {
		keep(StepAlive, func(e nodeEntry, _ NodeAttrs) bool { return e.health.Status != HealthFailed })
	}
	return entries, steps
}

// attachHealth 给订阅源节点填上检测结果；没有回调或没有记录时一律视为未检测。
func attachHealth(ctx context.Context, entries []nodeEntry, health HealthSource, sources []Source, results []SourceResult) {
	lookups := map[int]func(string) NodeHealth{}
	if health != nil {
		for i, s := range sources {
			if results[i].basis == nil {
				continue
			}
			if lookup := health(ctx, s.ID, results[i].basis); lookup != nil {
				lookups[s.ID] = lookup
			}
		}
	}
	for i := range entries {
		entries[i].health = unchecked()
		if entries[i].kind != "source" {
			continue
		}
		if lookup := lookups[entries[i].sourceID]; lookup != nil {
			if h := lookup(subscription.InventoryIdentity(entries[i].line)); h.Status != "" {
				entries[i].health = h
			}
		}
	}
}

// RegionCount 是一个地区的节点数。
type RegionCount struct {
	Region string `json:"region"`
	Count  int    `json:"count"`
}

// Available 是挑选之前的节点属性汇总。
type Available struct {
	Regions     []RegionCount `json:"regions"`
	Multipliers []float64     `json:"multipliers"`
	Unlabeled   int           `json:"unlabeled_multiplier"`
}

func summarize(entries []nodeEntry) Available {
	out := Available{Regions: []RegionCount{}, Multipliers: []float64{}}
	regions := map[string]int{}
	seen := map[float64]bool{}
	for _, e := range entries {
		a := AttrsOf(e.line)
		if _, ok := regions[a.Region]; !ok {
			out.Regions = append(out.Regions, RegionCount{Region: a.Region})
		}
		regions[a.Region]++
		if a.Multiplier == nil {
			out.Unlabeled++
		} else if !seen[*a.Multiplier] {
			seen[*a.Multiplier] = true
			out.Multipliers = append(out.Multipliers, *a.Multiplier)
		}
	}
	for i := range out.Regions {
		out.Regions[i].Count = regions[out.Regions[i].Region]
	}
	sort.SliceStable(out.Regions, func(i, j int) bool { return out.Regions[i].Count > out.Regions[j].Count })
	sort.Float64s(out.Multipliers)
	return out
}
