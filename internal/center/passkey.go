package center

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/passkey"
	"github.com/shusfun/weir/internal/store/ent/user"
)

type passkeyUser struct {
	account     *ent.User
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(u.account.ID))
	return b
}
func (u passkeyUser) WebAuthnName() string                       { return u.account.Email }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.account.Email }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// webAuthn 的依赖方取当前访问域名；通行密钥与域名绑定，换域名需重新注册。
func (s *Server) webAuthn(r *http.Request) (*webauthn.WebAuthn, error) {
	origin := requestOrigin(r)
	u, err := url.Parse(origin)
	if err != nil {
		return nil, err
	}
	return webauthn.New(&webauthn.Config{RPDisplayName: "Weir", RPID: u.Hostname(), RPOrigins: []string{origin}, AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired}})
}

func (s *Server) passUser(c *gin.Context, u *ent.User) (passkeyUser, error) {
	p := passkeyUser{account: u}
	items, err := s.Store.Ent.Passkey.Query().Where(passkey.UserIDEQ(u.ID)).All(c.Request.Context())
	if err != nil {
		return p, err
	}
	for _, item := range items {
		var credential webauthn.Credential
		if json.Unmarshal(item.Credential, &credential) == nil {
			p.credentials = append(p.credentials, credential)
		}
	}
	return p, nil
}

type passSession struct {
	UserID int                  `json:"user_id"`
	Kind   string               `json:"kind"`
	Data   webauthn.SessionData `json:"data"`
}

func (s *Server) passkeyRoutes(ready, auth *gin.RouterGroup) {
	ready.POST("/auth/passkey/begin", s.passLoginBegin)
	ready.POST("/auth/passkey/finish", s.passLoginFinish)
	auth.POST("/security/passkey/begin", s.passRegisterBegin)
	auth.POST("/security/passkey/finish", s.passRegisterFinish)
	auth.GET("/security/passkeys", s.listPasskeys)
	auth.DELETE("/security/passkeys/:id", s.deletePasskey)
}

func (s *Server) passEnabled(c *gin.Context) bool {
	policy, ok := s.securityPolicy(c)
	if !ok {
		return false
	}
	if !policy.Passkey {
		failure(c, 403, "passkey_disabled")
		return false
	}
	return true
}

func (s *Server) savePassSession(c *gin.Context, id int, kind string, data *webauthn.SessionData, options any) {
	token := randomToken(32)
	b, _ := json.Marshal(passSession{UserID: id, Kind: kind, Data: *data})
	if s.Store.KV.Set(c.Request.Context(), "passkey:"+digest(token), string(b), 5*time.Minute) != nil {
		failure(c, 503, "session_unavailable")
		return
	}
	c.JSON(200, gin.H{"token": token, "options": options})
}

func (s *Server) consumePassSession(c *gin.Context, kind string) (passSession, bool) {
	var ses passSession
	raw, ok, err := s.Store.KV.GetDel(c.Request.Context(), "passkey:"+digest(c.GetHeader("X-Passkey-Token")))
	if err != nil || !ok || json.Unmarshal([]byte(raw), &ses) != nil || ses.Kind != kind {
		failure(c, 401, "passkey_challenge_invalid")
		return ses, false
	}
	return ses, true
}

