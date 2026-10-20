package control

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
)

// Pool ordering is presentation only, like the upstream subscription priority.
// Route matching remains the independent, acknowledged routing order contract.
func (s *Server) orderPools(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Items []struct {
			ID      int `json:"id"`
			Version int `json:"version"`
		} `json:"items"`
	}
	if !bind(c, &req) {
		return
	}
	if len(req.Items) < 1 || len(req.Items) > 1000 {
		failure(c, 400, "invalid_pool_order")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	defer tx.Rollback()
	if _, err = lockPoolDevice(ctx, tx, id); err != nil {
		failure(c, 409, "device_changed")
		return
	}
	rows, err := tx.Resource.Query().Where(resource.KindEQ("outbound-pool"), resource.OwnerIDEQ(id)).Order(ent.Asc("id")).All(ctx)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	byID := map[int]*ent.Resource{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if len(rows) != len(req.Items) {
		failure(c, 409, "pool_order_changed")
		return
	}
	for i, item := range req.Items {
		r := byID[item.ID]
		if r == nil || int(number(r.Data["version"])) != item.Version || r.Data["status"] == "pending" {
			failure(c, 409, "pool_order_changed")
			return
		}
		delete(byID, item.ID)
		r.Data["priority"], r.Data["version"] = (i+1)*10, item.Version+1
		delete(r.Data, "preview")
		delete(r.Data, "preview_digest")
		if _, err = tx.Resource.UpdateOne(r).SetData(r.Data).Save(ctx); err != nil {
			failure(c, 503, "save_failed")
			return
		}
	}
	if tx.Commit() != nil {
		failure(c, 503, "save_failed")
		return
	}
	s.audit(c, "outbound.pool.order", strconv.Itoa(id), "saved")
	c.JSON(200, gin.H{"saved": true})
}

func (s *Server) refreshPools(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if !s.rate(c, "refresh-pools-"+strconv.Itoa(id), 2) {
		return
	}
	rows, err := s.Store.Resource.Query().Where(resource.KindEQ("outbound-pool"), resource.OwnerIDEQ(id)).Order(ent.Asc("id")).Limit(101).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	if len(rows) > 100 {
		failure(c, 400, "pool_refresh_limit")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	sources := map[int]bool{}
	results := []gin.H{}
	for _, r := range rows {
		sourceID := int(number(r.Data["source_id"]))
		ok, known := sources[sourceID]
		if !known {
			callCtx, done := context.WithTimeout(ctx, 8*time.Second)
			_, e := s.refreshSourceCache(callCtx, sourceID, fetchSource)
			done()
			ok = e == nil
			sources[sourceID] = ok
		}
		result := "source_refresh_failed_previous_preserved"
		if ok {
			result = "manual_preview_required"
			if r.Data["auto_refresh"] == true && r.Data["enabled"] == true {
				if e := s.syncPool(ctx, r.ID, true); e != nil {
					result = "pool_configuration_pending"
				} else {
					result = "sync_checked"
				}
			}
		}
		results = append(results, gin.H{"pool_id": r.ID, "result": result})
	}
	s.audit(c, "outbound.pool.refresh", strconv.Itoa(id), "reported")
	c.JSON(200, gin.H{"items": results})
}
