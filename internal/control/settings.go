package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/ent/user"
	"github.com/imzyb/MiGate/internal/mailer"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) getSettings(c *gin.Context) {
	data, err := readSystemSettings(c.Request.Context(), s.DB)
	if err != nil {
		failure(c, 503, "settings_unavailable")
		return
	}
	hasSMTP, hasModel := data.SMTP.Password != "", data.Laya.Key != ""
	data.SMTP.Password = ""
	data.Laya.Key = ""
	c.JSON(200, gin.H{"security": data.Security, "smtp": data.SMTP, "laya": data.Laya, "smtp_password_set": hasSMTP, "laya_key_set": hasModel, "settings_revision": data.Revision})
}
func (s *Server) putSettings(c *gin.Context) {
	var req systemSettingsUpdate
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	changed, err := s.writeSystemSettings(c.Request.Context(), req)
	if errors.Is(err, errSettingsConflict) {
		failure(c, 409, "settings_changed")
		return
	}
	var invalid settingsValidationError
	if errors.As(err, &invalid) {
		failure(c, 400, string(invalid))
		return
	}
	if err != nil {
		failure(c, 503, "settings_save_failed")
		return
	}
	s.audit(c, "settings.update", "system", "ok")
	if changed {
		s.revokeAll(c.Request.Context(), c.GetInt("user_id"))
	}
	c.JSON(200, gin.H{"ok": true, "login_required": changed})
}
func (s *Server) smtpTest(c *gin.Context) {
	var req struct {
		To string `json:"to"`
	}
	if !bind(c, &req) || !validEmail(req.To) {
		failure(c, 400, "valid_email_required")
		return
	}
	if err := s.queueMail(c.Request.Context(), req.To, "MiGate 邮件测试", "<p>MiGate SMTP 测试邮件。</p>"); err != nil {
		failure(c, 400, "smtp_not_configured")
		return
	}
	c.JSON(202, gin.H{"status": "queued"})
}
func (s *Server) queueMail(ctx context.Context, to, subject, body string) error {
	var cfg mailer.SMTPConfig
	if !s.setting(ctx, "smtp", &cfg) || cfg.Host == "" {
		return fmt.Errorf("smtp not configured")
	}
	sealed, err := s.Crypto.Encrypt(body)
	if err != nil {
		return err
	}
	_, err = s.Store.Resource.Create().SetKind("mail").SetKey(randomToken(16)).SetData(map[string]any{"to": to, "subject": subject, "body": sealed, "status": "pending", "attempts": 0}).Save(ctx)
	return err
}

