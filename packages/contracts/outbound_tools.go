package contracts

import "errors"

type CatalogProbe struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	Key     string `json:"key"`
}

// Full manual inventory, including disabled entries. Sort is the observed value.
type OutboundOrder struct {
	Items []RoutePosition `json:"items"`
}

func (o OutboundOrder) Validate(current []RoutePosition) error {
	if (RoutingOrder{Rules: o.Items}).Validate(current) != nil {
		return errors.New("outbound_order_changed")
	}
	return nil
}

type OutboundProbeItem struct {
	ID     int64  `json:"id"`
	Digest string `json:"digest"`
}
type OutboundProbe struct {
	Items []OutboundProbeItem `json:"items"`
}

func (p OutboundProbe) Validate() error {
	if len(p.Items) == 0 || len(p.Items) > 50 {
		return errors.New("invalid_outbound_probe")
	}
	seen := map[int64]bool{}
	for _, item := range p.Items {
		if item.ID <= 0 || len(item.Digest) != 64 || seen[item.ID] {
			return errors.New("invalid_outbound_probe")
		}
		seen[item.ID] = true
	}
	return nil
}
