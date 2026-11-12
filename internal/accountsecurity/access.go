package accountsecurity

import (
	"context"
	"errors"
	"strconv"
	"time"
)

type AccessPolicy struct {
	InviteRequired        bool   `json:"invite_required"`
	RedeemEnabled         bool   `json:"redeem_enabled"`
	AccessAttempts        int    `json:"access_code_attempts"`
	AccessWindowSeconds   int    `json:"access_code_window_seconds"`
	AccessCooldownSeconds [3]int `json:"access_code_cooldown_seconds"`
}

func DefaultAccessPolicy() AccessPolicy {
	return AccessPolicy{AccessAttempts: 5, AccessWindowSeconds: 3600, AccessCooldownSeconds: [3]int{60, 300, 900}}
}
func (p AccessPolicy) Validate() error {
	if p.AccessAttempts < 1 || p.AccessAttempts > 20 || p.AccessWindowSeconds < 900 || p.AccessWindowSeconds > 86400 {
		return errors.New("invalid access code attempt bounds")
	}
	for i, n := range p.AccessCooldownSeconds {
		if n < 30 || n > 3600 || (i > 0 && n < p.AccessCooldownSeconds[i-1]) {
			return errors.New("invalid access code cooldown bounds")
		}
	}
	return nil
}

// Callers serialize Check / validation / Failure with their existing locker.
// Subjects are hashed by the host; neither raw codes nor identities are keys.
type AccessGuard struct {
	Store  CodeStore
	Policy AccessPolicy
	Now    func() time.Time
}

func (g AccessGuard) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}
func (g AccessGuard) Check(ctx context.Context, subjects []string) error {
	if err := g.Policy.Validate(); err != nil {
		return err
	}
	var until time.Time
	for _, key := range subjects {
		v, ok, err := g.Store.Get(ctx, "access-lock:"+key)
		if err != nil {
			return err
		}
		if ok {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return errors.New("invalid access code lock state")
			}
			t := time.Unix(n, 0)
			if t.After(until) {
				until = t
			}
		}
	}
	if until.After(g.now()) {
		return &CooldownError{Until: until}
	}
	return nil
}
func (g AccessGuard) Failure(ctx context.Context, subjects []string) error {
	if err := g.Policy.Validate(); err != nil {
		return err
	}
	for _, key := range subjects {
		n, err := g.Store.Incr(ctx, "access-failed:"+key, time.Duration(g.Policy.AccessWindowSeconds)*time.Second)
		if err != nil {
			return err
		}
		if n >= int64(g.Policy.AccessAttempts) {
			stage := min(2, int(n/int64(g.Policy.AccessAttempts))-1)
			ttl := time.Duration(g.Policy.AccessCooldownSeconds[stage]) * time.Second
			if err := g.Store.Set(ctx, "access-lock:"+key, strconv.FormatInt(g.now().Add(ttl).Unix(), 10), ttl); err != nil {
				return err
			}
		}
	}
	return nil
}
