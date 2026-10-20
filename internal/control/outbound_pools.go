package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/device"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/ent/task"
	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/internal/subscription"
)

// A CAS no-op UPDATE locks the device without overwriting a concurrent rename
// or revocation. Pool snapshots and ordinary route/outbound tasks share it.
func lockPoolDevice(ctx context.Context, tx *ent.Tx, id int) (*ent.Device, error) {
	d, err := tx.Device.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return tx.Device.UpdateOneID(id).SetName(d.Name).Where(device.NameEQ(d.Name), device.StatusEQ("approved")).Save(ctx)
}

func pendingPoolConfiguration(ctx context.Context, tx *ent.Tx, id int, poolOnly bool) (bool, error) {
	q := tx.Task.Query().Where(task.DeviceIDEQ(id), task.Or(task.StatusEQ("running"), task.And(task.StatusEQ("pending"), task.ExpiresAtGT(time.Now()))))
	if poolOnly {
		q = q.Where(task.OperationIn("outbound.pool.apply", "outbound.reorder"))
	} else {
		q = q.Where(task.Or(task.And(task.OperationHasPrefix("outbound."), task.OperationNotIn("outbound.probe", "outbound.catalog.probe")), task.OperationHasPrefix("routing.")))
	}
	return q.Exist(ctx)
}

// A pool consumes the existing protected source service. Its private preview is
// encrypted and bounded, and never returned as a generic resource or task list.
func (s *Server) poolRoutes(admin *gin.RouterGroup) {
	admin.GET("/devices/:id/pools", s.listPools)
	admin.POST("/devices/:id/pools", s.savePool)
	admin.POST("/devices/:id/pools/order", s.orderPools)
	admin.POST("/devices/:id/pools/refresh", s.refreshPools)
	admin.POST("/devices/:id/pools/:pool/preview", s.previewPool)
	admin.POST("/devices/:id/pools/:pool/apply", s.applyPool)
}

