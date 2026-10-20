package control

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/ent/user"
)

// User handle and complete credential persistence follow Sub2API passkey.go.
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

// 依赖方取当前访问域名；通行密钥与域名绑定，换域名需重新注册。
func (s *Server) webAuthn(r *http.Request) (*webauthn.WebAuthn, error) {
	origin := requestOrigin(r)
	u, err := url.Parse(origin)
	if err != nil {
		return nil, err
	}
	return webauthn.New(&webauthn.Config{RPDisplayName: "MiGate", RPID: u.Hostname(), RPOrigins: []string{origin}, AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired}})
}
func (s *Server) passUser(c *gin.Context, u *ent.User) passkeyUser {
	p := passkeyUser{account: u}
	items, _ := s.Store.Resource.Query().Where(resource.KindEQ("passkey"), resource.OwnerIDEQ(u.ID)).All(c.Request.Context())
	for _, r := range items {
		b, _ := json.Marshal(r.Data)
		var credential webauthn.Credential
		if json.Unmarshal(b, &credential) == nil {
			p.credentials = append(p.credentials, credential)
		}
	}
	return p
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
	auth.GET("/security/passkeys", func(c *gin.Context) {
		items, err := s.Store.Resource.Query().Where(resource.KindEQ("passkey"), resource.OwnerIDEQ(c.GetInt("user_id"))).All(c.Request.Context())
		if err != nil {
			failure(c, 503, "query_failed")
			return
		}
		out := []gin.H{}
		for _, item := range items {
			out = append(out, gin.H{"id": item.ID, "created_at": item.CreatedAt})
		}
		c.JSON(200, gin.H{"items": out})
	})
	auth.DELETE("/security/passkeys/:id", func(c *gin.Context) {
		var req struct {
			Password string `json:"password"`
			Code     string `json:"code"`
		}
		if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
			return
		}
		id, _ := strconv.Atoi(c.Param("id"))
		n, e := s.Store.Resource.Delete().Where(resource.IDEQ(id), resource.KindEQ("passkey"), resource.OwnerIDEQ(c.GetInt("user_id"))).Exec(c.Request.Context())
		if e != nil || n != 1 {
			failure(c, 404, "passkey_not_found")
			return
		}
		s.revokeAll(c.Request.Context(), c.GetInt("user_id"))
		s.audit(c, "auth.passkey.delete", "self", "ok")
		c.JSON(200, gin.H{"ok": true})
	})
}
func (s *Server) passEnabled(c *gin.Context) bool {
	sec, policyOK := s.securityPolicy(c)
	if !policyOK {
		return false
	}
	if !sec.Passkey {
		failure(c, 403, "passkey_disabled")
		return false
	}
	return true
}
func (s *Server) savePassSession(c *gin.Context, id int, kind string, data *webauthn.SessionData, options any) {
	token := randomToken(32)
	b, _ := json.Marshal(passSession{UserID: id, Kind: kind, Data: *data})
	if s.Redis.Set(c.Request.Context(), "migate:passkey:"+digest(token), b, 5*time.Minute).Err() != nil {
		failure(c, 503, "session_unavailable")
		return
	}
	c.JSON(200, gin.H{"token": token, "options": options})
}
func (s *Server) consumePassSession(c *gin.Context, kind string) (passSession, bool) {
	var ses passSession
	b, err := s.Redis.GetDel(c.Request.Context(), "migate:passkey:"+digest(c.GetHeader("X-Passkey-Token"))).Bytes()
	if err != nil || json.Unmarshal(b, &ses) != nil || ses.Kind != kind {
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
	u, err := s.Store.User.Query().Where(user.EmailEQ(req.Email), user.EnabledEQ(true)).Only(c.Request.Context())
	if err != nil {
		failure(c, 401, "passkey_unavailable")
		return
	}
	wa, err := s.webAuthn(c.Request)
	if err != nil {
		failure(c, 500, "passkey_configuration_invalid")
		return
	}
	options, data, err := wa.BeginLogin(s.passUser(c, u))
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
	u, err := s.Store.User.Get(c.Request.Context(), ses.UserID)
	if err != nil || !u.Enabled {
		failure(c, 401, "account_disabled")
		return
	}
	security, policyOK := s.securityPolicy(c)
	if !policyOK {
		return
	}
	if security.EmailVerification && !u.EmailVerified {
		failure(c, 403, "email_verification_required")
		return
	}
	wa, err := s.webAuthn(c.Request)
	if err != nil {
		failure(c, 500, "passkey_configuration_invalid")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	credential, err := wa.FinishLogin(s.passUser(c, u), ses.Data, c.Request)
	if err != nil {
		failure(c, 401, "passkey_verification_failed")
		return
	}
	key := digest(string(credential.ID))
	r, err := s.Store.Resource.Query().Where(resource.KindEQ("passkey"), resource.KeyEQ(key), resource.OwnerIDEQ(u.ID)).Only(c.Request.Context())
	if err != nil {
		failure(c, 401, "passkey_not_found")
		return
	}
	b, _ := json.Marshal(credential)
	var data map[string]any
	json.Unmarshal(b, &data)
	if _, err = s.Store.Resource.UpdateOne(r).SetData(data).Save(c.Request.Context()); err != nil {
		failure(c, 500, "save_failed")
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
	options, data, err := wa.BeginRegistration(s.passUser(c, u))
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	credential, err := wa.FinishRegistration(s.passUser(c, u), ses.Data, c.Request)
	if err != nil {
		failure(c, 400, "passkey_verification_failed")
		return
	}
	b, _ := json.Marshal(credential)
	var data map[string]any
	json.Unmarshal(b, &data)
	_, err = s.Store.Resource.Create().SetKind("passkey").SetKey(digest(string(credential.ID))).SetOwnerID(u.ID).SetData(data).Save(c.Request.Context())
	if err != nil {
		failure(c, 409, "passkey_already_exists")
		return
	}
	s.audit(c, "auth.passkey.enroll", fmt.Sprint(u.ID), "ok")
	c.JSON(201, gin.H{"created": true})
}
