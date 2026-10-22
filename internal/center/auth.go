package center

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"github.com/imzyb/MiGate/internal/store/ent"
	"github.com/imzyb/MiGate/internal/store/ent/recoverycode"
	"github.com/imzyb/MiGate/internal/store/ent/setting"
	"github.com/imzyb/MiGate/internal/store/ent/user"
)

type entUser = ent.User

const (
	sessionCookie = "migate_session"
	sessionTTL    = 24 * time.Hour
)

// SecuritySettings 是登录安全策略；未保存时使用默认值（允许 TOTP 与通行密钥）。
type SecuritySettings struct {
	TOTP            bool `json:"totp_enabled"`
	Passkey         bool `json:"passkey_enabled"`
	RequireAdminMFA bool `json:"require_admin_mfa"`
}

func defaultSecurity() SecuritySettings { return SecuritySettings{TOTP: true, Passkey: true} }

// Session 存在 KV 的 session:<用户 ID>:<令牌摘要> 键下，按用户前缀即可列出或撤销。
type Session struct {
	UserID    int       `json:"user_id"`
	MFA       bool      `json:"mfa"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IP        string    `json:"ip"`
}

var nonexistentAccountHash, _ = bcrypt.GenerateFromPassword([]byte("migate-nonexistent-account"), bcrypt.DefaultCost)

func sessionPrefix(uid int) string           { return fmt.Sprintf("session:%d:", uid) }
func sessionKey(uid int, token string) string { return sessionPrefix(uid) + digest(token) }

// 读取或解析失败时拒绝（503），不能把策略缺失当作「关闭 MFA」。
func (s *Server) securityPolicy(c *gin.Context) (SecuritySettings, bool) {
	row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ("security")).Only(c.Request.Context())
	if ent.IsNotFound(err) {
		return defaultSecurity(), true
	}
	policy := defaultSecurity()
	if err == nil {
		var b []byte
		if b, err = json.Marshal(row.Value); err == nil {
			err = json.Unmarshal(b, &policy)
		}
	}
	if err != nil {
		failure(c, 503, "security_policy_unavailable")
		return SecuritySettings{}, false
	}
	return policy, true
}

func (s *Server) rate(c *gin.Context, kind string, limit int64) bool {
	n, err := s.Store.KV.Incr(c.Request.Context(), "limit:"+kind+":"+digest(c.ClientIP()), 10*time.Minute)
	if err != nil {
		failure(c, 503, "rate_limit_unavailable")
		return false
	}
	if n > limit {
		if n == limit+1 {
			s.securityEvent(c, "rate-limit", kind, "warning")
		}
		failure(c, 429, "rate_limited")
		return false
	}
	return true
}

// securityEvent 先写入审计（action 以 security. 开头）；独立的安全事件与 AI 分类在后续阶段接入。
func (s *Server) securityEvent(c *gin.Context, kind, scope, severity string) {
	s.audit(c, "security."+kind, scope, severity)
}

func (s *Server) failedLoginEvent(c *gin.Context) {
	if n, err := s.Store.KV.Incr(c.Request.Context(), "failed-login:"+digest(c.ClientIP()), 10*time.Minute); err == nil && n == 5 {
		s.securityEvent(c, "repeated-login-failure", "authentication", "warning")
	}
}

func userView(u *ent.User) gin.H {
	return gin.H{
		"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role, "enabled": u.Enabled,
		"quota": u.Quota, "used": u.Used, "expires_at": u.ExpiresAt, "monthly_reset": u.MonthlyReset,
		"all_nodes": u.AllNodes, "allow_chain": u.AllowChain, "totp_enrolled": u.TotpSecret != "",
		"email_verified": true, "created_at": u.CreatedAt,
	}
}

func (s *Server) publicSettings(c *gin.Context) {
	p, ok := s.securityPolicy(c)
	if !ok {
		return
	}
	// 自助注册与邮箱验证依赖邮件服务，后续阶段接入前固定关闭。
	c.JSON(200, gin.H{"registration_enabled": false, "email_verification": false, "totp_enabled": p.TOTP, "passkey_enabled": p.Passkey})
}

func (s *Server) storeSession(c *gin.Context, u *ent.User, mfa bool) error {
	token := randomToken(32)
	now := time.Now().UTC()
	b, _ := json.Marshal(Session{UserID: u.ID, MFA: mfa, CreatedAt: now, ExpiresAt: now.Add(sessionTTL), IP: c.ClientIP()})
	if err := s.Store.KV.Set(c.Request.Context(), sessionKey(u.ID, token), string(b), sessionTTL); err != nil {
		return err
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookie, fmt.Sprintf("%d.%s", u.ID, token), int(sessionTTL.Seconds()), "/", "", requestSecure(c.Request), true)
	c.Set("user_id", u.ID)
	return nil
}

func (s *Server) issueSession(c *gin.Context, u *ent.User, mfa bool) {
	if err := s.storeSession(c, u, mfa); err != nil {
		failure(c, 503, "session_unavailable")
		return
	}
	s.audit(c, "auth.login", fmt.Sprint(u.ID), "ok")
	c.JSON(200, gin.H{"user": userView(u), "mfa": mfa})
}

func parseSessionCookie(v string) (int, string, bool) {
	id, token, ok := strings.Cut(v, ".")
	uid, err := strconv.Atoi(id)
	if !ok || err != nil || uid <= 0 || len(token) != 64 {
		return 0, "", false
	}
	return uid, token, true
}

func (s *Server) login(c *gin.Context) {
	if !s.rate(c, "login", 20) {
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !bind(c, &req) {
		return
	}
	u, err := s.Store.Ent.User.Query().Where(user.EmailEQ(strings.ToLower(strings.TrimSpace(req.Email)))).Only(c.Request.Context())
	hash := nonexistentAccountHash
	if err == nil {
		hash = []byte(u.PasswordHash)
	}
	passwordErr := bcrypt.CompareHashAndPassword(hash, []byte(req.Password))
	if err != nil || !u.Enabled || passwordErr != nil {
		s.audit(c, "auth.login", "", "denied")
		s.failedLoginEvent(c)
		failure(c, 401, "invalid_credentials")
		return
	}
	if _, ok := s.securityPolicy(c); !ok {
		return
	}
	if u.TotpSecret != "" {
		challenge := randomToken(32)
		if err = s.Store.KV.Set(c.Request.Context(), "mfa:"+digest(challenge), strconv.Itoa(u.ID), 5*time.Minute); err != nil {
			failure(c, 503, "session_unavailable")
			return
		}
		c.JSON(200, gin.H{"mfa_required": true, "challenge": challenge})
		return
	}
	s.issueSession(c, u, false)
}

// verifyTOTP 接受前后各一个时间片；同一时间片不可重放。也接受一次性恢复码。
func (s *Server) verifyTOTP(ctx context.Context, u *ent.User, code string) bool {
	if secret, err := s.Crypto.Decrypt(u.TotpSecret); err == nil {
		now := time.Now().Unix() / 30
		for _, step := range []int64{now - 1, now, now + 1} {
			if step <= u.TotpStep {
				continue
			}
			expected, err := totp.GenerateCodeCustom(secret, time.Unix(step*30, 0), totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
			if err == nil && subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
				n, e := s.Store.Ent.User.Update().Where(user.IDEQ(u.ID), user.TotpStepLT(step)).SetTotpStep(step).Save(ctx)
				return e == nil && n == 1
			}
		}
	}
	n, err := s.Store.Ent.RecoveryCode.Update().Where(recoverycode.UserIDEQ(u.ID), recoverycode.DigestEQ(digest(code)), recoverycode.UsedAtIsNil()).SetUsedAt(time.Now()).Save(ctx)
	return err == nil && n == 1
}

func (s *Server) completeMFA(c *gin.Context) {
	if !s.rate(c, "mfa", 15) {
		return
	}
	var req struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	key := "mfa:" + digest(req.Challenge)
	raw, ok, err := s.Store.KV.Get(ctx, key)
	id, convErr := strconv.Atoi(raw)
	if err != nil || !ok || convErr != nil {
		failure(c, 401, "challenge_expired")
		return
	}
	u, err := s.Store.Ent.User.Get(ctx, id)
	if err != nil || !u.Enabled || !s.verifyTOTP(ctx, u, req.Code) {
		failure(c, 401, "invalid_second_factor")
		return
	}
	if _, consumed, _ := s.Store.KV.GetDel(ctx, key); !consumed {
		failure(c, 401, "challenge_used")
		return
	}
	s.issueSession(c, u, true)
}

func (s *Server) authenticate(c *gin.Context) {
	raw, err := c.Cookie(sessionCookie)
	uid, token, ok := parseSessionCookie(raw)
	if err != nil || !ok {
		failure(c, 401, "unauthorized")
		return
	}
	key := sessionKey(uid, token)
	b, found, err := s.Store.KV.Get(c.Request.Context(), key)
	var session Session
	if err != nil || !found || json.Unmarshal([]byte(b), &session) != nil || session.UserID != uid {
		failure(c, 401, "session_expired")
		return
	}
	u, err := s.Store.Ent.User.Get(c.Request.Context(), uid)
	if err != nil || !u.Enabled {
		failure(c, 401, "account_disabled")
		return
	}
	c.Set("user_id", u.ID)
	c.Set("account", u)
	c.Set("session_key", key)
	c.Set("session", session)
	policy, ok := s.securityPolicy(c)
	if !ok {
		return
	}
	if policy.RequireAdminMFA && u.Role == user.RoleAdmin && !session.MFA {
		path := c.Request.URL.Path
		if path != "/api/v1/me" && !strings.HasPrefix(path, "/api/v1/security/") && path != "/api/v1/auth/logout" {
			failure(c, 403, "mfa_enrollment_or_login_required")
			return
		}
	}
	c.Next()
}

// requireAdmin 用于管理员接口；后端强制校验角色，不依赖前端隐藏菜单。
func requireAdmin(c *gin.Context) {
	if u, ok := c.MustGet("account").(*ent.User); !ok || u.Role != user.RoleAdmin {
		failure(c, 403, "forbidden")
		return
	}
	c.Next()
}

func (s *Server) me(c *gin.Context) { c.JSON(200, userView(c.MustGet("account").(*ent.User))) }

func (s *Server) logout(c *gin.Context) {
	_ = s.Store.KV.Del(c.Request.Context(), c.GetString("session_key"))
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookie, "", -1, "/", "", requestSecure(c.Request), true)
	c.JSON(200, gin.H{"ok": true})
}

// revokeAll 撤销某用户的全部会话（改密码、改二次验证、管理员停用时调用）。
func (s *Server) revokeAll(ctx context.Context, uid int) error {
	keys, err := s.Store.KV.Keys(ctx, sessionPrefix(uid), 5000)
	if err != nil {
		return err
	}
	return s.Store.KV.Del(ctx, keys...)
}

func (s *Server) reauth(c *gin.Context, password, code string) bool {
	u := c.MustGet("account").(*ent.User)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		failure(c, 401, "password_required")
		return false
	}
	if u.TotpSecret != "" && !s.verifyTOTP(c.Request.Context(), u, code) {
		failure(c, 401, "second_factor_required")
		return false
	}
	return true
}

func (s *Server) setupTOTP(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	policy, ok := s.securityPolicy(c)
	if !ok {
		return
	}
	if !policy.TOTP {
		failure(c, 403, "totp_disabled")
		return
	}
	u := c.MustGet("account").(*ent.User)
	if u.TotpSecret != "" {
		failure(c, 409, "already_enrolled")
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "MiGate", AccountName: u.Email})
	if err != nil {
		failure(c, 500, "key_generation_failed")
		return
	}
	sealed, err := s.Crypto.Encrypt(key.Secret())
	if err != nil {
		failure(c, 500, "key_generation_failed")
		return
	}
	if err = s.Store.KV.Set(c.Request.Context(), fmt.Sprintf("totp-setup:%d", u.ID), sealed, 5*time.Minute); err != nil {
		failure(c, 503, "setup_unavailable")
		return
	}
	c.JSON(200, gin.H{"secret": key.Secret(), "url": key.URL()})
}

func (s *Server) confirmTOTP(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	u := c.MustGet("account").(*ent.User)
	ctx := c.Request.Context()
	key := fmt.Sprintf("totp-setup:%d", u.ID)
	sealed, ok, err := s.Store.KV.Get(ctx, key)
	plain, decErr := s.Crypto.Decrypt(sealed)
	if err != nil || !ok || decErr != nil || !totp.Validate(req.Code, plain) {
		failure(c, 400, "invalid_code")
		return
	}
	codes, err := s.enrollTOTP(ctx, u.ID, sealed)
	if errors.Is(err, errAlreadyEnrolled) {
		failure(c, 409, "already_enrolled")
		return
	}
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	_ = s.Store.KV.Del(ctx, key)
	s.audit(c, "auth.totp.enroll", "self", "ok")
	_ = s.revokeAll(ctx, u.ID)
	c.JSON(200, gin.H{"recovery_codes": codes, "login_required": true})
}

var errAlreadyEnrolled = errors.New("already enrolled")

// enrollTOTP 用条件更新串行化并发确认，并在同一事务里生成 8 个恢复码（只存摘要）。
func (s *Server) enrollTOTP(ctx context.Context, uid int, sealed string) ([]string, error) {
	tx, err := s.Store.Ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	n, err := tx.User.Update().Where(user.IDEQ(uid), user.TotpSecretEQ("")).SetTotpSecret(sealed).SetTotpStep(time.Now().Unix() / 30).Save(ctx)
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, errAlreadyEnrolled
	}
	if _, err = tx.RecoveryCode.Delete().Where(recoverycode.UserIDEQ(uid)).Exec(ctx); err != nil {
		return nil, err
	}
	codes := make([]string, 0, 8)
	for i := 0; i < 8; i++ {
		code := randomToken(8)
		if _, err = tx.RecoveryCode.Create().SetUserID(uid).SetDigest(digest(code)).Save(ctx); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, tx.Commit()
}

func (s *Server) disableTOTP(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	ctx := c.Request.Context()
	id := c.GetInt("user_id")
	if _, err := s.Store.Ent.User.UpdateOneID(id).SetTotpSecret("").SetTotpStep(0).Save(ctx); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	_, _ = s.Store.Ent.RecoveryCode.Delete().Where(recoverycode.UserIDEQ(id)).Exec(ctx)
	_ = s.revokeAll(ctx, id)
	s.audit(c, "auth.totp.disable", "self", "ok")
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) changePassword(c *gin.Context) {
	var req struct {
		Password    string `json:"password"`
		Code        string `json:"code"`
		NewPassword string `json:"new_password"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	if !validAccountPassword(req.NewPassword) {
		failure(c, 400, "password_policy")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	id := c.GetInt("user_id")
	if _, err = s.Store.Ent.User.UpdateOneID(id).SetPasswordHash(string(hash)).Save(c.Request.Context()); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	_ = s.revokeAll(c.Request.Context(), id)
	s.audit(c, "auth.password.change", "self", "ok")
	c.JSON(200, gin.H{"ok": true})
}
