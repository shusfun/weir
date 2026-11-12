package center

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/shusfun/weir/internal/accountsecurity"
	"github.com/shusfun/weir/internal/mailer"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/accesscode"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/internal/store/ent/user"
)

// 邮件：SMTP 设置、找回密码与（可选的）自助注册。自助注册默认关闭，注册的用户不会自动获得任何节点。
// 发送验证码的接口对「账号是否存在」不作区分，避免被用来探测邮箱。

const smtpKey = "smtp"

// SMTPSettings 是邮件服务设置；密码加密保存、只写不读。
type SMTPSettings struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	From     string `json:"from"`
	FromName string `json:"from_name"`
	UseTLS   bool   `json:"use_tls"`
}

// Mailer 发送邮件；测试中替换。
type Mailer interface {
	Send(cfg mailer.SMTPConfig, to, subject, body string) error
}

type smtpMailer struct{}

func (smtpMailer) Send(cfg mailer.SMTPConfig, to, subject, body string) error {
	return (&mailer.EmailService{}).SendEmailWithConfig(&cfg, to, subject, body)
}

func (s *Server) mailer() Mailer {
	if s.Mail != nil {
		return s.Mail
	}
	return smtpMailer{}
}

func (s *Server) mailRoutes(ready, admin *gin.RouterGroup) {
	s.accountPolicyRoutes(admin)
	s.turnstileRoutes(admin)
	ready.POST("/auth/email", s.sendEmailCode)
	ready.POST("/auth/reset", s.resetPassword)
	ready.POST("/auth/register", s.register)
	ready.POST("/auth/verify", s.register)
	admin.GET("/settings/security", s.getSecurity)
	admin.PUT("/settings/security", s.putSecurity)
	admin.GET("/settings/smtp", s.getSMTP)
	admin.PUT("/settings/smtp", s.putSMTP)
	admin.POST("/settings/smtp/test", s.testSMTP)
}

func (s *Server) smtpSettings(ctx context.Context) (SMTPSettings, bool, error) {
	var cfg SMTPSettings
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(smtpKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	b, _ := json.Marshal(row.Value)
	if err = json.Unmarshal(b, &cfg); err != nil {
		return cfg, false, err
	}
	return cfg, cfg.Host != "" && cfg.From != "", nil
}

func (s *Server) sendMail(ctx context.Context, to, subject, body string) (err error) {
	started := time.Now()
	trace := taskTrace(ctx)
	defer func() {
		outcome, code := "succeeded", ""
		if err != nil {
			outcome, code = "failed", "smtp_failed"
		}
		slog.InfoContext(ctx, "mail delivery", "trace", trace, "outcome", outcome, "code", code, "ms", time.Since(started).Milliseconds())
	}()
	cfg, ok, err := s.smtpSettings(ctx)
	if err != nil || !ok {
		return errors.New("smtp_not_configured")
	}
	pw, err := s.reveal(cfg.Password)
	if err != nil {
		return err
	}
	return s.mailer().Send(mailer.SMTPConfig{Host: cfg.Host, Port: cfg.Port, Username: cfg.Username, Password: pw, From: cfg.From, FromName: cfg.FromName, UseTLS: cfg.UseTLS}, to, subject, body)
}

func smtpView(cfg SMTPSettings, configured bool) gin.H {
	return gin.H{"host": cfg.Host, "port": cfg.Port, "username": cfg.Username, "password_set": cfg.Password != "", "from": cfg.From, "from_name": cfg.FromName, "use_tls": cfg.UseTLS, "configured": configured}
}

func (s *Server) getSMTP(c *gin.Context) {
	cfg, ok, err := s.smtpSettings(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "settings_unavailable", err)
		return
	}
	c.JSON(200, smtpView(cfg, ok))
}

