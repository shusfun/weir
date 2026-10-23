package center

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/imzyb/MiGate/internal/mailer"
	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/setting"
	"github.com/imzyb/MiGate/internal/store/ent/user"
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

func (s *Server) sendMail(ctx context.Context, to, subject, body string) error {
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
		failure(c, 503, "settings_unavailable")
		return
	}
	c.JSON(200, smtpView(cfg, ok))
}

func (s *Server) putSMTP(c *gin.Context) {
	var req struct {
		SMTPSettings
		AccountPassword string `json:"account_password"`
		Code            string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.AccountPassword, req.Code) {
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
			failure(c, 500, "encrypt_failed")
			return
		}
	}
	if err := s.Store.Ent.Setting.Create().SetKey(smtpKey).SetValue(toMap(cfg)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "settings.smtp", "", "ok")
	c.JSON(200, smtpView(cfg, cfg.Host != "" && cfg.From != ""))
}

// testSMTP 给当前管理员自己发一封测试邮件。
func (s *Server) testSMTP(c *gin.Context) {
	u := c.MustGet("account").(*ent.User)
	if err := s.sendMail(c.Request.Context(), u.Email, "MiGate 测试邮件", "这是一封测试邮件，说明邮件服务设置可用。"); err != nil {
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
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	if err := s.Store.Ent.Setting.Create().SetKey("security").SetValue(toMap(req.SecuritySettings)).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(c.Request.Context()); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "settings.security", "", "ok")
	c.JSON(200, req.SecuritySettings)
}

func emailCodeKey(purpose, email string) string { return "email-code:" + purpose + ":" + digest(email) }

type emailCode struct {
	Code     string `json:"code"`
	Attempts int    `json:"attempts"`
}

func sixDigits() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06d", n.Int64())
}

// sendEmailCode 发送 10 分钟有效的 6 位验证码；无论账号是否存在都返回成功。
func (s *Server) sendEmailCode(c *gin.Context) {
	if !s.rate(c, "email-code", 5) {
		return
	}
	var req struct {
		Email   string `json:"email"`
		Purpose string `json:"purpose"`
	}
	if !bind(c, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !validEmail(email) || !catalogChoice(req.Purpose, "reset", "verify", "register") {
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
		failure(c, 503, "smtp_not_configured")
		return
	}
	exists, err := s.Store.Ent.User.Query().Where(user.EmailEQ(email)).Exist(ctx)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	// 找回密码只发给已存在的账号；注册只发给未注册的邮箱。不满足时静默成功。
	if exists == (req.Purpose == "reset") {
		code := sixDigits()
		raw, _ := json.Marshal(emailCode{Code: code})
		if err = s.Store.KV.Set(ctx, emailCodeKey(req.Purpose, email), string(raw), 10*time.Minute); err != nil {
			failure(c, 503, "email_code_unavailable")
			return
		}
		subject, body := "MiGate 找回密码", "你的验证码是 "+code+"，10 分钟内有效。如果不是你本人操作，请忽略这封邮件。"
		if req.Purpose != "reset" {
			subject, body = "MiGate 注册验证", "你的注册验证码是 "+code+"，10 分钟内有效。"
		}
		if err = s.sendMail(ctx, email, subject, body); err != nil {
			s.audit(c, "auth.email", req.Purpose, "failed")
			failure(c, 502, "smtp_failed")
			return
		}
	}
	c.JSON(200, gin.H{"sent": true})
}

// checkCode 校验验证码：最多 5 次，通过后作废。
func (s *Server) checkCode(ctx context.Context, purpose, email, code string) bool {
	key := emailCodeKey(purpose, email)
	raw, ok, err := s.Store.KV.Get(ctx, key)
	var ec emailCode
	if err != nil || !ok || json.Unmarshal([]byte(raw), &ec) != nil {
		return false
	}
	if ec.Attempts >= 5 {
		_ = s.Store.KV.Del(ctx, key)
		return false
	}
	if ec.Code != strings.TrimSpace(code) {
		ec.Attempts++
		b, _ := json.Marshal(ec)
		_ = s.Store.KV.Set(ctx, key, string(b), 10*time.Minute)
		return false
	}
	_ = s.Store.KV.Del(ctx, key)
	return true
}

func (s *Server) resetPassword(c *gin.Context) {
	if !s.rate(c, "email-reset", 10) {
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !validAccountPassword(req.Password) {
		failure(c, 400, "weak_password")
		return
	}
	ctx := c.Request.Context()
	if !s.checkCode(ctx, "reset", email, req.Code) {
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
		failure(c, 500, "hash_failed")
		return
	}
	if err = u.Update().SetPasswordHash(string(hash)).Exec(ctx); err != nil {
		failure(c, 500, "update_failed")
		return
	}
	s.dropSessions(ctx, u.ID)
	_, _ = s.Store.Ent.Audit.Create().SetActor(u.ID).SetAction("auth.reset").SetTarget("user:" + strconv.Itoa(u.ID)).SetResult("ok").SetIP(c.ClientIP()).Save(ctx)
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
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !validEmail(email) {
		failure(c, 400, "invalid_email")
		return
	}
	if !validAccountPassword(req.Password) {
		failure(c, 400, "weak_password")
		return
	}
	ctx := c.Request.Context()
	if p.EmailVerification && !s.checkCode(ctx, "register", email, req.Code) {
		failure(c, 400, "invalid_email_code")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		failure(c, 500, "hash_failed")
		return
	}
	u, err := s.Store.Ent.User.Create().SetEmail(email).SetPasswordHash(string(hash)).SetAllNodes(false).SetAllowChain(false).Save(ctx)
	if err != nil {
		failure(c, 409, "email_taken")
		return
	}
	if _, err = s.Store.Ent.Link.Create().SetUserID(u.ID).SetName("默认").SetToken(linkToken()).Save(ctx); err != nil {
		failure(c, 500, "link_failed")
		return
	}
	_, _ = s.Store.Ent.Audit.Create().SetActor(u.ID).SetAction("auth.register").SetTarget("user:" + strconv.Itoa(u.ID)).SetResult("ok").SetIP(c.ClientIP()).Save(ctx)
	c.JSON(201, gin.H{"registered": true})
}
