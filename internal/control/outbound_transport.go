package control

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/internal/db"
)

func (s *Server) prepareOutboundTransport(c *gin.Context, deviceID int, payload map[string]any) bool {
	options, supplied := payload["transport_options"]
	protocol := text(payload["protocol"])
	advanced := protocol == "hysteria2" || protocol == "tuic" || protocol == "anytls" || protocol == "shadowtls"
	if !supplied && protocol != "vmess" && !advanced {
		return true
	}
	d, err := s.Store.Device.Get(c.Request.Context(), deviceID)
	if err != nil {
		failure(c, 503, "query_failed")
		return false
	}
	capable := false
	trustCapable := false
	advancedCapable := false
	if caps, ok := d.Metadata["capabilities"].([]any); ok {
		for _, cap := range caps {
			capable = capable || cap == "outbound.transport.v1"
			trustCapable = trustCapable || cap == "outbound.trust.v1"
			advancedCapable = advancedCapable || cap == "outbound.pool.v1"
		}
	}
	if !capable {
		failure(c, 400, "agent_capability_required")
		return false
	}
	if protocol := text(payload["protocol"]); (protocol == "hysteria2" || protocol == "tuic" || protocol == "anytls" || protocol == "shadowtls") && !advancedCapable {
		failure(c, 400, "agent_capability_required")
		return false
	}
	if supplied {
		if fields, ok := options.(map[string]any); ok && text(fields["trusted_certificate"]) != "" && !trustCapable {
			failure(c, 400, "agent_capability_required")
			return false
		}
		if _, ok := options.(map[string]any); !ok {
			failure(c, 400, "invalid_outbound_transport")
			return false
		}
		raw, err := json.Marshal(options)
		if err != nil {
			failure(c, 400, "invalid_outbound_transport")
			return false
		}
		payload["settings_json"] = string(raw)
	}
	raw, err := json.Marshal(payload)
	var outbound db.Outbound
	if err != nil || json.Unmarshal(raw, &outbound) != nil || db.ValidateOutboundProfile(outbound) != nil {
		failure(c, 400, "invalid_outbound_profile")
		return false
	}
	settings, err := db.ManagedOutboundSettings(outbound)
	if err != nil {
		failure(c, 400, "invalid_outbound_transport")
		return false
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		failure(c, 400, "invalid_outbound_transport")
		return false
	}
	payload["settings_json"] = string(encoded)
	delete(payload, "transport_options")
	delete(payload, "transport_options_unavailable")
	return true
}

func publicOutboundTransport(data map[string]any) (any, bool) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, false
	}
	var outbound db.Outbound
	if json.Unmarshal(raw, &outbound) != nil {
		return nil, false
	}
	settings, err := db.ManagedOutboundSettings(outbound)
	settings.ObfsPassword = ""
	return settings, err == nil
}
