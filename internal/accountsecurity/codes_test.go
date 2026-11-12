package accountsecurity

import (
	"strings"
	"testing"
	"time"
)

func TestCodePolicyRejectsUnboundedOverrides(t *testing.T) {
	for _, mutate := range []func(*CodePolicy){
		func(p *CodePolicy) { p.Attempts = 0 }, func(p *CodePolicy) { p.Attempts = 11 },
		func(p *CodePolicy) { p.Lifetime = time.Hour }, func(p *CodePolicy) { p.Lifetime = time.Second },
		func(p *CodePolicy) { p.Cooldown[0] = time.Second }, func(p *CodePolicy) { p.Cooldown[1] = 30 * time.Second }, func(p *CodePolicy) { p.Cooldown[2] = 2 * time.Hour },
	} {
		p := DefaultCodePolicy()
		mutate(&p)
		if p.Validate() == nil {
			t.Fatal("unbounded policy accepted", p)
		}
	}
}

func TestCodeDigestBindsPurposeEmailAndGeneration(t *testing.T) {
	s := Codes{Key: []byte(strings.Repeat("a", 32))}
	register, _ := identity("register", " A@Example.test ")
	alias, _ := identity("verify", "a@example.test")
	reset, _ := identity("reset", "a@example.test")
	if register != alias {
		t.Fatal("registration alias or mailbox case changed identity")
	}
	a := s.digest(register, "generation-a", "123456")
	for _, b := range []string{s.digest(reset, "generation-a", "123456"), s.digest(register, "generation-b", "123456"), s.digest(register, "generation-a", "654321"), (Codes{Key: []byte(strings.Repeat("b", 32))}).digest(register, "generation-a", "123456")} {
		if a == b {
			t.Fatal("digest is not bound to its context")
		}
	}
}
