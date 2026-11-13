package center

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/selfupdate"
	"time"
)

func (s *Server) getHostOperation(c *gin.Context) {
	st, ok := selfupdate.ReadOperation(c.Param("operation"))
	if !ok {
		failure(c, 404, "host_operation_not_found")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, st)
}

func (s *Server) previewLayaPreparation(c *gin.Context) {
	if why := selfUpdateBlocker(); why != "" {
		failDetail(c, 409, "laya_preparation_unavailable", why, nil)
		return
	}
	p := localGatePreview{Actor: c.GetInt("user_id"), Role: selfupdate.LocalRole(), Expires: time.Now().Add(10 * time.Minute), Action: "prepare-laya"}
	id := randomHex(16)
	b, _ := json.Marshal(p)
	if err := s.Store.KV.Set(c.Request.Context(), "host-preview:"+id, string(b), 10*time.Minute); err != nil {
		failInternal(c, 503, "save_failed", err)
		return
	}
	c.JSON(200, gin.H{"id": id, "expires_at": p.Expires})
}

func (s *Server) confirmLayaPreparation(c *gin.Context) {
	ctx := c.Request.Context()
	err := s.Store.Lock.Do(ctx, "host-operation:laya:"+c.Param("id"), func(ctx context.Context) error {
		raw, ok, err := s.Store.KV.Get(ctx, "host-preview:"+c.Param("id"))
		if err != nil {
			return err
		}
		var p localGatePreview
		if !ok || json.Unmarshal([]byte(raw), &p) != nil || p.Actor != c.GetInt("user_id") || p.Action != "prepare-laya" || p.Role != selfupdate.LocalRole() || time.Now().After(p.Expires) {
			failure(c, 409, "host_preview_expired")
			return nil
		}
		id := "laya-" + c.Param("id")
		if st, ok := selfupdate.ReadOperation(id); ok {
			c.JSON(200, st)
			return nil
		}
		if err = selfupdate.WriteRequest(selfupdate.Request{ID: id, Action: p.Action, TraceID: taskTrace(ctx)}); err != nil {
			return err
		}
		s.audit(c, "laya.prepare", "center", "queued")
		c.JSON(202, gin.H{"id": id, "state": "queued"})
		return nil
	})
	if err != nil {
		failInternal(c, 409, "host_operation_busy", err)
	}
}
