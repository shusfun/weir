package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/packages/contracts"
)

func decodeRoutingOrder(payload map[string]any) (contracts.RoutingOrder, error) {
	var order contracts.RoutingOrder
	b, err := json.Marshal(payload)
	if err != nil {
		return order, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	err = d.Decode(&order)
	return order, err
}

func (s *Server) checkRoutingOrder(c *gin.Context, id int, payload map[string]any) bool {
	d, err := s.Store.Device.Get(c.Request.Context(), id)
	if err != nil {
		failure(c, 404, "device_not_found")
		return false
	}
	capable := false
	if caps, ok := d.Metadata["capabilities"].([]any); ok {
		for _, cap := range caps {
			capable = capable || cap == "routing.order.v1"
		}
	}
	if !capable {
		failure(c, 400, "agent_capability_required")
		return false
	}
	order, err := decodeRoutingOrder(payload)
	if err != nil {
		failure(c, 400, "invalid_routing_order")
		return false
	}
	rows, err := s.Store.Resource.Query().Where(resource.KindEQ("device-routing"), resource.OwnerIDEQ(id)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return false
	}
	current := make([]contracts.RoutePosition, 0, len(rows))
	for _, r := range rows {
		current = append(current, contracts.RoutePosition{ID: int64(number(r.Data["id"])), Sort: int(number(r.Data["sort"]))})
	}
	if order.Validate(current) != nil {
		failure(c, 409, "routing_order_changed")
		return false
	}
	return true
}

func saveRoutingOrder(ctx context.Context, tx *ent.Tx, deviceID int, payload map[string]any) error {
	order, err := decodeRoutingOrder(payload)
	if err != nil {
		return err
	}
	for i, rule := range order.Rules {
		r, err := tx.Resource.Query().Where(resource.KindEQ("device-routing"), resource.OwnerIDEQ(deviceID), resource.KeyEQ(fmt.Sprintf("%d:%d", deviceID, rule.ID))).Only(ctx)
		if err != nil {
			return err
		}
		r.Data["sort"] = (i + 1) * 10
		if _, err = tx.Resource.UpdateOne(r).SetData(r.Data).Save(ctx); err != nil {
			return err
		}
	}
	return nil
}
