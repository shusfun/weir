package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/accountsecurity"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/link"
	"github.com/shusfun/weir/internal/store/ent/user"
)

func TestEmailCodesDigestExpiryAndAtomicConsumption(t *testing.T) {
	s, _ := newInstalledServer(t)
	codes := s.emailCodes()
	now := time.Now().UTC()
	codes.Now = func() time.Time { return now }
	ctx := t.Context()
	const email = "code-check@example.test"
	code, err := codes.Issue(ctx, "verify", email)
	if err != nil {
		t.Fatal(err)
	}
	key := "email-code:register:" + digest(email)
	raw, ok, err := s.Store.KV.Get(ctx, key)
	var record map[string]any
	if err != nil || !ok || json.Unmarshal([]byte(raw), &record) != nil || record["code"] != nil || record["digest"] == code || len(record["digest"].(string)) != 64 {
		t.Fatal("code was not stored as a keyed digest", err)
	}
	now = now.Add(9*time.Minute + 59*time.Second)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 0; i < 4; i++ {
		if valid, err := codes.Verify(ctx, "register", email, wrong); valid || err != nil {
			t.Fatal("wrong code accepted", err)
		}
	}
	after, _, _ := s.Store.KV.Get(ctx, key)
	if after != raw {
		t.Fatal("wrong guesses rewrote code or extended expiry")
	}
	if valid, err := codes.Verify(ctx, "register", email, code); !valid || err != nil {
		t.Fatal("fifth attempt rejected", err)
	}
	if valid, _ := codes.Verify(ctx, "register", email, code); valid {
		t.Fatal("code replay accepted")
	}

	code, err = codes.Issue(ctx, "reset", "expired@example.test")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10*time.Minute + time.Second)
	if valid, _ := codes.Verify(ctx, "reset", "expired@example.test", code); valid {
		t.Fatal("expired code accepted")
	}

	code, err = codes.Issue(ctx, "register", "concurrent@example.test")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var accepted atomic.Int32
	var failures atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			valid, err := codes.Verify(ctx, "register", "concurrent@example.test", code)
			if err != nil {
				failures.Add(1)
			}
			if valid {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 || failures.Load() != 0 {
		t.Fatal("concurrent code consumption", accepted.Load(), failures.Load())
	}
	code, err = codes.Issue(ctx, "register", "exhausted@example.test")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := codes.Verify(ctx, "register", "exhausted@example.test", "invalid")
			if err != nil {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if valid, err := codes.Verify(ctx, "register", "exhausted@example.test", code); valid || err != nil || failures.Load() != 0 {
		t.Fatal("attempt limit bypassed", err)
	}
}

func TestEmailCodesCooldownAndStorageFailure(t *testing.T) {
	s, _ := newInstalledServer(t)
	codes := s.emailCodes()
	now := time.Now().UTC()
	codes.Now = func() time.Time { return now }
	ctx := t.Context()
	for i, want := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 15 * time.Minute} {
		if _, err := codes.Issue(ctx, "register", "cooldown@example.test"); err != nil {
			t.Fatal(err)
		}
		_, err := codes.Issue(ctx, "reset", "cooldown@example.test")
		var cooldown *accountsecurity.CooldownError
		if !errors.As(err, &cooldown) || cooldown.Until.Sub(now) != want {
			t.Fatalf("stage %d: %v", i, err)
		}
		now = now.Add(want + time.Second)
	}
	code, err := codes.Issue(ctx, "register", "storage@example.test")
	if err != nil {
		t.Fatal(err)
	}
	codes.Store = codeCounterFailure{CodeStore: codes.Store}
	if valid, err := codes.Verify(ctx, "register", "storage@example.test", code); valid || err == nil {
		t.Fatal("storage failure did not reject verification")
	}
	if _, err := codes.Issue(ctx, "register", "new@example.test"); err == nil {
		t.Fatal("storage failure did not reject issuance")
	}
}

type codeCounterFailure struct{ accountsecurity.CodeStore }

func (codeCounterFailure) Incr(context.Context, string, time.Duration) (int64, error) {
	return 0, errors.New("injected counter failure")
}

func TestRegistrationRollsBackLinkAndAuditFailures(t *testing.T) {
	for _, stage := range []string{"link", "audit"} {
		t.Run(stage, func(t *testing.T) {
			s, admin := newInstalledServer(t)
			mustJSON(t, admin, "PUT", "/api/v1/admin/settings/security", map[string]any{"registration_enabled": true}, 200)
			broken := true
			hook := func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					if broken {
						return nil, fmt.Errorf("injected %s failure", stage)
					}
					return next.Mutate(ctx, m)
				})
			}
			if stage == "link" {
				s.Store.Ent.Link.Use(hook)
			} else {
				s.Store.Ent.Audit.Use(hook)
			}
			anon := &testClient{t: t, router: s.Router()}
			email := stage + "-rollback@example.test"
			w := anon.do("POST", "/api/v1/auth/register", map[string]any{"email": email, "password": "Passw0rd"}, nil)
			if w.Code < 500 {
				t.Fatalf("failure status %d %s", w.Code, w.Body)
			}
			if s.Store.Ent.User.Query().Where(user.EmailEQ(email)).ExistX(t.Context()) || s.Store.Ent.Audit.Query().Where(audit.ActionEQ("auth.register")).ExistX(t.Context()) {
				t.Fatal("registration partially committed")
			}
			broken = false
			mustJSON(t, anon, "POST", "/api/v1/auth/register", map[string]any{"email": email, "password": "Passw0rd"}, 201)
			u := s.Store.Ent.User.Query().Where(user.EmailEQ(email)).OnlyX(t.Context())
			if u.Unlimited || u.Quota != 0 {
				t.Fatal("new registration received unlimited traffic")
			}
			if n := s.Store.Ent.Link.Query().Where(link.UserIDEQ(u.ID)).CountX(t.Context()); n != 1 {
				t.Fatal("default link missing", n)
			}
		})
	}
}

func TestEmailEndpointCooldownDoesNotExposeAccountExistence(t *testing.T) {
	s, admin := newInstalledServer(t)
	s.Mail = &fakeMail{}
	s.Store.Ent.User.Create().SetEmail("known@example.test").SetPasswordHash("unused").SaveX(t.Context())
	mustJSON(t, admin, "PUT", "/api/v1/admin/settings/smtp", map[string]any{"host": "smtp.example.test", "port": 465, "from": "noreply@example.test", "use_tls": true}, 200)
	anon := &testClient{t: t, router: s.Router()}
	for _, email := range []string{"known@example.test", "unknown@example.test"} {
		mustJSON(t, anon, "POST", "/api/v1/auth/email", map[string]any{"email": email, "purpose": "reset"}, 200)
		w := anon.do("POST", "/api/v1/auth/email", map[string]any{"email": email, "purpose": "reset"}, nil)
		if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "email_code_cooldown") {
			t.Fatalf("cooldown mismatch: %d %s", w.Code, w.Body)
		}
	}
}
