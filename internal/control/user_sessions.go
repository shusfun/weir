package control

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent/user"
)

func (s *Server) sessionUser(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		failure(c, 400, "invalid_user")
		return 0, false
	}
	exists, err := s.Store.User.Query().Where(user.IDEQ(id)).Exist(c.Request.Context())
	if err != nil {
		failure(c, 503, "user_directory_unavailable")
		return 0, false
	}
	if !exists {
		failure(c, 404, "user_not_found")
		return 0, false
	}
	return id, true
}
func (s *Server) adminUserSessions(c *gin.Context) {
	id, ok := s.sessionUser(c)
	if !ok {
		return
	}
	s.listSessions(c, id)
}

func (s *Server) adminRevokeUserSession(c *gin.Context) {
	id, ok := s.sessionUser(c)
	if !ok {
		return
	}
	token := c.Param("session")
	if len(token) != 64 || strings.Trim(token, "0123456789abcdef") != "" {
		failure(c, 400, "invalid_session")
		return
	}
	// Check ownership and revoke atomically; never accept a Redis key from a client.
	script := revokeOwnedSession
	n, err := script.Run(c.Request.Context(), s.Redis, []string{"migate:session:" + token}, id).Int()
	if err != nil {
		failure(c, 503, "session_unavailable")
		return
	}
	if n != 1 {
		failure(c, 404, "session_not_found")
		return
	}
	s.audit(c, "user.session.revoke", fmt.Sprint(id), "ok")
	c.JSON(200, gin.H{"ok": true})
}
