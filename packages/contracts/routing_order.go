package contracts

import "errors"

// Rules are in desired priority order; Sort is the previously observed value.
// Only order changes: Agent and center retain the current rule attributes.
type RoutePosition struct {
	ID   int64 `json:"id"`
	Sort int   `json:"sort"`
}
type RoutingOrder struct {
	Rules []RoutePosition `json:"rules"`
}

func (o RoutingOrder) Validate(current []RoutePosition) error {
	if len(o.Rules) == 0 || len(o.Rules) > 1000 || len(o.Rules) != len(current) {
		return errors.New("routing_order_changed")
	}
	before := map[int64]int{}
	for _, r := range current {
		if r.ID <= 0 {
			return errors.New("routing_order_changed")
		}
		before[r.ID] = r.Sort
	}
	if len(before) != len(current) {
		return errors.New("routing_order_changed")
	}
	for _, r := range o.Rules {
		sort, ok := before[r.ID]
		if !ok || sort != r.Sort {
			return errors.New("routing_order_changed")
		}
		delete(before, r.ID)
	}
	return nil
}
