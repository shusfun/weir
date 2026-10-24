package center

import (
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/packages/contracts"
)

// 一次性预批准配对凭证：管理员「添加 Gate」生成（KV，1 小时），或单机安装时写入的本机摘要文件。
type pairInvite struct {
	Name string `json:"name"`
}

func (s *Server) localPairFile() string {
	return filepath.Join(filepath.Dir(s.Path), "local-pair.sha256")
}

// consumePairToken 校验并销毁凭证；返回预设名称。本机凭证只接受直接来自回环、未经反代的请求。
func (s *Server) consumePairToken(c *gin.Context, token string) (string, bool) {
	if len(token) < 32 {
		return "", false
	}
	if raw, ok, err := s.Store.KV.GetDel(c.Request.Context(), "pair:"+digest(token)); err == nil && ok {
		var inv pairInvite
		_ = json.Unmarshal([]byte(raw), &inv)
		return inv.Name, true
	}
	if !directLoopback(c) {
		return "", false
	}
	raw, err := os.ReadFile(s.localPairFile())
	expected := strings.ToLower(strings.TrimSpace(string(raw)))
	if err != nil || len(expected) != 64 || subtle.ConstantTimeCompare([]byte(expected), []byte(digest(token))) != 1 {
		return "", false
	}
	// 先删除摘要再继续：并发的第二个请求读不到摘要，凭证不可重放。
	if os.Remove(s.localPairFile()) != nil {
		return "", false
	}
	return "本机", true
}

func (s *Server) agentPair(c *gin.Context) {
	if !s.rate(c, "gate-pair", 20) {
		return
	}
	var req contracts.PairRequest
	if !bind(c, &req) {
		return
	}
	pub, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize || len(req.Name) > 100 {
		failure(c, 400, "invalid_device")
		return
	}
	preset, ok := s.consumePairToken(c, req.Token)
	if !ok {
		s.securityEvent(c, "gate-pair-rejected", "gate", "warning")
		failure(c, 403, "pair_token_invalid")
		return
	}
	name := strings.TrimSpace(req.Name)
	if preset != "" {
		name = preset
	}
	if name == "" {
		name = "Gate"
	}
	g, err := s.Store.Ent.Gate.Create().SetName(name).SetPublicKey(req.PublicKey).Save(c.Request.Context())
	if err != nil {
		failure(c, 409, "device_already_registered")
		return
	}
	s.audit(c, "gate.pair", strconv.Itoa(g.ID), "ok")
	c.JSON(201, contracts.PairResult{GateID: int64(g.ID)})
}

// agentChallenge / agentAuth：Gate 用设备私钥签名一次性挑战，换取 15 分钟访问令牌。
func (s *Server) agentChallenge(c *gin.Context) {
	if !s.rate(c, "gate-auth", 240) {
		return
	}
	var req struct {
		GateID int `json:"gate_id"`
	}
	if !bind(c, &req) {
		return
	}
	g, err := s.Store.Ent.Gate.Get(c.Request.Context(), req.GateID)
	if err != nil || g.Status != gate.StatusActive {
		failure(c, 403, "device_not_active")
		return
	}
	challenge := randomToken(32)
	if s.Store.KV.Set(c.Request.Context(), "gate-challenge:"+strconv.Itoa(g.ID)+":"+challenge, "1", time.Minute) != nil {
		failure(c, 503, "auth_unavailable")
		return
	}
	c.JSON(200, gin.H{"challenge": challenge})
}

func (s *Server) agentAuthenticate(c *gin.Context) {
	var req struct {
		GateID    int    `json:"gate_id"`
		Challenge string `json:"challenge"`
		Signature string `json:"signature"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	if _, ok, err := s.Store.KV.GetDel(ctx, "gate-challenge:"+strconv.Itoa(req.GateID)+":"+req.Challenge); err != nil || !ok {
		failure(c, 401, "challenge_invalid")
		return
	}
	g, err := s.Store.Ent.Gate.Get(ctx, req.GateID)
	pub, keyErr := base64.StdEncoding.DecodeString(g.PublicKey)
	sig, sigErr := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil || g.Status != gate.StatusActive || keyErr != nil || sigErr != nil || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, []byte(req.Challenge), sig) {
		failure(c, 401, "signature_invalid")
		return
	}
	token := randomToken(32)
	if s.Store.KV.Set(ctx, "gate-token:"+digest(token), strconv.Itoa(g.ID), 15*time.Minute) != nil {
		failure(c, 503, "auth_unavailable")
		return
	}
	c.JSON(200, gin.H{"token": token, "expires_in": 900})
}

// agentAuth 是 /agent/* 的鉴权中间件；吊销的设备立即失效。
func (s *Server) agentAuth(c *gin.Context) {
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	raw, ok, err := s.Store.KV.Get(c.Request.Context(), "gate-token:"+digest(token))
	id, convErr := strconv.Atoi(raw)
	if err != nil || !ok || convErr != nil {
		failure(c, 401, "unauthorized")
		return
	}
	g, err := s.Store.Ent.Gate.Get(c.Request.Context(), id)
	if err != nil || g.Status != gate.StatusActive {
		failure(c, 401, "device_not_active")
		return
	}
	c.Set("gate", g)
	c.Next()
}

func gateFrom(c *gin.Context) *ent.Gate { return c.MustGet("gate").(*ent.Gate) }
