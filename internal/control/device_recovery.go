package control

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/device"
)

type deviceSession struct {
	ID    int    `json:"id"`
	Epoch string `json:"epoch"`
}

func deviceChallengeKey(d *ent.Device, challenge string) string {
	return fmt.Sprintf("migate:device-challenge:%d:%s", d.ID, digest(d.PairExpires.UTC().Format(time.RFC3339Nano)+":"+challenge))
}

func (s *Server) markDeviceRevoked(ctx context.Context, id int) error {
	_, err := s.Store.Device.UpdateOneID(id).SetStatus("revoked").SetPairCode("").SetPairExpires(time.Now().UTC()).Save(ctx)
	return err
}

func (s *Server) recoveryChallenge(c *gin.Context) {
	if !s.rate(c, "device-recovery", 10) {
		return
	}
	var req struct {
		ID int `json:"device_id"`
	}
	if !bind(c, &req) {
		return
	}
	d, err := s.Store.Device.Get(c.Request.Context(), req.ID)
	if err != nil || (d.Status != "revoked" && d.Status != "recovery") {
		failure(c, 403, "revoked_device_required")
		return
	}
	challenge := randomToken(32)
	if s.Redis.Set(c.Request.Context(), fmt.Sprintf("migate:device-recovery:%d:%s", d.ID, digest(challenge)), "1", time.Minute).Err() != nil {
		failure(c, 503, "challenge_unavailable")
		return
	}
	c.JSON(200, gin.H{"challenge": challenge, "expires_in": 60})
}

func (s *Server) recoverDevice(c *gin.Context) {
	if !s.rate(c, "device-recovery-proof", 10) {
		return
	}
	var req struct {
		ID        int    `json:"device_id"`
		Challenge string `json:"challenge"`
		Signature string `json:"signature"`
	}
	if !bind(c, &req) {
		return
	}
	d, err := s.Store.Device.Get(c.Request.Context(), req.ID)
	if err != nil || (d.Status != "revoked" && d.Status != "recovery") {
		failure(c, 403, "revoked_device_required")
		return
	}
	pub, e := base64.StdEncoding.DecodeString(d.PublicKey)
	sig, e2 := base64.StdEncoding.DecodeString(req.Signature)
	message := fmt.Sprintf("migate-device-recovery:v1:%d:%s", d.ID, req.Challenge)
	if e != nil || e2 != nil || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, []byte(message), sig) {
		failure(c, 401, "invalid_signature")
		return
	}
	n, err := s.Redis.Del(c.Request.Context(), fmt.Sprintf("migate:device-recovery:%d:%s", d.ID, digest(req.Challenge))).Result()
	if err != nil || n != 1 {
		failure(c, 401, "challenge_expired_or_replayed")
		return
	}
	code := randomToken(6)
	changed, err := s.Store.Device.Update().Where(device.IDEQ(d.ID), device.StatusIn("revoked", "recovery")).SetStatus("recovery").SetPairExpires(time.Now().Add(10 * time.Minute)).SetPairCode(digest(code)).Save(c.Request.Context())
	if err != nil || changed != 1 {
		failure(c, 409, "device_state_changed")
		return
	}
	s.audit(c, "device.recovery.request", fmt.Sprint(d.ID), "pending_admin_verification")
	c.JSON(200, gin.H{"device_id": d.ID, "pair_code": code, "fingerprint": digestPublic(d.PublicKey), "expires_in": 600, "status": "recovery"})
}
