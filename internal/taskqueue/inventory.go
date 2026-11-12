package taskqueue

import (
	"context"

	"github.com/shusfun/weir/packages/contracts"
)

type WorkerSnapshot struct {
	Capabilities contracts.CapabilityDocument `json:"capabilities"`
	Active       int                          `json:"active"`
}

func (q *Queue) WorkerInventory(ctx context.Context) (map[int]WorkerSnapshot, error) {
	rows, err := q.DB.QueryContext(ctx, `SELECT c.gate_id,c.document,COALESCE(a.active,0)
		FROM worker_capabilities c LEFT JOIN
		(SELECT gate_id,count(*) AS active FROM worker_tasks WHERE status IN ('leased','cancelling') GROUP BY gate_id) a
		ON a.gate_id=c.gate_id ORDER BY c.gate_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]WorkerSnapshot{}
	for rows.Next() {
		var id int
		var raw string
		var snapshot WorkerSnapshot
		if err := rows.Scan(&id, &raw, &snapshot.Active); err != nil {
			return nil, err
		}
		if err := contracts.DecodeStrict([]byte(raw), &snapshot.Capabilities, 256<<10); err != nil {
			return nil, err
		}
		if err := snapshot.Capabilities.Validate(); err != nil {
			return nil, err
		}
		out[id] = snapshot
	}
	return out, rows.Err()
}