func (s *Server) passLoginBegin(c *gin.Context) {
	if !s.rate(c, "passkey", 20) || !s.passEnabled(c) {
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if !bind(c, &req) {
		return
	}
	u, err := s.Store.Ent.User.Query().Where(user.EmailEQ(strings.ToLower(strings.TrimSpace(req.Email))), user.EnabledEQ(true)).Only(c.Request.Context())
	if err != nil {
		failure(c, 401, "passkey_unavailable")
		return
	}
	wa, err := s.webAuthn(c.Request)
	if err != nil {
		failure(c, 500, "passkey_configuration_invalid")
		return
	}
	pu, err := s.passUser(c, u)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	options, data, err := wa.BeginLogin(pu)
	if err != nil {
		failure(c, 401, "passkey_unavailable")
		return
	}
	s.savePassSession(c, u.ID, "login", data, options)
}

func (s *Server) passLoginFinish(c *gin.Context) {
	if !s.rate(c, "passkey", 20) || !s.passEnabled(c) {
		return
	}
	ses, ok := s.consumePassSession(c, "login")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	u, err := s.Store.Ent.User.Get(ctx, ses.UserID)
	if err != nil || !u.Enabled {
		failure(c, 401, "account_disabled")
		return
	}
	wa, err := s.webAuthn(c.Request)
	if err != nil {
		failure(c, 500, "passkey_configuration_invalid")
		return
	}
	pu, err := s.passUser(c, u)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	credential, err := wa.FinishLogin(pu, ses.Data, c.Request)
	if err != nil {
		failure(c, 401, "passkey_verification_failed")
		return
	}
	b, _ := json.Marshal(credential)
	// 写回签名计数等变化；凭据必须属于该用户。
	n, err := s.Store.Ent.Passkey.Update().Where(passkey.CredentialIDEQ(digest(string(credential.ID))), passkey.UserIDEQ(u.ID)).SetCredential(b).SetLastUsedAt(time.Now()).Save(ctx)
	if err != nil || n != 1 {
		failure(c, 401, "passkey_not_found")
		return
	}
	s.issueSession(c, u, true)
}

func (s *Server) passRegisterBegin(c *gin.Context) {
	if !s.passEnabled(c) {
		return
	}
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	u := c.MustGet("account").(*ent.User)
	wa, err := s.webAuthn(c.Request)
	if err != nil {
		failure(c, 500, "passkey_configuration_invalid")
		return
	}
	pu, err := s.passUser(c, u)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	options, data, err := wa.BeginRegistration(pu)
	if err != nil {
		failure(c, 400, "passkey_registration_failed")
		return
	}
	s.savePassSession(c, u.ID, "register", data, options)
}

func (s *Server) passRegisterFinish(c *gin.Context) {
	if !s.passEnabled(c) {
		return
	}
	ses, ok := s.consumePassSession(c, "register")
	if !ok {
		return
	}
	if ses.UserID != c.GetInt("user_id") {
		failure(c, 403, "forbidden")
		return
	}
	u := c.MustGet("account").(*ent.User)
	wa, err := s.webAuthn(c.Request)
	if err != nil {
		failure(c, 500, "passkey_configuration_invalid")
		return
	}
	pu, err := s.passUser(c, u)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	credential, err := wa.FinishRegistration(pu, ses.Data, c.Request)
	if err != nil {
		failure(c, 400, "passkey_verification_failed")
		return
	}
	b, _ := json.Marshal(credential)
	if _, err = s.Store.Ent.Passkey.Create().SetUserID(u.ID).SetCredentialID(digest(string(credential.ID))).SetCredential(b).Save(c.Request.Context()); err != nil {
		failure(c, 409, "passkey_already_exists")
		return
	}
	s.audit(c, "auth.passkey.enroll", fmt.Sprint(u.ID), "ok")
	c.JSON(201, gin.H{"created": true})
}

func (s *Server) listPasskeys(c *gin.Context) {
	items, err := s.Store.Ent.Passkey.Query().Where(passkey.UserIDEQ(c.GetInt("user_id"))).Order(ent.Asc(passkey.FieldID)).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, item := range items {
		out = append(out, gin.H{"id": item.ID, "name": item.Name, "created_at": item.CreatedAt, "last_used_at": item.LastUsedAt})
	}
	c.JSON(200, gin.H{"items": out})
}

func (s *Server) deletePasskey(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	uid := c.GetInt("user_id")
	if err != nil {
		failure(c, 400, "invalid_id")
		return
	}
	n, err := s.Store.Ent.Passkey.Delete().Where(passkey.IDEQ(id), passkey.UserIDEQ(uid)).Exec(c.Request.Context())
	if err != nil || n != 1 {
		failure(c, 404, "passkey_not_found")
		return
	}
	_ = s.revokeAll(c.Request.Context(), uid)
	s.audit(c, "auth.passkey.delete", "self", "ok")
	c.JSON(200, gin.H{"ok": true})
}
