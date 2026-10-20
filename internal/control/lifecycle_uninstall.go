package control

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func UninstallDispatch(capable bool, mode string, scopeConfirmed bool) (string, bool) {
	switch mode {
	case "retain", "cores", "purge":
	default:
		return "refused", false
	}
	if mode != "retain" && !scopeConfirmed {
		return "scope_confirmation_required", false
	}
	if !capable {
		return "pending", false
	}
	return "pending", true
}

func ShouldRevokeAfterUninstall(operation, status string) bool {
	return operation == "lifecycle.uninstall" && status == "applied"
}

func (s *Server) uninstallGate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Confirm        bool   `json:"confirm"`
		Mode           string `json:"mode"`
		ScopeConfirmed bool   `json:"scope_confirmed"`
	}
	if !bind(c, &req) || !req.Confirm || id <= 0 {
		failure(c, 400, "uninstall_confirmation_required")
		return
	}
	device, err := s.Store.Device.Get(c.Request.Context(), id)
	if err != nil || device.Status != "approved" {
		failure(c, 404, "device_not_found")
		return
	}
	status, enqueue := UninstallDispatch(deviceCapable(device.Metadata, "lifecycle.uninstall.v1"), req.Mode, req.ScopeConfirmed)
	switch status {
	case "refused":
		failure(c, 400, "uninstall_mode_refused")
		return
	case "scope_confirmation_required":
		failure(c, 400, "uninstall_scope_confirmation_required")
		return
	}
	if !enqueue {
		c.JSON(200, gin.H{"status": "pending"})
		return
	}
	if _, err = s.enqueue(c.Request.Context(), id, "lifecycle.uninstall", "lifecycle.uninstall:"+strconv.Itoa(id)+":"+req.Mode, time.Now().Unix(), map[string]any{"mode": req.Mode}); err != nil {
		failure(c, 503, "uninstall_request_failed")
		return
	}
	s.audit(c, "gate.uninstall", strconv.Itoa(id), req.Mode)
	c.JSON(200, gin.H{"status": "pending"})
}
