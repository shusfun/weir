package center

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/node"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/route"
	"github.com/shusfun/weir/packages/contracts"
)

const ruleMeterKind = "rule-meter"
const ruleDailyKind = "rule-traffic-daily"

type ruleBaseline struct {
	Epoch    string                    `json:"epoch"`
	Revision int64                     `json:"revision"`
	Sample   contracts.RuleMeterSample `json:"sample"`
}

type ruleDaily struct {
	RuleID     int       `json:"rule_id"`
	Up         int64     `json:"up,string"`
	Down       int64     `json:"down,string"`
	ObservedAt time.Time `json:"observed_at"`
}

// Called within the same transaction as the epoch/sequence acknowledgement.
func (s *Server) applyRuleMeter(ctx context.Context, g *ent.Gate, rep contracts.MeterReport, now time.Time) (err error) {
	finish := operationStage(ctx, "traffic.rules.report", "gate_id", g.ID, "samples", len(rep.Rules))
	defer func() { finish(err) }()
	for _, sample := range rep.Rules {
		r, e := s.Store.Ent.Route.Query().Where(route.IDEQ(int(sample.RuleID)), route.GateIDEQ(g.ID)).Only(ctx)
		if ent.IsNotFound(e) {
			continue
		}
		if e != nil {
			return e
		}
		// A Gate can only report its own current rule and its consuming engine.
		q := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID), node.KernelEQ(sample.Engine))
		if r.NodeID != nil {
			q.Where(node.IDEQ(*r.NodeID))
		}
		if exists, e := q.Exist(ctx); e != nil {
			return e
		} else if !exists {
			continue
		}
		key := fmt.Sprintf("%d:%s:%d", g.ID, sample.Engine, sample.RuleID)
		row, e := s.Store.Ent.Resource.Query().Where(resource.KindEQ(ruleMeterKind), resource.KeyEQ(key)).Only(ctx)
		if e != nil && !ent.IsNotFound(e) {
			return e
		}
		var old ruleBaseline
		if row != nil {
			if e = decodeResource(row, &old); e != nil {
				return e
			}
		}
		if _, ok := meterDelta(old.Epoch, old.Sample.Up, old.Sample.Down, rep.Epoch, sample.Up, sample.Down); !ok {
			return fmt.Errorf("rule counter regressed")
		}
		up, down := sample.Up, sample.Down
		if old.Epoch == rep.Epoch {
			up -= old.Sample.Up
			down -= old.Sample.Down
		}
		if old.Epoch == rep.Epoch && sample.ObservedAt.Before(old.Sample.ObservedAt) {
			return fmt.Errorf("rule observation regressed")
		}
		next := ruleBaseline{Epoch: rep.Epoch, Revision: rep.Revision, Sample: sample}
		if row == nil {
			e = s.Store.Ent.Resource.Create().SetKind(ruleMeterKind).SetKey(key).SetOwnerID(g.ID).SetData(toMap(next)).Exec(ctx)
		} else {
			e = row.Update().SetData(toMap(next)).Exec(ctx)
		}
		if e != nil {
			return e
		}
		dayKey := fmt.Sprintf("%s:%d:%d", now.Format("2006-01-02"), g.ID, sample.RuleID)
		dayRow, e := s.Store.Ent.Resource.Query().Where(resource.KindEQ(ruleDailyKind), resource.KeyEQ(dayKey)).Only(ctx)
		if e != nil && !ent.IsNotFound(e) {
			return e
		}
		d := ruleDaily{RuleID: int(sample.RuleID)}
		if dayRow != nil {
			if e = decodeResource(dayRow, &d); e != nil {
				return e
			}
		}
		if up > math.MaxInt64-d.Up || down > math.MaxInt64-d.Down {
			return fmt.Errorf("rule traffic overflow")
		}
		d.Up += up
		d.Down += down
		if sample.ObservedAt.After(d.ObservedAt) {
			d.ObservedAt = sample.ObservedAt
		}
		if dayRow == nil {
			e = s.Store.Ent.Resource.Create().SetKind(ruleDailyKind).SetKey(dayKey).SetOwnerID(g.ID).SetData(toMap(d)).Exec(ctx)
		} else {
			e = dayRow.Update().SetData(toMap(d)).Exec(ctx)
		}
		if e != nil {
			return e
		}
	}
	return nil
}

type ruleUsageView struct {
	Bytes      *int64     `json:"bytes"`
	ObservedAt *time.Time `json:"observed_at"`
	Coverage   string     `json:"coverage"`
	Reason     string     `json:"reason"`
}

func (s *Server) ruleUsageViews(ctx context.Context, g *ent.Gate, rules []*ent.Route, now time.Time) (out map[int]ruleUsageView, err error) {
	finish := operationStage(ctx, "traffic.rules.aggregate", "gate_id", g.ID)
	defer func() { finish(err) }()
	out = map[int]ruleUsageView{}
	baselines, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(ruleMeterKind), resource.OwnerIDEQ(g.ID)).All(ctx)
	if err != nil {
		return nil, err
	}
	latest := map[int]map[string]ruleBaseline{}
	for _, row := range baselines {
		var b ruleBaseline
		if err = decodeResource(row, &b); err != nil {
			return nil, err
		}
		id := int(b.Sample.RuleID)
		if latest[id] == nil {
			latest[id] = map[string]ruleBaseline{}
		}
		latest[id][b.Sample.Engine] = b
	}
	days, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(ruleDailyKind), resource.OwnerIDEQ(g.ID), resource.KeyGTE(now.AddDate(0, 0, -29).Format("2006-01-02")+":")).All(ctx)
	if err != nil {
		return nil, err
	}
	totals := map[int]int64{}
	for _, row := range days {
		var d ruleDaily
		if err = decodeResource(row, &d); err != nil {
			return nil, err
		}
		if d.Up > math.MaxInt64-d.Down || totals[d.RuleID] > math.MaxInt64-d.Up-d.Down {
			return nil, fmt.Errorf("rule traffic overflow")
		}
		totals[d.RuleID] += d.Up + d.Down
	}
	nodes, err := s.Store.Ent.Node.Query().Where(node.GateIDEQ(g.ID), node.EnabledEQ(true)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		v := ruleUsageView{Coverage: "unknown", Reason: "尚未收到此规则的原生出口计数；旧 VPS 版本不提供此能力"}
		engines := map[string]bool{}
		for _, n := range nodes {
			if r.NodeID == nil || *r.NodeID == n.ID {
				engines[n.Kernel] = true
			}
		}
		observed, fresh := 0, 0
		for engine := range engines {
			b, ok := latest[r.ID][engine]
			if !ok {
				continue
			}
			observed++
			at := b.Sample.ObservedAt
			if v.ObservedAt == nil || at.After(*v.ObservedAt) {
				copy := at
				v.ObservedAt = &copy
			}
			if b.Sample.Status == "fresh" && now.Sub(at) <= 2*time.Minute && b.Revision == g.AppliedRevision {
				fresh++
			}
		}
		if observed > 0 {
			bytes := totals[r.ID]
			v.Bytes = &bytes
			v.Coverage, v.Reason = "partial", "近 30 天已观测流量；部分内核未覆盖或最近观测已过期，不参与额度扣费"
			if fresh == len(engines) && fresh > 0 {
				v.Coverage, v.Reason = "native", "近 30 天原生出口累计；断线增量计入收到当天，可能包含内核探测流量，不参与额度扣费"
			}
		}
		out[r.ID] = v
	}
	return out, nil
}
