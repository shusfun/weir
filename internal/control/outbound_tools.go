package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/ent/task"
	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/packages/contracts"
)

func decodeOutboundTool(payload map[string]any, out any) error {
	b, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(out)
}

func (s *Server) prepareOutboundTool(c *gin.Context, tx *ent.Tx, id int, op string, payload map[string]any) (map[string]any, bool) {
	d, e := tx.Device.Get(c.Request.Context(), id)
	capable := false
	if d != nil {
		if caps, ok := d.Metadata["capabilities"].([]any); ok {
			for _, cap := range caps {
				capable = capable || cap == "outbound.tools.v1"
			}
		}
	}
	if e != nil || !capable {
		failure(c, 400, "agent_capability_required")
		return nil, false
	}
	if op == "outbound.catalog.probe" && !c.GetBool("proxy_catalog_trusted") {
		failure(c, 400, "invalid_outbound_probe")
		return nil, false
	}
	outs, _, e := poolResources(c.Request.Context(), tx, id)
	if e != nil {
		failure(c, 503, "query_failed")
		return nil, false
	}
	if op == "outbound.reorder" {
		if pending, e := pendingPoolConfiguration(c.Request.Context(), tx, id, false); e != nil || pending {
			failure(c, 409, "pool_configuration_pending")
			return nil, false
		}
		var order contracts.OutboundOrder
		if decodeOutboundTool(payload, &order) != nil {
			failure(c, 400, "invalid_outbound_order")
			return nil, false
		}
		current := []contracts.RoutePosition{}
		for _, out := range outs {
			if out.Source != db.OutboundSourceSubscription {
				current = append(current, contracts.RoutePosition{ID: out.ID, Sort: out.Sort})
			}
		}
		if order.Validate(current) != nil {
			failure(c, 409, "outbound_order_changed")
			return nil, false
		}
		return payload, true
	}
	if op == "outbound.catalog.probe" {
		var req contracts.CatalogProbe
		if decodeOutboundTool(payload, &req) != nil || !publicCatalogIP(req.Address) || req.Port < 1 || req.Port > 65535 || len(req.Key) != 64 {
			failure(c, 400, "invalid_outbound_probe")
			return nil, false
		}
		pending, e := tx.Task.Query().Where(task.DeviceIDEQ(id), task.OperationIn("outbound.probe", "outbound.catalog.probe"), task.StatusIn("pending", "running"), task.ExpiresAtGT(time.Now())).Exist(c.Request.Context())
		if e != nil || pending {
			failure(c, 409, "outbound_probe_pending")
			return nil, false
		}
		if !s.rate(c, fmt.Sprintf("outbound-probe-%d", id), 3) {
			return nil, false
		}
		return payload, true
	}
	// The browser can select IDs only. Digests come from committed server state.
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if decodeOutboundTool(payload, &req) != nil || len(req.IDs) == 0 || len(req.IDs) > 50 {
		failure(c, 400, "invalid_outbound_probe")
		return nil, false
	}
	pending, e := tx.Task.Query().Where(task.DeviceIDEQ(id), task.OperationIn("outbound.probe", "outbound.catalog.probe"), task.StatusIn("pending", "running"), task.ExpiresAtGT(time.Now())).Exist(c.Request.Context())
	if e != nil || pending {
		failure(c, 409, "outbound_probe_pending")
		return nil, false
	}
	byID := map[int64]db.Outbound{}
	for _, out := range outs {
		byID[out.ID] = out
	}
	probe := contracts.OutboundProbe{}
	for _, id := range req.IDs {
		out, ok := byID[id]
		if !ok {
			failure(c, 409, "outbound_inventory_changed")
			return nil, false
		}
		probe.Items = append(probe.Items, contracts.OutboundProbeItem{ID: id, Digest: db.PoolMemberDigest(out)})
	}
	if probe.Validate() != nil {
		failure(c, 400, "invalid_outbound_probe")
		return nil, false
	}
	if !s.rate(c, fmt.Sprintf("outbound-probe-%d", id), 3) {
		return nil, false
	}
	b, _ := json.Marshal(probe)
	var result map[string]any
	json.Unmarshal(b, &result)
	return result, true
}

func saveOutboundOrder(ctx context.Context, tx *ent.Tx, deviceID int, payload map[string]any) error {
	var order contracts.OutboundOrder
	if decodeOutboundTool(payload, &order) != nil {
		return errors.New("invalid_outbound_order")
	}
	outs, _, e := poolResources(ctx, tx, deviceID)
	if e != nil {
		return e
	}
	current := []contracts.RoutePosition{}
	for _, out := range outs {
		if out.Source != db.OutboundSourceSubscription {
			current = append(current, contracts.RoutePosition{ID: out.ID, Sort: out.Sort})
		}
	}
	if e = order.Validate(current); e != nil {
		return e
	}
	for i, item := range order.Items {
		r, e := tx.Resource.Query().Where(resource.OwnerIDEQ(deviceID), resource.KindEQ("device-outbound"), resource.KeyEQ(fmt.Sprintf("%d:%d", deviceID, item.ID))).Only(ctx)
		if e != nil {
			return e
		}
		r.Data["sort"] = (i + 1) * 10
		if _, e = tx.Resource.UpdateOne(r).SetData(r.Data).Save(ctx); e != nil {
			return e
		}
	}
	return nil
}

// Project only bounded, typed observations. Do not persist arbitrary Agent fields.
func outboundProbeResult(payload, data map[string]any) (map[string]any, error) {
	var probe contracts.OutboundProbe
	if decodeOutboundTool(payload, &probe) != nil || probe.Validate() != nil {
		return nil, errors.New("invalid_probe_result")
	}
	at, e := time.Parse(time.RFC3339Nano, text(data["observed_at"]))
	if e != nil || at.After(time.Now().Add(time.Minute)) || time.Since(at) > 5*time.Minute || data["source"] != "gate_tcp_connect" {
		return nil, errors.New("invalid_probe_result")
	}
	raw, ok := data["items"].([]any)
	if !ok || len(raw) != len(probe.Items) {
		return nil, errors.New("invalid_probe_result")
	}
	wanted := map[int64]bool{}
	for _, item := range probe.Items {
		wanted[item.ID] = true
	}
	items := []map[string]any{}
	for _, value := range raw {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("invalid_probe_result")
		}
		id := int64(number(item["id"]))
		status := text(item["status"])
		if !wanted[id] {
			return nil, errors.New("invalid_probe_result")
		}
		delete(wanted, id)
		out := map[string]any{"id": id, "status": status}
		switch status {
		case "reachable":
			ms, ok := item["latency_ms"].(float64)
			if !ok || ms < 0 || ms > 3000 || ms != float64(int64(ms)) {
				return nil, errors.New("invalid_probe_result")
			}
			out["latency_ms"] = ms
		case "unreachable", "unknown", "blocked", "inventory_changed":
		default:
			return nil, errors.New("invalid_probe_result")
		}
		items = append(items, out)
	}
	return map[string]any{"source": "gate_tcp_connect", "observed_at": at.UTC().Format(time.RFC3339Nano), "items": items}, nil
}
