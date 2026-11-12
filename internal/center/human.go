package center

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/accountsecurity"
)

type humanProof struct {
	Turnstile string `json:"turnstile"`
	Altcha    string `json:"altcha"`
	Website   string `json:"website"`
}

func (s *Server) humanService(c *gin.Context) (accountsecurity.Human, bool) {
	view, err := s.effectiveAccountPolicy(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "account_policy_unavailable", err)
		return accountsecurity.Human{}, false
	}
	p, err := accountsecurity.ParsePolicy(view.Effective)
	if err != nil {
		failInternal(c, 503, "account_policy_unavailable", err)
		return accountsecurity.Human{}, false
	}
	key, err := hex.DecodeString(s.Config.EncryptionKey)
	if err != nil {
		failInternal(c, 503, "human_verification_unavailable", err)
		return accountsecurity.Human{}, false
	}
	return accountsecurity.Human{Store: s.Store.KV, Key: key, Policy: p.HumanPolicy}, true
}

func (s *Server) humanChallenge(c *gin.Context) {
	h, ok := s.humanService(c)
	if !ok {
		return
	}
	if !s.rate(c, "human-challenge", h.Policy.ChallengeIP) {
		return
	}
	purpose := c.Query("purpose")
	if !catalogChoice(purpose, "login", "register", "reset", "email") {
		failure(c, 400, "invalid_request")
		return
	}
	n, err := s.Store.KV.Incr(c.Request.Context(), "human-challenge:site", time.Minute)
	if err != nil {
		failInternal(c, 503, "human_verification_unavailable", err)
		return
	}
	if n > h.Policy.ChallengeSite {
		failure(c, 429, "rate_limited")
		return
	}
	started := time.Now()
	var cfg turnstileSettings
	if h.Provider(purpose) == "turnstile" {
		cfg, err = s.turnstileSettings(c.Request.Context())
		if err != nil || !cfg.configured() {
			failInternal(c, 503, "human_verification_unavailable", err)
			return
		}
	}
	challenge, err := h.Issue(c.Request.Context(), purpose, c.ClientIP())
	if err != nil {
		failInternal(c, 503, "human_verification_unavailable", err)
		return
	}
	slog.InfoContext(c.Request.Context(), "human challenge", "trace", taskTrace(c.Request.Context()), "purpose", purpose, "outcome", "issued", "ms", time.Since(started).Milliseconds())
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"parameters": challenge.Parameters, "signature": challenge.Signature, "provider": h.Provider(purpose), "sitekey": cfg.SiteKey})
}

func (s *Server) verifyHuman(c *gin.Context, purpose string, proof humanProof) bool {
	h, ok := s.humanService(c)
	if !ok {
		return false
	}
	started := time.Now()
	var err error
	if h.Provider(purpose) == "turnstile" {
		var cfg turnstileSettings
		cfg, err = s.turnstileSettings(c.Request.Context())
		if err == nil {
			err = h.VerifyExternal(c.Request.Context(), purpose, c.ClientIP(), proof.Altcha, proof.Website, func(ctx context.Context) error {
				return s.verifyTurnstile(ctx, cfg, proof.Turnstile, purpose, c.ClientIP())
			})
		}
	} else {
		err = h.Verify(c.Request.Context(), purpose, c.ClientIP(), proof.Altcha, proof.Website)
	}
	code, outcome := "", "verified"
	if err != nil {
		code, outcome = "human_verification_unavailable", "failed"
		if errors.Is(err, accountsecurity.ErrHumanInvalid) || errors.Is(err, accountsecurity.ErrHumanRequired) {
			code, outcome = err.Error(), "rejected"
		}
	}
	slog.InfoContext(c.Request.Context(), "human verification", "trace", taskTrace(c.Request.Context()), "purpose", purpose, "outcome", outcome, "code", code, "ms", time.Since(started).Milliseconds())
	if err == nil {
		return true
	}
	status := 503
	if outcome == "rejected" {
		status = 400
	}
	failure(c, status, code)
	return false
}

// loginHumanRequired 看来源 IP 与（已知时）目标账号的失败次数，任一达到阈值就要求人机验证。
func (s *Server) loginHumanRequired(c *gin.Context, email string) (bool, bool) {
	h, ok := s.humanService(c)
	if !ok {
		return false, false
	}
	keys := []string{failedLoginIPKey(c)}
	if email != "" {
		keys = append(keys, failedLoginAccountKey(email))
	}
	for _, key := range keys {
		raw, exists, err := s.Store.KV.Get(c.Request.Context(), key)
		if err != nil {
			failInternal(c, 503, "human_verification_unavailable", err)
			return false, false
		}
		if !exists {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			failInternal(c, 503, "human_verification_unavailable", err)
			return false, false
		}
		if n >= h.Policy.LoginFailures {
			return true, true
		}
	}
	return false, true
}