func poolView(r *ent.Resource) gin.H {
	d := r.Data
	return gin.H{"id": r.ID, "device_id": r.OwnerID, "name": d["name"], "source_id": d["source_id"], "version": d["version"], "status": d["status"], "task_id": d["task_id"], "last_applied": d["last_applied"], "last_error": d["last_error"], "selected": d["selected"], "member_count": d["member_count"], "enabled": d["enabled"], "priority": d["priority"], "prepend": d["prepend"] == true, "auto_refresh": d["auto_refresh"] == true, "last_sync_check": d["last_sync_check"], "sync_error": d["sync_error"], "sync_unsupported": d["sync_unsupported"]}
}
func (s *Server) listPools(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	rows, err := s.Store.Resource.Query().Where(resource.KindEQ("outbound-pool"), resource.OwnerIDEQ(id)).Order(ent.Asc("id")).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := []gin.H{}
	for _, r := range rows {
		out = append(out, poolView(r))
	}
	c.JSON(200, gin.H{"items": out})
}
func poolCapable(d *ent.Device) bool {
	caps, _ := d.Metadata["capabilities"].([]any)
	for _, cap := range caps {
		if cap == "outbound.pool.v1" {
			return true
		}
	}
	return false
}
func (s *Server) savePool(c *gin.Context) {
	deviceID, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		SourceID    int    `json:"source_id"`
		Version     int    `json:"version"`
		AutoRefresh *bool  `json:"auto_refresh"`
		Prepend     *bool  `json:"prepend"`
	}
	if !bind(c, &req) {
		return
	}
	if !catalogText(req.Name, 100, false) {
		failure(c, 400, "invalid_pool_name")
		return
	}
	ctx := c.Request.Context()
	d, err := s.Store.Device.Get(ctx, deviceID)
	if err != nil || d.Status != "approved" {
		failure(c, 404, "device_not_found")
		return
	}
	if !poolCapable(d) {
		failure(c, 400, "agent_capability_required")
		return
	}
	src, err := s.Store.Resource.Get(ctx, req.SourceID)
	if err != nil || src.Kind != "sources" || src.Data["enabled"] == false {
		failure(c, 400, "active_source_required")
		return
	}
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	defer tx.Rollback()
	if _, err = lockPoolDevice(ctx, tx, deviceID); err != nil {
		failure(c, 409, "device_changed")
		return
	}
	var r *ent.Resource
	if req.ID == 0 {
		r, err = tx.Resource.Create().SetKind("outbound-pool").SetKey(randomToken(16)).SetOwnerID(deviceID).SetData(map[string]any{"name": req.Name, "source_id": req.SourceID, "version": 1, "status": "unapplied", "enabled": false, "selected": []string{}, "member_count": 0}).Save(ctx)
	} else {
		r, err = tx.Resource.UpdateOneID(req.ID).SetOwnerID(deviceID).Where(resource.KindEQ("outbound-pool"), resource.OwnerIDEQ(deviceID)).Save(ctx)
		if err == nil {
			if int(number(r.Data["version"])) != req.Version || r.Data["status"] == "pending" {
				failure(c, 409, "pool_changed_or_pending")
				return
			}
			if int(number(r.Data["source_id"])) != req.SourceID || req.AutoRefresh != nil && *req.AutoRefresh && r.Data["auto_refresh"] != true {
				r.Data["auto_armed"] = false
			}
			r.Data["name"], r.Data["source_id"], r.Data["version"] = req.Name, req.SourceID, req.Version+1
			delete(r.Data, "sync_source_digest")
			delete(r.Data, "preview")
			delete(r.Data, "preview_digest")
			r, err = tx.Resource.UpdateOne(r).SetData(r.Data).Save(ctx)
		}
	}
	if err == nil && req.AutoRefresh != nil {
		r.Data["auto_refresh"] = *req.AutoRefresh
		delete(r.Data, "next_sync")
		r, err = tx.Resource.UpdateOne(r).SetData(r.Data).Save(ctx)
	}
	if err == nil && req.Prepend != nil {
		r.Data["prepend"] = *req.Prepend
		r, err = tx.Resource.UpdateOne(r).SetData(r.Data).Save(ctx)
	}
	if err != nil || tx.Commit() != nil {
		failure(c, 409, "pool_save_failed")
		return
	}
	s.audit(c, "outbound.pool.save", fmt.Sprint(r.ID), "saved")
	c.JSON(200, poolView(r))
}

func (s *Server) ownedPool(c *gin.Context) (*ent.Resource, bool) {
	id, _ := strconv.Atoi(c.Param("id"))
	poolID, _ := strconv.Atoi(c.Param("pool"))
	r, err := s.Store.Resource.Query().Where(resource.IDEQ(poolID), resource.KindEQ("outbound-pool"), resource.OwnerIDEQ(id)).Only(c.Request.Context())
	if err != nil {
		failure(c, 404, "pool_not_found")
		return nil, false
	}
	return r, true
}

func poolOutbound(link string) (db.Outbound, error) {
	n, err := subscription.ParseLink(link)
	if err != nil {
		return db.Outbound{}, errors.New("unsupported_protocol")
	}
	var settings subscription.Settings
	if json.Unmarshal([]byte(n.SettingsJSON), &settings) != nil || !supportedSubscriptionSettings(n.Protocol, settings) {
		return db.Outbound{}, errors.New("unsupported_protocol_fields")
	}
	// Strict decoding in ManagedOutboundSettings refuses any field not mapped by
	// the managed core builders, even if subscription output supports that field.
	protocol := n.Protocol
	switch protocol {
	case "vless", "vmess", "trojan", "shadowsocks", "http", "socks5", "hysteria2", "tuic", "anytls":
	default:
		return db.Outbound{}, errors.New("unsupported_managed_transport")
	}
	if protocol == "socks5" {
		protocol = "socks"
	}
	if protocol == "http" && settings.TLS {
		protocol = "https"
	}
	user := n.Username
	if protocol == "shadowsocks" {
		user = settings.Method
	}
	out := db.Outbound{Protocol: protocol, Address: n.Address, Port: n.Port, Username: user, Password: n.Password, Remark: n.Remark, SettingsJSON: n.SettingsJSON, SubscriptionIdentity: subscription.InventoryIdentity(link), Enabled: true, Source: db.OutboundSourceSubscription}
	if db.ValidateOutboundProfile(out) != nil {
		return out, errors.New("unsupported_profile")
	}
	if _, err = db.ManagedOutboundSettings(out); err != nil {
		return out, errors.New("unsupported_managed_transport")
	}
	return out, nil
}

