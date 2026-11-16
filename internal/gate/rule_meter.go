package gate

import (
	"fmt"
	"math"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

type RuleCounter struct {
	Sample contracts.RuleMeterSample `json:"sample"`
	Last   Counter                   `json:"last"`
}

func (st *State) markRulesUnavailable(wanted map[string]map[string]int64) {
	for key, row := range st.RuleCounters {
		keep := false
		for _, id := range wanted[row.Sample.Engine] {
			if id == row.Sample.RuleID {
				keep = true
				break
			}
		}
		if !keep {
			delete(st.RuleCounters, key)
			continue
		}
		row.Sample.Status = "unavailable"
		st.RuleCounters[key] = row
	}
}

func (st *State) absorbRules(engine string, raw map[int64]Counter, restarted bool, at time.Time) {
	if st.RuleCounters == nil {
		st.RuleCounters = map[string]RuleCounter{}
	}
	for id, r := range raw {
		if id <= 0 || r.Up < 0 || r.Down < 0 {
			continue
		}
		key := fmt.Sprintf("%s:%d", engine, id)
		row := st.RuleCounters[key]
		delta := r
		if !restarted && r.Up >= row.Last.Up && r.Down >= row.Last.Down {
			delta.Up -= row.Last.Up
			delta.Down -= row.Last.Down
		}
		if row.Sample.Up > math.MaxInt64-delta.Up || row.Sample.Down > math.MaxInt64-delta.Down {
			continue
		}
		row.Sample.RuleID, row.Sample.Engine = id, engine
		row.Sample.Up += delta.Up
		row.Sample.Down += delta.Down
		row.Sample.ObservedAt, row.Sample.Status = at, "fresh"
		row.Last = r
		st.RuleCounters[key] = row
	}
}