func (s *Server) putSMTP(c *gin.Context) {
	var req struct {
		SMTPSettings
	}
	if !bind(c, &req) {
		return
	}
	cfg := req.SMTPSettings
	cfg.Host, cfg.From = strings.TrimSpace(cfg.Host), strings.TrimSpace(cfg.From)
	if cfg.Host != "" && (strings.ContainsAny(cfg.Host, " /@\r\n") || cfg.Port < 1 || cfg.Port > 65535 || !validEmail(cfg.From) || len(cfg.Username) > 255 || len(cfg.FromName) > 100 || strings.ContainsAny(cfg.FromName, "\r\n")) {
		failure(c, 400, "invalid_smtp_settings")
		return
	}
	ctx := c.Request.Context()
	old, _, _ := s.smtpSettings(ctx)
	if cfg.Password == "" {
		cfg.Password = old.Password
	} else {
		var err error
		if cfg.Password, err = s.Crypto.Encrypt(cfg.Password); err != nil {
			failInternal(c, 500, "encrypt_failed", err)
			return
		}
	}
	if err := s.Store.Ent.Setting.Create().SetKey(smtpKey).SetValue(toMap(cfg)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	s.audit(c, "settings.smtp", "", "ok")
	c.JSON(200, smtpView(cfg, cfg.Host != "" && cfg.From != ""))
}

// testSMTP 给当前管理员自己发一封测试邮件。
func (s *Server) testSMTP(c *gin.Context) {
	u := c.MustGet("account").(*ent.User)
	if err := s.sendMail(c.Request.Context(), u.Email, "Weir 测试邮件", "这是一封测试邮件，说明邮件服务设置可用。"); err != nil {
		failDetail(c, 502, "smtp_failed", err.Error(), nil)
		return
	}
	c.JSON(200, gin.H{"sent_to": u.Email})
}

func (s *Server) getSecurity(c *gin.Context) {
	p, ok := s.securityPolicy(c)
	if ok {
		c.JSON(200, p)
	}
}

func (s *Server) putSecurity(c *gin.Context) {
	var req struct {
		SecuritySettings
	}
	if !bind(c, &req) {
		return
	}
	if req.RequireAdminMFA && !req.TOTP {
		failDetail(c, 400, "invalid_security_settings", "管理员必须二次验证时必须允许 TOTP", gin.H{"field": "require_admin_mfa", "reason": "totp_required"})
		return
	}
	if err := s.Store.Ent.Setting.Create().SetKey("security").SetValue(toMap(req.SecuritySettings)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(c.Request.Context()); err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	s.audit(c, "settings.security", "", "ok")
	c.JSON(200, req.SecuritySettings)
}

// sendEmailCode 发送 10 分钟有效的 6 位验证码；无论账号是否存在都返回成功。
func (s *Server) sendEmailCode(c *gin.Context) {
	var req struct {
		humanProof
		Email      string `json:"email"`
		Purpose    string `json:"purpose"`
		InviteCode string `json:"invite_code"`
	}
	if !bind(c, &req) {
		return
	}
	email, err := accountsecurity.Mailbox(req.Email)
	if err != nil || !catalogChoice(req.Purpose, "reset", "verify", "register") {
		failure(c, 400, "invalid_request")
		return
	}
	ctx := c.Request.Context()
	p, ok := s.securityPolicy(c)
	if !ok {
		return
	}
	if req.Purpose != "reset" && (!p.Registration || !p.EmailVerification) {
		failure(c, 403, "registration_disabled")
		return
	}
	if _, configured, err := s.smtpSettings(ctx); err != nil || !configured {
		failInternal(c, 503, "smtp_not_configured", err)
		return
	}
	view, err := s.effectiveAccountPolicy(ctx)
	if err != nil {
		failInternal(c, 503, "account_policy_unavailable", err)
		return
	}
	policy, err := accountsecurity.ParsePolicy(view.Effective)
	if err != nil {
		failInternal(c, 503, "account_policy_unavailable", err)
		return
	}
	if err = accountsecurity.ReserveMail(ctx, s.Store.KV, policy.MailLimits, email, c.ClientIP(), time.Now()); err != nil {
		var limited *accountsecurity.LimitError
		if errors.As(err, &limited) {
			seconds := max(1, int(time.Until(limited.Until).Seconds()+1))
			c.Header("Retry-After", strconv.Itoa(seconds))
			if limited.First {
				s.securityEvent(c, "email-send-limit", limited.Scope, "warning")
			}
			failDetail(c, 429, "email_rate_limited", "发信请求已达到限制，请稍后重试", gin.H{"retry_after": seconds})
		} else {
			failInternal(c, 503, "email_code_unavailable", err)
		}
		return
	}
	if !s.verifyHuman(c, "email", req.humanProof) {
		return
	}
	if req.Purpose != "reset" && !s.registrationEmail(c, email, policy.EmailPolicy) {
		return
	}
	if req.Purpose != "reset" && (policy.InviteRequired || req.InviteCode != "") {
		if _, ok := s.checkAccessCode(c, accesscode.KindInvite, strings.TrimSpace(req.InviteCode), email, policy.AccessPolicy); !ok {
			return
		}
	}
	codes := s.emailCodes()
	codes.Policy = policy.Codes()
	code, err := codes.Issue(ctx, req.Purpose, email)
	if err != nil {
		var cooldown *accountsecurity.CooldownError
		if errors.As(err, &cooldown) {
			seconds := max(1, int(time.Until(cooldown.Until).Seconds()+1))
			c.Header("Retry-After", strconv.Itoa(seconds))
			failDetail(c, 429, "email_code_cooldown", "请等待冷却结束后再发送", gin.H{"retry_after": seconds})
		} else {
			failInternal(c, 503, "email_code_unavailable", err)
		}
		return
	}
	exists, err := s.Store.Ent.User.Query().Where(user.EmailEQ(email)).Exist(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	// 找回密码只发给已存在的账号；注册只发给未注册的邮箱。不满足时静默成功。
	if exists == (req.Purpose == "reset") {
		minutes := strconv.Itoa(policy.LifetimeSeconds / 60)
		subject, body := "Weir 找回密码", "你的验证码是 "+code+"，"+minutes+" 分钟内有效。如果不是你本人操作，请忽略这封邮件。"
		if req.Purpose != "reset" {
			subject, body = "Weir 注册验证", "你的注册验证码是 "+code+"，"+minutes+" 分钟内有效。"
		}
		if err = s.sendMail(ctx, email, subject, body); err != nil {
			s.audit(c, "auth.email", req.Purpose, "failed")
			failInternal(c, 502, "smtp_failed", err)
			return
		}
	}
	c.JSON(200, gin.H{"sent": true, "expires_in": policy.LifetimeSeconds, "retry_after": policy.CooldownSeconds[0]})
}

func (s *Server) resetPassword(c *gin.Context) {
	if !s.rate(c, "email-reset", 10) {
		return
	}
	var req struct {
		humanProof
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	email, parseErr := accountsecurity.Mailbox(req.Email)
	if parseErr != nil {
		failure(c, 400, "invalid_email_code")
		return
	}
	if !validAccountPassword(req.Password) {
		failure(c, 400, "weak_password")
		return
	}
	if !s.verifyHuman(c, "reset", req.humanProof) {
		return
	}
	ctx := c.Request.Context()
	valid, err := s.emailCodes().Verify(ctx, "reset", email, req.Code)
	if err != nil {
		failInternal(c, 503, "email_code_unavailable", err)
		return
	}
	if !valid {
		failure(c, 400, "invalid_email_code")
		return
	}
	u, err := s.Store.Ent.User.Query().Where(user.EmailEQ(email)).Only(ctx)
	if err != nil {
		failure(c, 400, "invalid_email_code")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		failInternal(c, 500, "hash_failed", err)
		return
	}
	if err = u.Update().SetPasswordHash(string(hash)).Exec(ctx); err != nil {
		failInternal(c, 500, "update_failed", err)
		return
	}
	s.dropSessions(ctx, u.ID)
	_, _ = s.Store.Ent.Audit.Create().SetActor(u.ID).SetAction("auth.reset").SetTarget("user:" + strconv.Itoa(u.ID)).SetResult("ok").SetIP(c.ClientIP()).SetDetail(traceDetail(c)).Save(ctx)
	c.JSON(200, gin.H{"reset": true})
}

// register 自助注册：需要后台开启；开启邮箱验证时须提供验证码。注册的用户没有任何节点，由管理员分配。
func (s *Server) register(c *gin.Context) {
	if !s.rate(c, "register", 10) {
		return
	}
	p, ok := s.securityPolicy(c)
	if !ok {
		return
	}
	if !p.Registration {
		failure(c, 403, "registration_disabled")
		return
	}
	var req struct {
		humanProof
		Email      string `json:"email"`
		Password   string `json:"password"`
		Code       string `json:"code"`
		InviteCode string `json:"invite_code"`
	}
	if !bind(c, &req) {
		return
	}
	email, parseErr := accountsecurity.Mailbox(req.Email)
	if parseErr != nil {
		failure(c, 400, "invalid_email")
		return
	}
	if !validAccountPassword(req.Password) {
		failure(c, 400, "weak_password")
		return
	}
	ctx := c.Request.Context()
	view, err := s.effectiveAccountPolicy(ctx)
	if err != nil {
		failInternal(c, 503, "account_policy_unavailable", err)
		return
	}
	policy, err := accountsecurity.ParsePolicy(view.Effective)
	if err != nil {
		failInternal(c, 503, "account_policy_unavailable", err)
		return
	}
	if !s.verifyHuman(c, "register", req.humanProof) {
		return
	}
	if !s.registrationEmail(c, email, policy.EmailPolicy) {
		return
	}
	var invite *ent.AccessCode
	if policy.InviteRequired || req.InviteCode != "" {
		var valid bool
		invite, valid = s.checkAccessCode(c, accesscode.KindInvite, strings.TrimSpace(req.InviteCode), email, policy.AccessPolicy)
		if !valid {
			return
		}
	}
	if p.EmailVerification {
		valid, err := s.emailCodes().Verify(ctx, "register", email, req.Code)
		if err != nil {
			failInternal(c, 503, "email_code_unavailable", err)
			return
		}
		if !valid {
			failure(c, 400, "invalid_email_code")
			return
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		failInternal(c, 500, "hash_failed", err)
		return
	}
	err = s.Store.Lock.Do(ctx, "registration-identity", func(ctx context.Context) error {
		var grantSyncID int
		current, err := s.effectiveAccountPolicy(ctx)
		if err != nil {
			return err
		}
		if current.Revision != view.Revision {
			return errors.New("account policy changed during registration")
		}
		identity, err := policy.EmailPolicy.Identity(email)
		if err != nil {
			return err
		}
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			failInternal(c, 503, "registration_unavailable", err)
			return nil
		}
		defer tx.Rollback()
		exists, err := registrationIdentityExists(ctx, tx.Client(), policy.EmailPolicy, identity)
		if err != nil {
			return err
		}
		if exists {
			failure(c, 409, "email_taken")
			return nil
		}
		u, err := tx.User.Create().SetEmail(email).SetNormalizedEmail(identity).SetPasswordHash(string(hash)).SetAllNodes(false).SetAllowChain(false).Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				failure(c, 409, "email_taken")
			} else {
				failInternal(c, 503, "registration_unavailable", err)
			}
			return nil
		}
		if _, err = tx.Link.Create().SetUserID(u.ID).SetName("默认").SetToken(linkToken()).Save(ctx); err != nil {
			failInternal(c, 500, "link_failed", err)
			return nil
		}
		if invite != nil {
			if err = claimAccessCode(ctx, tx, invite, u.ID); err != nil {
				if err.Error() == "invalid_access_code" {
					failure(c, 400, "invalid_access_code")
				} else {
					failInternal(c, 503, "registration_unavailable", err)
				}
				return nil
			}
			if err = applyAccountGrant(ctx, tx, u, invite.Grant, true); err != nil {
				failInternal(c, 503, "registration_unavailable", err)
				return nil
			}
			storeCopy := *s.Store
			storeCopy.Ent = tx.Client()
			serverCopy := &Server{Store: &storeCopy, Config: s.Config, Crypto: s.Crypto}
			if _, err = serverCopy.syncUserLocked(ctx, u.ID); err != nil {
				failInternal(c, 503, "registration_unavailable", err)
				return nil
			}
			marker, e := markAccountGrant(ctx, tx, u.ID, invite.ID)
			if e != nil {
				failInternal(c, 503, "registration_unavailable", e)
				return nil
			}
			grantSyncID = marker.ID
		}
		registrationDetail := traceDetail(c)
		if invite != nil {
			registrationDetail["code_id"], registrationDetail["plan_id"], registrationDetail["plan_revision"] = invite.ID, invite.PlanID, invite.PlanRevision
		}
		if _, err = tx.Audit.Create().SetActor(u.ID).SetAction("auth.register").SetTarget("user:" + strconv.Itoa(u.ID)).SetResult("ok").SetIP(c.ClientIP()).SetDetail(registrationDetail).Save(ctx); err != nil {
			failInternal(c, 503, "registration_unavailable", err)
			return nil
		}
		if err = tx.Commit(); err != nil {
			failInternal(c, 503, "registration_unavailable", err)
			return nil
		}
		if invite != nil {
			if err = s.reconcileAccountGrant(ctx, grantSyncID); err != nil {
				c.JSON(202, gin.H{"registered": true, "synchronization": "pending", "message": "注册已完成，VPS 配置同步尚未完成，将自动重试"})
				return nil
			}
		}
		c.JSON(201, gin.H{"registered": true})
		return nil
	})
	if err != nil {
		failInternal(c, 503, "registration_unavailable", err)
	}
}