func (s *Server) retryMail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		failure(c, 400, "invalid_mail")
		return
	}
	if !s.rate(c, "mail-retry", 20) {
		return
	}
	result, err := s.DB.ExecContext(c.Request.Context(), `UPDATE migate.resources SET data=(data-'last_error')||'{"status":"pending"}'::jsonb WHERE id=$1 AND kind='mail' AND data->>'status'='failed' AND COALESCE((data->>'attempts')::int,0)<5 AND COALESCE(data->>'body','')<>''`, id)
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		failure(c, 409, "mail_not_retryable")
		return
	}
	s.audit(c, "mail.retry", fmt.Sprint(id), "queued")
	c.JSON(202, gin.H{"status": "queued"})
}
func (s *Server) Workers(ctx context.Context) {
	if s.Config.Installed {
		s.enqueueGateRelease(ctx)
	}
	go s.modelWorker(ctx)
	go s.securityWorker(ctx)
	go s.sourceWorker(ctx)
	go s.certificateWorker(ctx)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.configMu.RLock()
			if s.Config.Installed {
				s.processMail(ctx)
			}
			s.configMu.RUnlock()
		}
	}
}
func (s *Server) processMail(ctx context.Context) {
	// A session lock prevents multiple centers from sending the same pending
	// message concurrently. A crash after SMTP acceptance can still cause an
	// at-least-once retry; email delivery cannot promise exactly-once semantics.
	conn, err := acquireMailWorker(ctx, s.DB)
	if err != nil {
		return
	}
	defer releaseMailWorker(conn)
	var cfg mailer.SMTPConfig
	if !s.setting(ctx, "smtp", &cfg) || cfg.Host == "" {
		return
	}
	password, err := s.Crypto.Decrypt(cfg.Password)
	if err != nil {
		return
	}
	cfg.Password = password
	items, err := s.pendingMail(ctx)
	if err != nil {
		return
	}
	for _, item := range items {
		if item.Data["status"] != "pending" {
			continue
		}
		tries := int(number(item.Data["attempts"]))
		body, err := s.Crypto.Decrypt(text(item.Data["body"]))
		if err == nil {
			err = (&mailer.EmailService{}).SendEmailWithConfig(&cfg, text(item.Data["to"]), text(item.Data["subject"]), body)
		}
		item.Data["attempts"] = tries + 1
		if err == nil {
			item.Data["status"] = "sent"
			delete(item.Data, "body")
		} else {
			item.Data["last_error"] = "smtp_delivery_failed"
			if tries >= 2 {
				item.Data["status"] = "failed"
			}
		}
		s.Store.Resource.UpdateOneID(item.ID).SetData(item.Data).Save(ctx)
	}
}
func (s *Server) pendingMail(ctx context.Context) ([]*ent.Resource, error) {
	return s.Store.Resource.Query().Where(resource.KindEQ("mail"), func(q *sql.Selector) {
		q.Where(sqljson.ValueEQ(resource.FieldData, "pending", sqljson.Path("status")))
	}).Order(ent.Asc("id")).Limit(200).All(ctx)
}
func (s *Server) emailCode(c *gin.Context) {
	if !s.rate(c, "email", 5) {
		return
	}
	var req struct {
		Email   string `json:"email"`
		Purpose string `json:"purpose"`
	}
	if !bind(c, &req) || !validEmail(req.Email) {
		failure(c, 400, "valid_email_required")
		return
	}
	email := strings.ToLower(req.Email)
	if req.Purpose != "verify" && req.Purpose != "reset" {
		failure(c, 400, "invalid_purpose")
		return
	}
	u, err := s.Store.User.Query().Where(user.EmailEQ(email)).Only(c.Request.Context())
	security, policyOK := s.securityPolicy(c)
	if !policyOK {
		return
	}
	allowed := err == nil && u.Enabled
	if req.Purpose == "verify" && security.Registration {
		allowed = true
	}
	if allowed {
		code := randomToken(16)
		key := "migate:email:" + req.Purpose + ":" + digest(email)
		s.Redis.Set(c.Request.Context(), key, digest(code), 10*time.Minute)
		body := "<p>MiGate 验证码（10 分钟有效）：</p><p>" + html.EscapeString(code) + "</p>"
		if err := s.queueMail(c.Request.Context(), email, "MiGate 账号验证", body); err != nil {
			failure(c, 503, "smtp_unavailable")
			return
		}
	}
	c.JSON(202, gin.H{"status": "if_eligible_email_queued"})
}
func (s *Server) consumeEmail(ctx context.Context, purpose, email, code string) bool {
	key := "migate:email:" + purpose + ":" + digest(strings.ToLower(email))
	script := `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end; return 0`
	n, err := s.Redis.Eval(ctx, script, []string{key}, digest(code)).Int()
	return err == nil && n == 1
}
func (s *Server) register(c *gin.Context) {
	if !s.rate(c, "register", 5) {
		return
	}
	security, policyOK := s.securityPolicy(c)
	if !policyOK {
		return
	}
	if !security.Registration {
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
	if !validEmail(req.Email) || len(req.Password) < 12 || len(req.Password) > 72 {
		failure(c, 400, "invalid_account")
		return
	}
	verified := false
	if security.EmailVerification {
		verified = s.consumeEmail(c.Request.Context(), "verify", req.Email, req.Code)
		if !verified {
			failure(c, 400, "invalid_verification_code")
			return
		}
	}
	u, err := s.newUser(c, req.Email, req.Password, verified)
	if err != nil {
		failure(c, 409, "account_create_failed")
		return
	}
	s.audit(c, "user.register", fmt.Sprint(u.ID), "ok")
	c.JSON(201, gin.H{"created": true, "authorization": "none"})
}
func (s *Server) resetPassword(c *gin.Context) {
	if !s.rate(c, "reset", 5) {
		return
	}
	var req struct {
		Email    string `json:"email"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if !bind(c, &req) {
		return
	}
	if len(req.Password) < 12 || len(req.Password) > 72 {
		failure(c, 400, "invalid_password")
		return
	}
	if !s.consumeEmail(c.Request.Context(), "reset", req.Email, req.Code) {
		failure(c, 400, "invalid_reset_code")
		return
	}
	u, err := s.Store.User.Query().Where(user.EmailEQ(strings.ToLower(req.Email))).Only(c.Request.Context())
	if err != nil {
		failure(c, 400, "invalid_reset_code")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if _, err = s.Store.User.UpdateOne(u).SetPasswordHash(string(hash)).Save(c.Request.Context()); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.revokeAll(c.Request.Context(), u.ID)
	s.audit(c, "auth.password.reset", fmt.Sprint(u.ID), "ok")
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) verifyEmail(c *gin.Context) {
	if !s.rate(c, "email-verify", 10) {
		return
	}
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	if !s.consumeEmail(c.Request.Context(), "verify", req.Email, req.Code) {
		failure(c, 400, "invalid_verification_code")
		return
	}
	n, e := s.Store.User.Update().Where(user.EmailEQ(strings.ToLower(req.Email)), user.EnabledEQ(true)).SetEmailVerified(true).Save(c.Request.Context())
	if e != nil || n != 1 {
		failure(c, 400, "invalid_verification_code")
		return
	}
	s.audit(c, "auth.email.verify", "self", "ok")
	c.JSON(200, gin.H{"verified": true})
}
func text(v any) string { s, _ := v.(string); return s }
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
