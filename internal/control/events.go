package control

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"time"
)

func (s *Server) securityEvent(c *gin.Context, kind, scope, severity string) {
	key := digest(fmt.Sprintf("%s:%s:%s:%d", kind, scope, c.ClientIP(), time.Now().Unix()/600))
	_, _ = s.Store.Resource.Create().SetKind("security-event").SetKey(key).SetData(map[string]any{"rule": kind, "scope": scope, "severity": severity, "status": "review_required", "category": "unknown", "source_hash": digest(c.ClientIP()), "observed_at": time.Now().UTC().Format(time.RFC3339), "automatic_action": "none"}).Save(c.Request.Context())
}
func (s *Server) failedLoginEvent(c *gin.Context) {
	script := redis.NewScript(`local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('EXPIRE',KEYS[1],600) end;return n`)
	n, e := script.Run(c.Request.Context(), s.Redis, []string{"migate:failed-login:" + digest(c.ClientIP())}).Int()
	if e == nil && n == 5 {
		s.securityEvent(c, "repeated-login-failure", "authentication", "warning")
	}
}
