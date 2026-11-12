package accountsecurity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

type CodeStore interface {
	Get(context.Context, string) (string, bool, error)
	Set(context.Context, string, string, time.Duration) error
	GetDel(context.Context, string) (string, bool, error)
	Incr(context.Context, string, time.Duration) (int64, error)
}

type Locker interface {
	Do(context.Context, string, func(context.Context) error) error
}

type CodePolicy struct {
	Lifetime time.Duration
	Attempts int
	Cooldown [3]time.Duration
}

func DefaultCodePolicy() CodePolicy {
	return CodePolicy{Lifetime: 10 * time.Minute, Attempts: 5, Cooldown: [3]time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}}
}

func (p CodePolicy) Validate() error {
	if p.Lifetime < time.Minute || p.Lifetime > 30*time.Minute || p.Attempts < 1 || p.Attempts > 10 {
		return errors.New("invalid email code policy")
	}
	for i, d := range p.Cooldown {
		if d < 30*time.Second || d > time.Hour || i > 0 && d < p.Cooldown[i-1] {
			return errors.New("invalid email cooldown policy")
		}
	}
	return nil
}

type CooldownError struct{ Until time.Time }

func (e *CooldownError) Error() string { return "email_code_cooldown" }

// Codes uses host storage and locks in both single-instance and shared-storage
// deployments. The digest key must be stable across centers and kept outside
// the code store; a dump of six-digit digests cannot be searched offline.
type Codes struct {
	Store  CodeStore
	Locks  Locker
	Key    []byte
	Policy CodePolicy
	Now    func() time.Time
}

type codeRecord struct {
	ID          string    `json:"id"`
	Digest      string    `json:"digest"`
	ExpiresAt   time.Time `json:"expires_at"`
	MaxAttempts int       `json:"max_attempts"`
}

func (s Codes) ready() error {
	if s.Store == nil || s.Locks == nil || len(s.Key) != 32 {
		return errors.New("email code service unavailable")
	}
	return s.Policy.Validate()
}

func (s Codes) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func identity(purpose, email string) (string, error) {
	if purpose == "verify" {
		purpose = "register"
	}
	if purpose != "register" && purpose != "reset" || email == "" || len(email) > 254 {
		return "", errors.New("invalid email code identity")
	}
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return "email-code:" + purpose + ":" + hex.EncodeToString(h[:]), nil
}

func lockShard(key string) string {
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("email-code:%02x", h[0]&63)
}

func (s Codes) digest(key, id, code string) string {
	h := hmac.New(sha256.New, s.Key)
	_, _ = h.Write([]byte("weir/email-code/v1\x00" + key + "\x00" + id + "\x00" + code))
	return hex.EncodeToString(h.Sum(nil))
}

// Issue reserves the same cooldown for every purpose and account-existence
// outcome. Delivery remains the host's responsibility; no plaintext is stored.
func (s Codes) Issue(ctx context.Context, purpose, email string) (code string, err error) {
	if err = s.ready(); err != nil {
		return "", err
	}
	key, err := identity(purpose, email)
	if err != nil {
		return "", err
	}
	_, emailDigest, _ := strings.Cut(strings.TrimPrefix(key, "email-code:"), ":")
	shared := "email-code-send:" + emailDigest
	err = s.Locks.Do(ctx, lockShard(shared), func(ctx context.Context) error {
		now := s.now()
		raw, ok, err := s.Store.Get(ctx, shared+":cooldown")
		if err != nil {
			return err
		}
		if ok {
			until, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				return errors.New("invalid email cooldown state")
			}
			if now.Before(until) {
				return &CooldownError{Until: until}
			}
		}
		n, err := s.Store.Incr(ctx, shared+":count", 24*time.Hour)
		if err != nil {
			return err
		}
		until := now.Add(s.Policy.Cooldown[min(max(n-1, 0), 2)])
		if err = s.Store.Set(ctx, shared+":cooldown", until.Format(time.RFC3339Nano), until.Sub(now)); err != nil {
			return err
		}
		value, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			return err
		}
		code = fmt.Sprintf("%06d", value.Int64())
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return err
		}
		id := hex.EncodeToString(nonce[:])
		record := codeRecord{ID: id, Digest: s.digest(key, id, code), ExpiresAt: now.Add(s.Policy.Lifetime), MaxAttempts: s.Policy.Attempts}
		body, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return s.Store.Set(ctx, key, string(body), s.Policy.Lifetime)
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// Verify never rewrites the challenge or extends its expiry. An atomic counter
// belongs to this generation, and GetDel permits just one successful consumer.
func (s Codes) Verify(ctx context.Context, purpose, email, code string) (bool, error) {
	if err := s.ready(); err != nil {
		return false, err
	}
	key, err := identity(purpose, email)
	if err != nil {
		return false, err
	}
	raw, ok, err := s.Store.Get(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	var r codeRecord
	if json.Unmarshal([]byte(raw), &r) != nil || len(r.ID) != 32 || len(r.Digest) != 64 || r.MaxAttempts < 1 || r.MaxAttempts > 10 {
		return false, nil
	}
	remaining := r.ExpiresAt.Sub(s.now())
	if remaining <= 0 || remaining > 30*time.Minute {
		return false, nil
	}
	attempts, err := s.Store.Incr(ctx, key+":attempts:"+r.ID, remaining)
	if err != nil || attempts > int64(r.MaxAttempts) {
		return false, err
	}
	if subtle.ConstantTimeCompare([]byte(r.Digest), []byte(s.digest(key, r.ID, strings.TrimSpace(code)))) != 1 {
		return false, nil
	}
	matched := false
	err = s.Locks.Do(ctx, lockShard("email-code-send:"+strings.Split(key, ":")[2]), func(ctx context.Context) error {
		current, ok, err := s.Store.Get(ctx, key)
		if err != nil || !ok || current != raw || !s.now().Before(r.ExpiresAt) {
			return err
		}
		_, matched, err = s.Store.GetDel(ctx, key)
		return err
	})
	return matched, err
}
