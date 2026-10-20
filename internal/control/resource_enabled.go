package control

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
)

// Omitted state preserves historical clients' updates. Explicit changes require
// an Agent that understands disabled resources; never silently re-enable one.
func (s *Server) prepareResourceEnabled(c *gin.Context, deviceID int, operation string, payload map[string]any) bool {
	if operation != "outbound.upsert" && operation != "routing.upsert" && operation != "inbound.upsert" {
		return true
	}
	if value, supplied := payload["enabled"]; supplied {
		if _, ok := value.(bool); !ok {
			failure(c, 400, "invalid_resource_enabled")
			return false
		}
		d, err := s.Store.Device.Get(c.Request.Context(), deviceID)
		if err != nil {
			failure(c, 503, "query_failed")
			return false
		}
		capable := false
		if caps, ok := d.Metadata["capabilities"].([]any); ok {
			for _, cap := range caps {
				required := "resource.enabled.v1"
				if operation == "inbound.upsert" {
					required = "inbound.enabled.v1"
				}
				capable = capable || cap == required
			}
		}
		if !capable {
			failure(c, 400, "agent_capability_required")
			return false
		}
		return true
	}
	kind := "device-outbound"
	if operation == "inbound.upsert" {
		kind = "device-inbound"
	}
	if operation == "routing.upsert" {
		kind = "device-routing"
	}
	old, err := s.Store.Resource.Query().Where(resource.KindEQ(kind), resource.KeyEQ(fmt.Sprintf("%d:%d", deviceID, int(number(payload["id"]))))).Only(c.Request.Context())
	if err != nil && !ent.IsNotFound(err) {
		failure(c, 503, "query_failed")
		return false
	}
	// Keep omission for old Agents, whose compatibility default is enabled.
	if old != nil && old.Data["enabled"] == false {
		payload["enabled"] = false
	}
	return true
}
