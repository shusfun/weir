package center

import (
	"errors"
	"github.com/shusfun/weir/internal/accountsecurity"
	"testing"
	"time"
)

func TestAccessGuardSubjectsCooldownAndStoreFailure(t *testing.T) {
	s, _ := newInstalledServer(t)
	now := time.Now().UTC().Truncate(time.Second)
	ctx := t.Context()
	g := accountsecurity.AccessGuard{Store: s.Store.KV, Policy: accountsecurity.DefaultAccessPolicy(), Now: func() time.Time { return now }}
	subjects := []string{"user:hashed-user", "ip:hashed-ip"}
	for stage, seconds := range []int{60, 300, 900} {
		for i := 0; i < 5; i++ {
			if err := g.Failure(ctx, subjects); err != nil {
				t.Fatal(err)
			}
		}
		for _, other := range [][]string{subjects, {subjects[0], "ip:another"}, {"user:another", subjects[1]}} {
			var cd *accountsecurity.CooldownError
			if err := g.Check(ctx, other); !errors.As(err, &cd) || cd.Until.Sub(now).Round(time.Second) != time.Duration(seconds)*time.Second {
				t.Fatalf("stage %d: %v", stage, err)
			}
		}
		if err := g.Check(ctx, []string{"user:unrelated", "ip:unrelated"}); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Duration(seconds+1) * time.Second)
		if err := g.Check(ctx, subjects); err != nil {
			t.Fatal("cooldown did not end", err)
		}
	}
	g.Store = codeCounterFailure{CodeStore: s.Store.KV}
	if err := g.Failure(ctx, []string{"user:new", "ip:new"}); err == nil {
		t.Fatal("counter failure allowed code attempts")
	}
	for _, f := range []func(*accountsecurity.AccessPolicy){func(p *accountsecurity.AccessPolicy) { p.AccessAttempts = 0 }, func(p *accountsecurity.AccessPolicy) { p.AccessWindowSeconds = 1 }, func(p *accountsecurity.AccessPolicy) { p.AccessCooldownSeconds = [3]int{300, 60, 30} }} {
		p := accountsecurity.DefaultAccessPolicy()
		f(&p)
		if p.Validate() == nil {
			t.Fatal("invalid access policy accepted")
		}
	}
}
