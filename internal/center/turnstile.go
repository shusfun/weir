package center

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/accountsecurity"
	"github.com/shusfun/weir/internal/policydata"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/setting"
)

type turnstileSettings struct {
	SiteKey   string   `json:"site_key"`
	Secret    string   `json:"secret,omitempty"`
	Hostnames []string `json:"hostnames"`
}

func (s *Server) validateHumanProvider(ctx context.Context, values policydata.Values) error {
	p, err := accountsecurity.ParsePolicy(values)
	if err != nil {
		return err
	}
	if p.Provider != "turnstile" {
		return nil
	}
	cfg, err := s.turnstileSettings(ctx)
	if err != nil {
		return err
	}
	if !cfg.configured() {
		return errors.New("请先配置 Turnstile Site Key、Secret 和站点域名")
	}
	return nil
}

func (s *Server) turnstileSettings(ctx context.Context) (turnstileSettings, error) {
	var cfg turnstileSettings
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ("turnstile")).Only(ctx)
	if ent.IsNotFound(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	b, err := json.Marshal(row.Value)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(b, &cfg)
	return cfg, err
}

func (c turnstileSettings) configured() bool {
	return c.SiteKey != "" && c.Secret != "" && len(c.Hostnames) > 0
}
func (c turnstileSettings) view() gin.H {
	return gin.H{"site_key": c.SiteKey, "secret_set": c.Secret != "", "hostnames": c.Hostnames, "configured": c.configured()}
}

func (s *Server) turnstileRoutes(admin *gin.RouterGroup) {
	admin.GET("/settings/turnstile", func(c *gin.Context) {
		cfg, err := s.turnstileSettings(c.Request.Context())
		if err != nil {
			failInternal(c, 503, "settings_unavailable", err)
			return
		}
		c.JSON(200, cfg.view())
	})
	admin.PUT("/settings/turnstile", func(c *gin.Context) {
		var cfg turnstileSettings
		if !bindWorker(c, &cfg, 8<<10) {
			return
		}
		if len(cfg.SiteKey) > 256 || len(cfg.Secret) > 2048 || len(cfg.Hostnames) > 20 || strings.ContainsAny(cfg.SiteKey, " \r\n\t") {
			failure(c, 400, "invalid_turnstile_settings")
			return
		}
		for i, host := range cfg.Hostnames {
			host = strings.ToLower(strings.TrimSpace(host))
			if !validHost(host) || strings.ContainsAny(host, ":[]") {
				failure(c, 400, "invalid_turnstile_settings")
				return
			}
			cfg.Hostnames[i] = host
		}
		old, err := s.turnstileSettings(c.Request.Context())
		if err != nil {
			failInternal(c, 503, "settings_unavailable", err)
			return
		}
		if cfg.Secret == "" {
			cfg.Secret = old.Secret
		} else if cfg.Secret, err = s.Crypto.Encrypt(cfg.Secret); err != nil {
			failInternal(c, 503, "encrypt_failed", err)
			return
		}
		if cfg.SiteKey != "" && !cfg.configured() {
			failure(c, 400, "invalid_turnstile_settings")
			return
		}
		if cfg.SiteKey == "" {
			view, err := s.accountPolicy(c.Request.Context())
			if err != nil {
				failInternal(c, 503, "account_policy_unavailable", err)
				return
			}
			p, err := accountsecurity.ParsePolicy(view.Effective)
			if err != nil {
				failInternal(c, 503, "account_policy_unavailable", err)
				return
			}
			if p.Provider == "turnstile" {
				failDetail(c, 400, "invalid_turnstile_settings", "请先把账户策略恢复为 altcha，再清空 Site Key", nil)
				return
			}
		}
		if err = s.Store.Ent.Setting.Create().SetKey("turnstile").SetValue(toMap(cfg)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(c.Request.Context()); err != nil {
			failInternal(c, 503, "save_failed", err)
			return
		}
		s.audit(c, "settings.turnstile", "", "ok")
		c.JSON(200, cfg.view())
	})
}

func (s *Server) verifyTurnstile(ctx context.Context, cfg turnstileSettings, token, purpose, ip string) (err error) {
	started := time.Now()
	defer func() {
		outcome, code := "verified", ""
		if err != nil {
			outcome, code = "failed", "human_verification_unavailable"
			if errors.Is(err, accountsecurity.ErrHumanInvalid) {
				outcome, code = "rejected", err.Error()
			}
		}
		slog.InfoContext(ctx, "turnstile verification", "trace", taskTrace(ctx), "outcome", outcome, "code", code, "ms", time.Since(started).Milliseconds())
	}()
	if !cfg.configured() {
		return errors.New("turnstile not configured")
	}
	if token == "" || len(token) > 2048 {
		return accountsecurity.ErrHumanInvalid
	}
	secret, err := s.reveal(cfg.Secret)
	if err != nil {
		return err
	}
	check, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	form := url.Values{"secret": {secret}, "response": {token}, "remoteip": {ip}}
	req, err := http.NewRequestWithContext(check, http.MethodPost, "https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("turnstile request failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := s.TurnstileHTTP
	if client == nil {
		client = turnstileHTTPClient
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("turnstile service unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("turnstile service returned non-success status")
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, (32<<10)+1))
	if err != nil || len(b) > 32<<10 {
		return errors.New("turnstile response unavailable or too large")
	}
	var result struct {
		Success     bool      `json:"success"`
		Hostname    string    `json:"hostname"`
		Action      string    `json:"action"`
		ChallengeTS time.Time `json:"challenge_ts"`
	}
	if json.Unmarshal(b, &result) != nil {
		return errors.New("turnstile response invalid")
	}
	matched := false
	for _, host := range cfg.Hostnames {
		if strings.EqualFold(result.Hostname, host) {
			matched = true
		}
	}
	if !result.Success || !matched || result.Action != purpose || result.ChallengeTS.IsZero() || time.Since(result.ChallengeTS) > 5*time.Minute || time.Until(result.ChallengeTS) > time.Minute {
		return accountsecurity.ErrHumanInvalid
	}
	return nil
}

var turnstileHTTPClient = &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
