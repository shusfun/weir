package control

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/ent/user"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type SecuritySettings struct {
	Registration      bool `json:"registration_enabled"`
	EmailVerification bool `json:"email_verification"`
	TOTP              bool `json:"totp_enabled"`
	Passkey           bool `json:"passkey_enabled"`
	RequireAdminMFA   bool `json:"require_admin_mfa"`
}
type Session struct {
	UserID    int       `json:"user_id"`
	MFA       bool      `json:"mfa"`
	CreatedAt time.Time `json:"created_at"`
	IP        string    `json:"ip"`
}

var nonexistentAccountHash, _ = bcrypt.GenerateFromPassword([]byte("migate-nonexistent-account"), bcrypt.DefaultCost)

func (s *Server) rate(c *gin.Context, kind string, limit int) bool {
	key := "migate:limit:" + kind + ":" + digest(c.ClientIP())
	script := redis.NewScript(`local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('EXPIRE',KEYS[1],ARGV[1]) end;return n`)
	n, err := script.Run(c.Request.Context(), s.Redis, []string{key}, 600).Int()
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
func (s *Server) issueSession(c *gin.Context, u *ent.User, mfa bool) {
	token := randomToken(32)
	sesh := Session{UserID: u.ID, MFA: mfa, CreatedAt: time.Now(), IP: c.ClientIP()}
	b, _ := json.Marshal(sesh)
	key := "migate:session:" + digest(token)
	if err := s.Redis.Set(c.Request.Context(), key, b, 24*time.Hour).Err(); err != nil {
		failure(c, 503, "session_unavailable")
		return
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("migate_session", token, 86400, "/", "", strings.HasPrefix(s.Config.PublicURL, "https://"), true)
	c.Set("user_id", u.ID)
	s.audit(c, "auth.login", fmt.Sprint(u.ID), "ok")
	c.JSON(200, gin.H{"user": userView(u), "mfa": mfa})
}
func userView(u *ent.User) gin.H {
	return gin.H{"id": u.ID, "email": u.Email, "role": u.Role, "enabled": u.Enabled, "quota": u.Quota, "used": u.Used, "expires": u.Expires, "email_verified": u.EmailVerified, "totp_enrolled": u.TotpSecret != "", "created_at": u.CreatedAt}
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
	u, err := s.Store.User.Query().Where(user.EmailEQ(strings.ToLower(req.Email))).Only(c.Request.Context())
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
	settings, policyOK := s.securityPolicy(c)
	if !policyOK {
		return
	}
	if settings.EmailVerification && !u.EmailVerified {
		failure(c, 403, "email_verification_required")
		return
	}
	if u.TotpSecret != "" {
		challenge := randomToken(32)
		s.Redis.Set(c.Request.Context(), "migate:mfa:"+digest(challenge), u.ID, 5*time.Minute)
		c.JSON(200, gin.H{"mfa_required": true, "challenge": challenge})
		return
	}
	s.issueSession(c, u, false)
}
func (s *Server) verifyTOTP(ctx context.Context, u *ent.User, code string) bool {
	secret, err := s.Crypto.Decrypt(u.TotpSecret)
	if err != nil {
		return false
	}
	now := time.Now()
	for _, offset := range []int64{-1, 0, 1} {
		tick := now.Unix()/30 + offset
		if tick <= u.TotpTick {
			continue
		}
		expected, err := totp.GenerateCodeCustom(secret, time.Unix(tick*30, 0), totp.ValidateOpts{Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			n, e := s.Store.User.Update().Where(user.IDEQ(u.ID), user.TotpTickLT(tick)).SetTotpTick(tick).Save(ctx)
			return e == nil && n == 1
		}
	}
	r, err := s.Store.Resource.Query().Where(resource.KindEQ("recovery"), resource.OwnerIDEQ(u.ID), resource.KeyEQ(digest(code))).Only(ctx)
	if err == nil {
		return s.Store.Resource.DeleteOneID(r.ID).Exec(ctx) == nil
	}
	return false
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
	id, err := s.Redis.Get(c.Request.Context(), "migate:mfa:"+digest(req.Challenge)).Int()
	if err != nil {
		failure(c, 401, "challenge_expired")
		return
	}
	u, err := s.Store.User.Get(c.Request.Context(), id)
	if err != nil || !u.Enabled || !s.verifyTOTP(c.Request.Context(), u, req.Code) {
		failure(c, 401, "invalid_second_factor")
		return
	}
	n, _ := s.Redis.Del(c.Request.Context(), "migate:mfa:"+digest(req.Challenge)).Result()
	if n != 1 {
		failure(c, 401, "challenge_used")
		return
	}
	s.issueSession(c, u, true)
}
func (s *Server) authenticate(c *gin.Context) {
	token, err := c.Cookie("migate_session")
	if err != nil {
		failure(c, 401, "unauthorized")
		return
	}
	key := "migate:session:" + digest(token)
	b, err := s.Redis.Get(c.Request.Context(), key).Bytes()
	var session Session
	if err != nil || json.Unmarshal(b, &session) != nil {
		failure(c, 401, "session_expired")
		return
	}
	u, err := s.Store.User.Get(c.Request.Context(), session.UserID)
	if err != nil || !u.Enabled {
		failure(c, 401, "account_disabled")
		return
	}
	c.Set("user_id", u.ID)
	c.Set("role", u.Role)
	c.Set("account", u)
	c.Set("session_key", key)
	c.Set("session", session)
	settings, policyOK := s.securityPolicy(c)
	if !policyOK {
		return
	}
	if settings.RequireAdminMFA && u.Role == "admin" && !session.MFA {
		path := c.Request.URL.Path
		if path != "/api/v1/me" && !strings.HasPrefix(path, "/api/v1/security/") && path != "/api/v1/auth/logout" {
			failure(c, 403, "mfa_enrollment_or_login_required")
			return
		}
	}
	if settings.EmailVerification && !u.EmailVerified && c.Request.URL.Path != "/api/v1/me" && c.Request.URL.Path != "/api/v1/auth/logout" {
		failure(c, 403, "email_verification_required")
		return
	}
	c.Next()
}
func (s *Server) me(c *gin.Context) { u := c.MustGet("account").(*ent.User); c.JSON(200, userView(u)) }
func (s *Server) logout(c *gin.Context) {
	s.Redis.Del(c.Request.Context(), c.GetString("session_key"))
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("migate_session", "", -1, "/", "", strings.HasPrefix(s.Config.PublicURL, "https://"), true)
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) sessions(c *gin.Context) { s.listSessions(c, c.GetInt("user_id")) }
func (s *Server) revokeSession(c *gin.Context) {
	token := c.Param("id")
	if !validSessionID(token) {
		failure(c, 400, "invalid_session")
		return
	}
	n, err := revokeOwnedSession.Run(c.Request.Context(), s.Redis, []string{"migate:session:" + token}, c.GetInt("user_id")).Int()
	if err != nil {
		failure(c, 503, "session_unavailable")
		return
	}
	if n != 1 {
		failure(c, 404, "session_not_found")
		return
	}
	s.audit(c, "auth.session.revoke", "self", "ok")
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) revokeAll(ctx context.Context, id int) {
	iter := s.Redis.Scan(ctx, 0, "migate:session:*", 100).Iterator()
	for iter.Next(ctx) {
		var ses Session
		b, err := s.Redis.Get(ctx, iter.Val()).Bytes()
		if err == nil && json.Unmarshal(b, &ses) == nil && ses.UserID == id {
			s.Redis.Del(ctx, iter.Val())
		}
	}
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
	settings, policyOK := s.securityPolicy(c)
	if !policyOK {
		return
	}
	if !settings.TOTP {
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
	if err = s.Redis.Set(c.Request.Context(), fmt.Sprintf("migate:totp-setup:%d", u.ID), sealed, 5*time.Minute).Err(); err != nil {
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
	key := fmt.Sprintf("migate:totp-setup:%d", u.ID)
	sealed, err := s.Redis.Get(c.Request.Context(), key).Result()
	plain, e := s.Crypto.Decrypt(sealed)
	if err != nil || e != nil || !totp.Validate(req.Code, plain) {
		failure(c, 400, "invalid_code")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	defer tx.Rollback()
	// A conditional update serializes concurrent confirmations, including requests
	// authenticated before another request committed the enrollment.
	n, err := tx.User.Update().Where(user.IDEQ(u.ID), user.TotpSecretEQ("")).SetTotpSecret(sealed).SetTotpTick(time.Now().Unix() / 30).Save(ctx)
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	if n != 1 {
		failure(c, 409, "already_enrolled")
		return
	}
	codes := []string{}
	for i := 0; i < 8; i++ {
		code := randomToken(8)
		if _, err = tx.Resource.Create().SetKind("recovery").SetKey(digest(code)).SetOwnerID(u.ID).SetData(map[string]any{}).Save(ctx); err != nil {
			failure(c, 500, "save_failed")
			return
		}
		codes = append(codes, code)
	}
	if err = tx.Commit(); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.Redis.Del(ctx, key)
	s.audit(c, "auth.totp.enroll", "self", "ok")
	s.revokeAll(c.Request.Context(), u.ID)
	c.JSON(200, gin.H{"recovery_codes": codes, "login_required": true})
}
func (s *Server) disableTOTP(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	id := c.GetInt("user_id")
	_, err := s.Store.User.UpdateOneID(id).SetTotpSecret("").Save(c.Request.Context())
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.Store.Resource.Delete().Where(resource.KindEQ("recovery"), resource.OwnerIDEQ(id)).Exec(c.Request.Context())
	s.revokeAll(c.Request.Context(), id)
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
	if len(req.NewPassword) < 12 || len(req.NewPassword) > 72 {
		failure(c, 400, "password_length_12_to_72")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if _, err := s.Store.User.UpdateOneID(c.GetInt("user_id")).SetPasswordHash(string(hash)).Save(c.Request.Context()); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.revokeAll(c.Request.Context(), c.GetInt("user_id"))
	s.audit(c, "auth.password.change", "self", "ok")
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) listUsers(c *gin.Context) {
	query := s.Store.User.Query()
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		if len(q) > 200 {
			failure(c, 400, "invalid_query")
			return
		}
		query.Where(user.EmailContainsFold(q))
	}
	switch c.Query("status") {
	case "enabled":
		query.Where(user.EnabledEQ(true))
	case "disabled":
		query.Where(user.EnabledEQ(false))
	case "", "all":
	default:
		failure(c, 400, "invalid_query")
		return
	}
	page, size := 1, 200
	paged := c.Query("page") != "" || c.Query("page_size") != ""
	if paged {
		var err error
		page, err = strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 1000000 {
			failure(c, 400, "invalid_page")
			return
		}
		size, err = strconv.Atoi(c.DefaultQuery("page_size", "20"))
		if err != nil || size < 1 || size > 200 {
			failure(c, 400, "invalid_page_size")
			return
		}
	}
	total, err := query.Clone().Count(c.Request.Context())
	if err != nil {
		failure(c, 500, "query_failed")
		return
	}
	items, err := query.Order(ent.Desc("id")).Offset((page - 1) * size).Limit(size).All(c.Request.Context())
	if err != nil {
		failure(c, 500, "query_failed")
		return
	}
	out := []gin.H{}
	for _, u := range items {
		out = append(out, userView(u))
	}
	c.JSON(200, gin.H{"items": out, "total": total, "page": page, "page_size": size})
}
func (s *Server) getUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		failure(c, 400, "invalid_id")
		return
	}
	u, err := s.Store.User.Get(c.Request.Context(), id)
	if ent.IsNotFound(err) {
		failure(c, 404, "not_found")
		return
	}
	if err != nil {
		failure(c, 500, "query_failed")
		return
	}
	c.JSON(200, userView(u))
}
func (s *Server) newUser(c *gin.Context, email, password string, verified bool) (*ent.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return s.Store.User.Create().SetEmail(strings.ToLower(email)).SetPasswordHash(string(hash)).SetEmailVerified(verified).SetSubscriptionToken(randomToken(32)).Save(c.Request.Context())
}
func (s *Server) createUser(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Quota    int64  `json:"quota"`
		Expires  int64  `json:"expires"`
	}
	if !bind(c, &req) {
		return
	}
	if !validEmail(req.Email) || len(req.Password) < 12 || len(req.Password) > 72 || req.Quota < 0 {
		failure(c, 400, "invalid_user")
		return
	}
	u, err := s.newUser(c, req.Email, req.Password, false)
	if err != nil {
		failure(c, 409, "user_create_failed")
		return
	}
	u, err = s.Store.User.UpdateOne(u).SetQuota(req.Quota).SetExpires(req.Expires).Save(c.Request.Context())
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "user.create", fmt.Sprint(u.ID), "ok")
	c.JSON(201, userView(u))
}
func (s *Server) updateUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		failure(c, 400, "invalid_id")
		return
	}
	u, err := s.Store.User.Get(c.Request.Context(), id)
	if err != nil {
		failure(c, 404, "not_found")
		return
	}
	var req struct {
		Enabled *bool  `json:"enabled"`
		Quota   *int64 `json:"quota"`
		Expires *int64 `json:"expires"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Quota != nil && *req.Quota < 0 {
		failure(c, 400, "invalid_quota")
		return
	}
	if u.Role == "admin" && req.Enabled != nil && !*req.Enabled {
		failure(c, 409, "administrator_disable_not_supported")
		return
	}
	update := s.Store.User.UpdateOneID(id)
	if req.Enabled != nil {
		update.SetEnabled(*req.Enabled)
	}
	if req.Quota != nil {
		update.SetQuota(*req.Quota)
	}
	if req.Expires != nil {
		update.SetExpires(*req.Expires)
	}
	u, err = update.Save(c.Request.Context())
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.revokeAll(c.Request.Context(), id)
	s.syncUserGrants(c.Request.Context(), u)
	s.audit(c, "user.update", fmt.Sprint(id), "ok")
	c.JSON(200, userView(u))
}
