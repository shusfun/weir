package accountsecurity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go/v2"
)

var ErrHumanInvalid = errors.New("human_verification_invalid")
var ErrHumanRequired = errors.New("human_verification_required")

type HumanPolicy struct {
	Provider        string `json:"human_provider"`
	ChallengeIP     int64  `json:"human_challenge_ip_ten_minutes"`
	ChallengeSite   int64  `json:"human_challenge_site_minute"`
	Cost            int    `json:"human_pow_cost"`
	Counter         int    `json:"human_pow_counter"`
	LifetimeSeconds int    `json:"human_lifetime_seconds"`
	MinSeconds      int    `json:"human_min_seconds"`
	LoginFailures   int    `json:"human_login_failures"`
}

func DefaultHumanPolicy() HumanPolicy {
	return HumanPolicy{Provider: "altcha", ChallengeIP: 60, ChallengeSite: 1000, Cost: 2000, Counter: 256, LifetimeSeconds: 300, MinSeconds: 2, LoginFailures: 5}
}

func (p HumanPolicy) Validate() error {
	if p.Provider != "altcha" && p.Provider != "turnstile" {
		return errors.New("unknown human verification provider")
	}
	if p.ChallengeIP < 1 || p.ChallengeIP > 600 || p.ChallengeSite < 1 || p.ChallengeSite > 10000 || p.Cost < 1000 || p.Cost > 100000 || p.Counter < 16 || p.Counter > 1024 ||
		p.LifetimeSeconds < 60 || p.LifetimeSeconds > 600 || p.MinSeconds < 1 || p.MinSeconds > 10 || p.LoginFailures < 1 || p.LoginFailures > 10 {
		return errors.New("human verification policy out of bounds")
	}
	return nil
}

// Human keeps the issued parameters server-side. Verification never derives a
// key from client-selected KDF parameters, even if a payload is malformed.
type Human struct {
	Store  CodeStore
	Key    []byte
	Policy HumanPolicy
	Now    func() time.Time
}

type humanRecord struct {
	Provider  string           `json:"provider"`
	Challenge altcha.Challenge `json:"challenge"`
	Purpose   string           `json:"purpose"`
	Binding   string           `json:"binding"`
	Issued    time.Time        `json:"issued"`
	NotBefore time.Time        `json:"not_before"`
	Expires   time.Time        `json:"expires"`
}

func (h Human) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h Human) ready() error {
	if h.Store == nil || len(h.Key) != 32 {
		return errors.New("human verification unavailable")
	}
	return h.Policy.Validate()
}

func (h Human) secret(scope string) string {
	m := hmac.New(sha256.New, h.Key)
	m.Write([]byte("weir/human/v1/" + scope))
	return hex.EncodeToString(m.Sum(nil))
}

func humanPurpose(p string) bool {
	return p == "login" || p == "register" || p == "reset" || p == "email"
}

func (h Human) Issue(ctx context.Context, purpose, binding string) (altcha.Challenge, error) {
	if err := h.ready(); err != nil {
		return altcha.Challenge{}, err
	}
	if !humanPurpose(purpose) || binding == "" {
		return altcha.Challenge{}, ErrHumanInvalid
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(h.Policy.Counter)))
	if err != nil {
		return altcha.Challenge{}, err
	}
	counter := int(n.Int64()) + 1
	now := h.now()
	expires := now.Add(time.Duration(h.Policy.LifetimeSeconds) * time.Second)
	challenge, err := altcha.CreateChallenge(altcha.CreateChallengeOptions{
		Algorithm: "PBKDF2/SHA-256", Cost: h.Policy.Cost, Counter: &counter,
		DeriveKey: altcha.DeriveKeyPBKDF2(), HMACSignatureSecret: h.secret("challenge"),
		HMACKeySignatureSecret: h.secret("solution"), ExpiresAt: &expires,
		Data: map[string]interface{}{"minimumSeconds": h.Policy.MinSeconds},
	})
	if err != nil {
		return altcha.Challenge{}, err
	}
	record := humanRecord{Provider: h.Provider(purpose), Challenge: challenge, Purpose: purpose, Binding: h.secret("binding/" + binding), Issued: now,
		NotBefore: now.Add(time.Duration(h.Policy.MinSeconds) * time.Second), Expires: expires}
	b, err := json.Marshal(record)
	if err != nil {
		return altcha.Challenge{}, err
	}
	if err = h.Store.Set(ctx, "human:"+challenge.Signature, string(b), time.Duration(h.Policy.LifetimeSeconds)*time.Second); err != nil {
		return altcha.Challenge{}, err
	}
	return challenge, nil
}

func (h Human) Verify(ctx context.Context, purpose, binding, payload, honeypot string) error {
	return h.verify(ctx, purpose, binding, payload, honeypot, nil)
}

func (h Human) Provider(purpose string) string {
	if purpose == "login" {
		return "altcha"
	}
	return h.Policy.Provider
}

// VerifyExternal retains the local binding, expiry and single-use ticket checks.
// The host must additionally verify the provider token before consuming.
func (h Human) VerifyExternal(ctx context.Context, purpose, binding, payload, honeypot string, verify func(context.Context) error) error {
	if verify == nil {
		return ErrHumanInvalid
	}
	return h.verify(ctx, purpose, binding, payload, honeypot, verify)
}

func (h Human) verify(ctx context.Context, purpose, binding, payload, honeypot string, external func(context.Context) error) error {
	if err := h.ready(); err != nil {
		return err
	}
	if honeypot != "" || !humanPurpose(purpose) || binding == "" || len(payload) > 8192 {
		return ErrHumanInvalid
	}
	if payload == "" {
		return ErrHumanRequired
	}
	b, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil {
		return ErrHumanInvalid
	}
	var proof altcha.Payload
	if json.Unmarshal(b, &proof) != nil || len(proof.Challenge.Signature) != 64 || proof.Solution.Counter < 0 || proof.Solution.Counter > 1024 {
		return ErrHumanInvalid
	}
	key := "human:" + proof.Challenge.Signature
	raw, ok, err := h.Store.Get(ctx, key)
	if err != nil {
		return err
	}
	if !ok {
		return ErrHumanInvalid
	}
	var record humanRecord
	if json.Unmarshal([]byte(raw), &record) != nil {
		return errors.New("invalid stored human challenge")
	}
	now := h.now()
	if record.Purpose != purpose || !hmac.Equal([]byte(record.Binding), []byte(h.secret("binding/"+binding))) || now.Before(record.NotBefore) || !now.Before(record.Expires) {
		return ErrHumanInvalid
	}
	if record.Provider != h.Provider(purpose) {
		return ErrHumanInvalid
	}
	if external != nil {
		if record.Provider != "turnstile" || purpose == "login" {
			return ErrHumanInvalid
		}
		if err := external(ctx); err != nil {
			return err
		}
	} else {
		if record.Provider != "altcha" || len(proof.Solution.DerivedKey) != 64 {
			return ErrHumanInvalid
		}
		result, err := altcha.VerifySolution(altcha.VerifySolutionOptions{
			Challenge: record.Challenge, Solution: proof.Solution, DeriveKey: altcha.DeriveKeyPBKDF2(),
			HMACSignatureSecret: h.secret("challenge"), HMACKeySignatureSecret: h.secret("solution"),
		})
		if err != nil {
			return err
		}
		if !result.Verified {
			return ErrHumanInvalid
		}
	}
	consumed, ok, err := h.Store.GetDel(ctx, key)
	if err != nil {
		return err
	}
	if !ok || consumed != raw {
		return ErrHumanInvalid
	}
	return nil
}
