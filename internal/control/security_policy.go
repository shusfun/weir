package control

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"
)

// A new installation can have no saved policy. A failed query or malformed
// policy must not silently disable MFA or email-verification requirements.
func (s *Server) securityPolicy(c *gin.Context) (SecuritySettings, bool) {
	var policy SecuritySettings
	var data []byte
	err := s.DB.QueryRowContext(c.Request.Context(), `SELECT data FROM migate.resources WHERE kind='settings' AND key='security'`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return policy, true
	}
	if err == nil {
		err = json.Unmarshal(data, &policy)
	}
	if err != nil || string(data) == "null" {
		failure(c, 503, "security_policy_unavailable")
		return SecuritySettings{}, false
	}
	return policy, true
}
