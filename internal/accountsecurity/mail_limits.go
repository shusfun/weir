package accountsecurity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"time"
)

type Counter interface {
	Incr(context.Context, string, time.Duration) (int64, error)
}

type LimitError struct {
	Scope string
	Until time.Time
	First bool
}

func (e *LimitError) Error() string { return "email_rate_limited:" + e.Scope }

// ReserveMail bounds issuance attempts, including requests for nonexistent
// accounts. All centers share atomic counters; failures never grant a permit.
// Windows are UTC hours, with no plaintext mailbox or IP in storage keys.
func ReserveMail(ctx context.Context, store Counter, limits MailLimits, email, clientIP string, now time.Time) error {
	if store == nil {
		return errors.New("email limit storage unavailable")
	}
	p := DefaultPolicy()
	p.MailLimits = limits
	if err := p.Validate(); err != nil {
		return err
	}
	ip, err := netip.ParseAddr(clientIP)
	if err != nil || ip.Zone() != "" {
		return errors.New("invalid email request address")
	}
	ip = ip.Unmap()
	bits := 64
	if ip.Is4() {
		bits = 24
	}
	network := netip.PrefixFrom(ip, bits).Masked().String()
	_, domain, ok := strings.Cut(email, "@")
	if !ok || domain == "" {
		return errors.New("invalid email request identity")
	}
	now = now.UTC()
	window := now.Truncate(time.Hour)
	until := window.Add(time.Hour)
	for _, v := range []struct {
		scope, identity string
		max             int64
	}{
		{"email", strings.ToLower(email), limits.Email}, {"ip", ip.String(), limits.IP},
		{"network", network, limits.Network}, {"domain", strings.ToLower(domain), limits.Domain}, {"site", "all", limits.Site},
	} {
		h := sha256.Sum256([]byte(v.identity))
		key := "email-send-limit:" + v.scope + ":" + window.Format("2006010215") + ":" + hex.EncodeToString(h[:])
		n, err := store.Incr(ctx, key, until.Sub(now))
		if err != nil {
			return err
		}
		if n > v.max {
			return &LimitError{Scope: v.scope, Until: until, First: n == v.max+1}
		}
	}
	return nil
}
