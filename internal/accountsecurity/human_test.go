package accountsecurity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go/v2"
)

type humanMemory struct {
	sync.Mutex
	values map[string]string
	fail   bool
}

func (m *humanMemory) Get(_ context.Context, k string) (string, bool, error) {
	m.Lock()
	defer m.Unlock()
	if m.fail {
		return "", false, errors.New("store offline")
	}
	v, ok := m.values[k]
	return v, ok, nil
}
func (m *humanMemory) Set(_ context.Context, k, v string, _ time.Duration) error {
	m.Lock()
	defer m.Unlock()
	if m.fail {
		return errors.New("store offline")
	}
	m.values[k] = v
	return nil
}
func (m *humanMemory) GetDel(_ context.Context, k string) (string, bool, error) {
	m.Lock()
	defer m.Unlock()
	if m.fail {
		return "", false, errors.New("store offline")
	}
	v, ok := m.values[k]
	delete(m.values, k)
	return v, ok, nil
}
func (m *humanMemory) Incr(context.Context, string, time.Duration) (int64, error) { panic("unused") }

func solveHuman(t *testing.T, ch altcha.Challenge) string {
	t.Helper()
	solution, err := altcha.SolveChallenge(altcha.SolveChallengeOptions{Challenge: ch, DeriveKey: altcha.DeriveKeyPBKDF2(), Timeout: 5 * time.Second})
	if err != nil || solution == nil {
		t.Fatalf("solve: %v", err)
	}
	b, _ := json.Marshal(altcha.Payload{Challenge: ch, Solution: *solution})
	return base64.StdEncoding.EncodeToString(b)
}

func TestHumanProofBindingExpiryAndSingleConsumption(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := &humanMemory{values: map[string]string{}}
	h := Human{Store: store, Key: []byte(strings.Repeat("k", 32)), Policy: DefaultHumanPolicy(), Now: func() time.Time { return now }}
	ch, err := h.Issue(ctx, "register", "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	proof := solveHuman(t, ch)
	if err := h.Verify(ctx, "register", "203.0.113.1", proof, ""); !errors.Is(err, ErrHumanInvalid) {
		t.Fatal("too-fast proof accepted", err)
	}
	now = now.Add(3 * time.Second)
	for _, args := range [][3]string{{"email", "203.0.113.1", ""}, {"register", "203.0.113.2", ""}, {"register", "203.0.113.1", "filled"}} {
		if err := h.Verify(ctx, args[0], args[1], proof, args[2]); !errors.Is(err, ErrHumanInvalid) {
			t.Fatal("binding bypass", args, err)
		}
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if h.Verify(ctx, "register", "203.0.113.1", proof, "") == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal("concurrent consumers", accepted.Load())
	}
	ch, err = h.Issue(ctx, "reset", "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	proof = solveHuman(t, ch)
	now = now.Add(301 * time.Second)
	if !errors.Is(h.Verify(ctx, "reset", "203.0.113.1", proof, ""), ErrHumanInvalid) {
		t.Fatal("expired proof accepted")
	}
}

func TestHumanRejectsTamperingAndStorageFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := &humanMemory{values: map[string]string{}}
	h := Human{Store: store, Key: []byte(strings.Repeat("k", 32)), Policy: DefaultHumanPolicy(), Now: func() time.Time { return now }}
	ch, err := h.Issue(ctx, "login", "ip")
	if err != nil {
		t.Fatal(err)
	}
	proof := solveHuman(t, ch)
	now = now.Add(3 * time.Second)
	for _, input := range []string{"bad", strings.Repeat("a", 8193), base64.StdEncoding.EncodeToString([]byte(`{"challenge":{"signature":"` + ch.Signature + `"},"solution":{"derivedKey":"` + strings.Repeat("0", 64) + `","counter":1}}`))} {
		if !errors.Is(h.Verify(ctx, "login", "ip", input, ""), ErrHumanInvalid) {
			t.Fatal("invalid proof accepted")
		}
	}
	if !errors.Is(h.Verify(ctx, "login", "ip", "", ""), ErrHumanRequired) {
		t.Fatal("missing proof accepted")
	}
	store.fail = true
	if h.Verify(ctx, "login", "ip", proof, "") == nil {
		t.Fatal("offline store accepted proof")
	}
	if _, err := h.Issue(ctx, "email", "ip"); err == nil {
		t.Fatal("offline store issued challenge")
	}
}

func TestHumanPolicyBounds(t *testing.T) {
	for _, mutate := range []func(*HumanPolicy){func(p *HumanPolicy) { p.Cost = 1000000 }, func(p *HumanPolicy) { p.Counter = 0 }, func(p *HumanPolicy) { p.MinSeconds = 0 }, func(p *HumanPolicy) { p.LifetimeSeconds = 3600 }, func(p *HumanPolicy) { p.LoginFailures = 0 }} {
		p := DefaultHumanPolicy()
		mutate(&p)
		if p.Validate() == nil {
			t.Fatal("unbounded policy accepted", p)
		}
	}
}

func TestExternalHumanCannotBypassLocalLoginOrBindings(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	h := Human{Store: &humanMemory{values: map[string]string{}}, Key: []byte(strings.Repeat("k", 32)), Policy: DefaultHumanPolicy(), Now: func() time.Time { return now }}
	h.Policy.Provider = "turnstile"
	ch, err := h.Issue(ctx, "register", "ip")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"challenge": map[string]any{"signature": ch.Signature}})
	proof := base64.StdEncoding.EncodeToString(b)
	now = now.Add(3 * time.Second)
	called := 0
	check := func(context.Context) error { called++; return nil }
	if h.VerifyExternal(ctx, "register", "different-ip", proof, "", check) == nil || called != 0 {
		t.Fatal("bad binding reached provider")
	}
	if h.Verify(ctx, "register", "ip", proof, "") == nil {
		t.Fatal("external proof bypassed provider")
	}
	if h.VerifyExternal(ctx, "register", "ip", proof, "", nil) == nil {
		t.Fatal("missing provider accepted")
	}
	if h.VerifyExternal(ctx, "register", "ip", proof, "", func(context.Context) error { return errors.New("provider offline") }) == nil {
		t.Fatal("provider failure accepted")
	}
	if err := h.VerifyExternal(ctx, "register", "ip", proof, "", check); err != nil || called != 1 {
		t.Fatal("external proof rejected", err)
	}
	ch, err = h.Issue(ctx, "login", "ip")
	if err != nil {
		t.Fatal(err)
	}
	proof = solveHuman(t, ch)
	now = now.Add(3 * time.Second)
	if h.VerifyExternal(ctx, "login", "ip", proof, "", check) == nil {
		t.Fatal("external provider used for login")
	}
	if err := h.Verify(ctx, "login", "ip", proof, ""); err != nil {
		t.Fatal("local login rejected", err)
	}
}
