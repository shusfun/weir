package center

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/accountsecurity"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/user"
)

func (s *Server) registrationEmail(c *gin.Context, email string, p accountsecurity.EmailPolicy) bool {
	ctx := c.Request.Context()
	started := time.Now()
	err := p.CheckAddress(email)
	if err == nil {
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = accountsecurity.CheckMX(check, s.EmailDNS, email)
		cancel()
	}
	outcome, code := "succeeded", ""
	if err != nil {
		outcome, code = "rejected", err.Error()
	}
	slog.InfoContext(ctx, "registration email policy", "trace", taskTrace(ctx), "outcome", outcome, "code", code, "ms", time.Since(started).Milliseconds())
	if err == nil {
		return true
	}
	if errors.Is(err, accountsecurity.ErrMXUnavailable) {
		failure(c, 503, err.Error())
		return false
	}
	if errors.Is(err, accountsecurity.ErrDisposableEmail) {
		s.securityEvent(c, "disposable-email", "registration", "warning")
	}
	failure(c, 400, err.Error())
	return false
}

// Re-evaluate old addresses under the current alias policy. The saved identity
// is historical, and cannot be trusted after an alias rule changes. This also
// covers old backups and accounts created before normalized_email existed.
func registrationIdentityExists(ctx context.Context, client *ent.Client, p accountsecurity.EmailPolicy, identity string) (bool, error) {
	last := 0
	for {
		rows, err := client.User.Query().Where(user.IDGT(last)).Select(user.FieldID, user.FieldEmail).Order(ent.Asc(user.FieldID)).Limit(250).All(ctx)
		if err != nil {
			return false, err
		}
		for _, u := range rows {
			normalized, err := p.Identity(u.Email)
			if err == nil && normalized == identity {
				return true, nil
			}
			last = u.ID
		}
		if len(rows) < 250 {
			return false, nil
		}
	}
}