func (s *Server) previewPool(c *gin.Context) {
	r, ok := s.ownedPool(c)
	if !ok {
		return
	}
	var req struct {
		Version int  `json:"version"`
		Refresh bool `json:"refresh"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Version != int(number(r.Data["version"])) || r.Data["status"] == "pending" {
		failure(c, 409, "pool_changed_or_pending")
		return
	}
	ctx := c.Request.Context()
	sourceID := int(number(r.Data["source_id"]))
	src, err := s.Store.Resource.Get(ctx, sourceID)
	if err != nil || src.Kind != "sources" {
		failure(c, 400, "source_not_found")
		return
	}
	if req.Refresh {
		src, err = s.refreshSourceCache(ctx, sourceID, fetchSource)
		if err != nil {
			failure(c, 502, "pool_refresh_failed_previous_preserved")
			return
		}
	}
	links, stale, err := sourceLinks(src.Data, time.Now())
	if err != nil || stale || len(links) > 500 {
		failure(c, 409, "fresh_bounded_source_required")
		return
	}
	nodes := []db.Outbound{}
	views := []gin.H{}
	seen := map[string]bool{}
	for _, link := range links {
		n, parseErr := subscription.ParseLink(link)
		identity := subscription.InventoryIdentity(link)
		if parseErr != nil || seen[identity] {
			continue
		}
		seen[identity] = true
		out, e := poolOutbound(link)
		reason := ""
		if e != nil {
			reason = e.Error()
		} else {
			nodes = append(nodes, out)
		}
		views = append(views, gin.H{"identity": identity, "name": n.Remark, "protocol": n.Protocol, "server": n.Address, "port": n.Port, "supported": e == nil, "reason": reason})
	}
	plain, _ := json.Marshal(nodes)
	sealed, err := s.Crypto.Encrypt(fmt.Sprintf("outbound-pool:%d:", r.ID) + string(plain))
	if err != nil {
		failure(c, 503, "preview_failed")
		return
	}
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		failure(c, 503, "preview_failed")
		return
	}
	defer tx.Rollback()
	current, err := tx.Resource.UpdateOneID(r.ID).SetOwnerID(r.OwnerID).Save(ctx)
	if err != nil || int(number(current.Data["version"])) != req.Version || current.Data["status"] == "pending" {
		failure(c, 409, "pool_changed_or_pending")
		return
	}
	token := randomToken(16)
	current.Data["preview"], current.Data["preview_digest"], current.Data["preview_expires"] = sealed, digest(token), time.Now().Add(10*time.Minute).Unix()
	sourceJSON, _ := json.Marshal(src.Data)
	current.Data["preview_source_digest"] = digest(string(sourceJSON))
	if _, err = tx.Resource.UpdateOne(current).SetData(current.Data).Save(ctx); err != nil || tx.Commit() != nil {
		failure(c, 503, "preview_failed")
		return
	}
	c.JSON(200, gin.H{"items": views, "preview_token": token, "version": req.Version, "duplicates": len(links) - len(views), "expires_in": 600})
}

func poolResources(ctx context.Context, tx *ent.Tx, deviceID int) ([]db.Outbound, []db.RoutingRule, error) {
	rows, err := tx.Resource.Query().Where(resource.OwnerIDEQ(deviceID), resource.KindIn("device-outbound", "device-routing")).All(ctx)
	outs, routes := []db.Outbound{}, []db.RoutingRule{}
	for _, r := range rows {
		b, _ := json.Marshal(r.Data)
		if r.Kind == "device-outbound" {
			var out db.Outbound
			if json.Unmarshal(b, &out) != nil {
				return nil, nil, errors.New("invalid_outbound")
			}
			outs = append(outs, out)
		} else {
			var route db.RoutingRule
			if json.Unmarshal(b, &route) != nil {
				return nil, nil, errors.New("invalid_route")
			}
			routes = append(routes, route)
		}
	}
	return outs, routes, err
}

func (s *Server) applyPool(c *gin.Context) {
	r, ok := s.ownedPool(c)
	if !ok {
		return
	}
	var req struct {
		Version      int      `json:"version"`
		PreviewToken string   `json:"preview_token"`
		Selected     []string `json:"selected"`
		Enabled      bool     `json:"enabled"`
		Delete       bool     `json:"delete"`
		Confirm      bool     `json:"confirm"`
	}
	if !bind(c, &req) {
		return
	}
	if !req.Confirm || len(req.Selected) > 500 {
		failure(c, 400, "pool_confirmation_required")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	defer tx.Rollback()
	d, err := lockPoolDevice(ctx, tx, r.OwnerID)
	if err != nil || !poolCapable(d) {
		failure(c, 400, "agent_capability_required")
		return
	}
	if pending, err := pendingPoolConfiguration(ctx, tx, r.OwnerID, false); err != nil || pending {
		failure(c, 409, "pool_configuration_pending")
		return
	}
	r, err = tx.Resource.UpdateOneID(r.ID).SetOwnerID(r.OwnerID).Save(ctx)
	if err != nil || int(number(r.Data["version"])) != req.Version || r.Data["status"] == "pending" {
		failure(c, 409, "pool_changed_or_pending")
		return
	}
	nodes := []db.Outbound{}
	if req.Enabled {
		src, e := tx.Resource.Get(ctx, int(number(r.Data["source_id"])))
		if e != nil || src.Kind != "sources" || src.Data["enabled"] == false {
			failure(c, 409, "active_source_required")
			return
		}
		src, e = tx.Resource.UpdateOne(src).SetOwnerID(src.OwnerID).Save(ctx)
		if e != nil {
			failure(c, 503, "query_failed")
			return
		}
		sourceJSON, _ := json.Marshal(src.Data)
		if digest(string(sourceJSON)) != text(r.Data["preview_source_digest"]) {
			failure(c, 409, "fresh_pool_preview_required")
			return
		}
		if req.Delete || len(req.Selected) == 0 || digest(req.PreviewToken) != text(r.Data["preview_digest"]) || time.Now().Unix() > int64(number(r.Data["preview_expires"])) {
			failure(c, 409, "fresh_pool_preview_required")
			return
		}
		plain, e := s.Crypto.Decrypt(text(r.Data["preview"]))
		prefix := fmt.Sprintf("outbound-pool:%d:", r.ID)
		if e != nil || len(plain) < len(prefix) || plain[:len(prefix)] != prefix || json.Unmarshal([]byte(plain[len(prefix):]), &nodes) != nil {
			failure(c, 503, "pool_preview_unavailable")
			return
		}
		if r.Data["auto_refresh"] == true {
			chosen := map[string]bool{}
			for _, identity := range req.Selected {
				chosen[identity] = true
			}
			if len(chosen) != len(nodes) {
				failure(c, 409, "automatic_pool_requires_all_supported")
				return
			}
			for _, out := range nodes {
				if !chosen[out.SubscriptionIdentity] {
					failure(c, 409, "automatic_pool_requires_all_supported")
					return
				}
			}
			b, _ := json.Marshal(nodes)
			r.Data["pending_source_digest"] = digest(string(b))
		} else {
			delete(r.Data, "pending_source_digest")
		}
	}
	change, err := buildPoolChange(ctx, tx, r, nodes, req.Selected)
	if err != nil {
		failure(c, 409, err.Error())
		return
	}
	t, err := queuePoolChange(ctx, tx, r, change, req.Selected, req.Enabled, req.Delete)
	if err != nil || tx.Commit() != nil {
		failure(c, 503, "task_create_failed")
		return
	}
	s.audit(c, "outbound.pool.apply", fmt.Sprint(t.ID), "queued")
	c.JSON(202, gin.H{"id": t.ID, "status": "pending"})
}

func savePoolResult(ctx context.Context, tx *ent.Tx, t *ent.Task, status string) error {
	id := int(number(t.Payload["pool_id"]))
	r, err := tx.Resource.UpdateOneID(id).SetOwnerID(t.DeviceID).Where(resource.KindEQ("outbound-pool"), resource.OwnerIDEQ(t.DeviceID)).Save(ctx)
	if err != nil || int(number(r.Data["task_id"])) != t.ID {
		return errors.New("pool_task_changed")
	}
	if status == "applied" {
		b, _ := json.Marshal(t.Payload)
		var change db.PoolChange
		if json.Unmarshal(b, &change) != nil {
			return errors.New("invalid_pool_change")
		}
		outs, routes, e := poolResources(ctx, tx, t.DeviceID)
		if e != nil {
			return e
		}
		if e = change.Validate(outs, routes); e != nil {
			return e
		}
		for _, out := range outs {
			if out.Source == db.OutboundSourceSubscription && out.SubscriptionID == int64(id) {
				_, e = tx.Resource.Delete().Where(resource.KindEQ("device-outbound"), resource.KeyEQ(fmt.Sprintf("%d:%d", t.DeviceID, out.ID))).Exec(ctx)
				if e != nil {
					return e
				}
			}
		}
		for _, out := range change.Next {
			raw, _ := json.Marshal(out)
			var data map[string]any
			json.Unmarshal(raw, &data)
			if e = saveDeviceResource(ctx, tx, t.DeviceID, "outbound", data); e != nil {
				return e
			}
		}
		r.Data["selected"], r.Data["enabled"], r.Data["member_count"], r.Data["last_applied"], r.Data["last_error"] = r.Data["pending_selected"], r.Data["pending_enabled"], len(change.Next), time.Now().UTC().Format(time.RFC3339), ""
		r.Data["auto_armed"] = r.Data["auto_refresh"] == true && r.Data["pending_enabled"] == true
		if value := text(r.Data["pending_source_digest"]); value != "" {
			r.Data["sync_source_digest"] = value
		}
		if r.Data["pending_delete"] == true {
			_, e = tx.Resource.Delete().Where(resource.KindEQ("pool-node-id"), resource.OwnerIDEQ(t.DeviceID), resource.KeyHasPrefix(fmt.Sprintf("%d:", id))).Exec(ctx)
			if e != nil {
				return e
			}
			return tx.Resource.DeleteOne(r).Exec(ctx)
		}
	} else {
		r.Data["last_error"] = "apply_failed_previous_preserved"
	}
	r.Data["status"] = status
	delete(r.Data, "pending_selected")
	delete(r.Data, "pending_enabled")
	delete(r.Data, "pending_delete")
	delete(r.Data, "pending_source_digest")
	_, err = tx.Resource.UpdateOne(r).SetData(r.Data).Save(ctx)
	return err
}

// Timed-out tasks remain failed and retryable without losing the last applied pool.
func (s *Server) expirePoolTasks(ctx context.Context) {
	rows, err := s.Store.Task.Query().Where(task.OperationEQ("outbound.pool.apply"), task.StatusEQ("pending"), task.ExpiresAtLT(time.Now())).Limit(100).All(ctx)
	if err != nil {
		return
	}
	for _, t := range rows {
		tx, e := s.Store.Tx(ctx)
		if e != nil {
			return
		}
		_, e = tx.Task.UpdateOneID(t.ID).SetStatus("failed").Where(task.StatusEQ("pending")).Save(ctx)
		if e == nil {
			e = savePoolResult(ctx, tx, t, "failed")
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			tx.Rollback()
		}
	}
}
