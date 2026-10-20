package agent

import (
	"errors"
	"fmt"
	"github.com/imzyb/MiGate/packages/contracts"
)

func (a *Agent) reorderRoutes(t contracts.Task) error {
	var order contracts.RoutingOrder
	if err := decode(t.Payload, &order); err != nil {
		return errors.New("routing_order_changed")
	}
	current := make([]contracts.RoutePosition, 0, len(a.State.Routing))
	for _, r := range a.State.Routing {
		if a.State.Versions[fmt.Sprintf("routing:%d", r.ID)] > t.Revision {
			return errors.New("stale_resource_revision")
		}
		current = append(current, contracts.RoutePosition{ID: r.ID, Sort: r.Sort})
	}
	if a.State.Versions["routing:0"] == t.Revision && t.Revision > 0 {
		return nil
	}
	if err := order.Validate(current); err != nil {
		return err
	}
	if a.State.Versions == nil {
		a.State.Versions = map[string]int64{}
	}
	positions := map[int64]int{}
	for i, r := range order.Rules {
		positions[r.ID] = (i + 1) * 10
	}
	for i := range a.State.Routing {
		r := &a.State.Routing[i]
		r.Sort = positions[r.ID]
		a.State.Versions[fmt.Sprintf("routing:%d", r.ID)] = t.Revision
	}
	return nil
}
