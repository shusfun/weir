package taskqueue

import (
	"context"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/workercapability"
	"github.com/shusfun/weir/internal/store/ent/workertask"
	"github.com/shusfun/weir/packages/contracts"
)

type WorkerSnapshot struct {
	Capabilities contracts.CapabilityDocument `json:"capabilities"`
	Active       int                          `json:"active"`
}

func (q *Queue) WorkerInventory(ctx context.Context) (map[int]WorkerSnapshot, error) {
	caps, err := q.Ent.WorkerCapability.Query().Order(workercapability.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	var counts []struct {
		GateID int `json:"gate_id"`
		Count  int `json:"count"`
	}
	err = q.Ent.WorkerTask.Query().Where(workertask.StatusIn(activeStatuses...)).
		GroupBy(workertask.FieldGateID).Aggregate(ent.Count()).Scan(ctx, &counts)
	if err != nil {
		return nil, err
	}
	active := make(map[int]int, len(counts))
	for _, c := range counts {
		active[c.GateID] = c.Count
	}
	out := make(map[int]WorkerSnapshot, len(caps))
	for _, c := range caps {
		snapshot := WorkerSnapshot{Active: active[c.ID]}
		snapshot.Capabilities, err = decodeCapabilities(c.Document)
		if err != nil {
			return nil, err
		}
		out[c.ID] = snapshot
	}
	return out, nil
}
