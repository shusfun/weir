package control

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/device"
	"github.com/imzyb/MiGate/ent/grant"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/ent/task"
	"github.com/imzyb/MiGate/ent/user"
	"github.com/imzyb/MiGate/packages/contracts"
)

var safeCoreVersion = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func (s *Server) deviceRoutes(ready, admin *gin.RouterGroup) {
	s.poolRoutes(admin)
	s.proxyCatalogRoutes(admin)
	s.acmeRoutes(admin)
	ready.POST("/devices/pair", s.pairDevice)
	ready.POST("/devices/challenge", s.deviceChallenge)
	ready.POST("/devices/auth", s.deviceAuth)
	ready.POST("/devices/recovery/challenge", s.recoveryChallenge)
	ready.POST("/devices/recovery", s.recoverDevice)
	a := ready.Group("/agent")
	a.Use(s.agentAuth)
	a.GET("/tasks", s.pollTasks)
	a.POST("/results", s.taskResult)
	a.POST("/heartbeat", s.heartbeat)
	a.POST("/meter", s.meter)
	a.POST("/revoke", func(c *gin.Context) {
		if err := s.markDeviceRevoked(c.Request.Context(), c.GetInt("device_id")); err != nil {
			failure(c, 500, "device_revoke_failed")
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	admin.GET("/devices", s.listDevices)
	admin.POST("/devices/:id/approve", s.approveDevice)
	admin.POST("/devices/:id/revoke", s.revokeDevice)
	admin.GET("/tasks", s.listTasks)
	admin.POST("/devices/:id/tasks", s.createTask)
	admin.GET("/grants", s.listGrants)
	admin.POST("/grants", s.createGrant)
	admin.DELETE("/grants/:id", s.deleteGrant)
}
func (s *Server) pairDevice(c *gin.Context) {
	if !s.rate(c, "device-pair", 10) {
		return
	}
	var req struct {
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
	}
	if !bind(c, &req) {
		return
	}
	pub, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize || len(req.Name) > 100 {
		failure(c, 400, "invalid_device")
		return
	}
	code := randomToken(6)
	d, err := s.Store.Device.Create().SetName(req.Name).SetPublicKey(req.PublicKey).SetPairCode(digest(code)).SetPairExpires(time.Now().Add(10 * time.Minute)).Save(c.Request.Context())
	if err != nil {
		failure(c, 409, "device_already_registered")
		return
	}
	c.JSON(201, gin.H{"device_id": d.ID, "pair_code": code, "fingerprint": digest(string(pub)), "expires_in": 600})
}
func (s *Server) deviceChallenge(c *gin.Context) {
	if !s.rate(c, "device-auth", 120) {
		return
	}
	var req struct {
		DeviceID int `json:"device_id"`
	}
	if !bind(c, &req) {
		return
	}
	d, err := s.Store.Device.Get(c.Request.Context(), req.DeviceID)
	if err != nil || d.Status != "approved" {
		failure(c, 403, "device_not_approved")
		return
	}
	challenge := randomToken(32)
	if s.Redis.Set(c.Request.Context(), deviceChallengeKey(d, challenge), "1", time.Minute).Err() != nil {
		failure(c, 503, "challenge_unavailable")
		return
	}
	c.JSON(200, gin.H{"challenge": challenge})
}
func (s *Server) deviceAuth(c *gin.Context) {
	var req struct {
		DeviceID  int    `json:"device_id"`
		Challenge string `json:"challenge"`
		Signature string `json:"signature"`
	}
	if !bind(c, &req) {
		return
	}
	d, err := s.Store.Device.Get(c.Request.Context(), req.DeviceID)
	if err != nil || d.Status != "approved" {
		failure(c, 403, "device_not_approved")
		return
	}
	pub, e := base64.StdEncoding.DecodeString(d.PublicKey)
	sig, e2 := base64.StdEncoding.DecodeString(req.Signature)
	if e != nil || e2 != nil || len(pub) != 32 || !ed25519.Verify(pub, []byte(req.Challenge), sig) {
		failure(c, 401, "invalid_signature")
		return
	}
	n, err := s.Redis.Del(c.Request.Context(), deviceChallengeKey(d, req.Challenge)).Result()
	if err != nil || n != 1 {
		failure(c, 401, "challenge_expired_or_replayed")
		return
	}
	token := randomToken(32)
	session, _ := json.Marshal(deviceSession{ID: d.ID, Epoch: d.PairExpires.UTC().Format(time.RFC3339Nano)})
	if s.Redis.Set(c.Request.Context(), "migate:device-token:"+digest(token), string(session), 15*time.Minute).Err() != nil {
		failure(c, 503, "device_session_unavailable")
		return
	}
	c.JSON(200, gin.H{"token": token, "expires_in": 900})
}
func (s *Server) agentAuth(c *gin.Context) {
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	raw, err := s.Redis.Get(c.Request.Context(), "migate:device-token:"+digest(token)).Bytes()
	var session deviceSession
	if err != nil || json.Unmarshal(raw, &session) != nil || session.ID <= 0 {
		failure(c, 401, "device_unauthorized")
		return
	}
	d, err := s.Store.Device.Get(c.Request.Context(), session.ID)
	if err != nil || d.Status != "approved" {
		failure(c, 403, "device_revoked")
		return
	}
	if session.Epoch != d.PairExpires.UTC().Format(time.RFC3339Nano) {
		failure(c, 401, "device_session_expired")
		return
	}
	c.Set("device_id", d.ID)
	c.Next()
}
func (s *Server) listDevices(c *gin.Context) {
	items, err := s.Store.Device.Query().Order(ent.Desc("id")).All(c.Request.Context())
	if err != nil {
		failure(c, 500, "query_failed")
		return
	}
	out := []gin.H{}
	for _, d := range items {
		status := d.Status
		if status == "approved" && time.Since(d.LastSeen) > 60*time.Second {
			status = "offline"
		}
		out = append(out, gin.H{"id": d.ID, "name": d.Name, "status": status, "last_seen": d.LastSeen, "fingerprint": digestPublic(d.PublicKey), "metadata": d.Metadata, "pair_expires": d.PairExpires})
	}
	c.JSON(200, gin.H{"items": out})
}
func digestPublic(v string) string {
	b, _ := base64.StdEncoding.DecodeString(v)
	return digest(string(b))
}
func (s *Server) approveDevice(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Code        string `json:"code"`
		Fingerprint string `json:"fingerprint"`
		Password    string `json:"password"`
		MFACode     string `json:"mfa_code"`
	}
	if !bind(c, &req) {
		return
	}
	d, err := s.Store.Device.Get(c.Request.Context(), id)
	if err != nil || (d.Status != "pending" && d.Status != "recovery") || time.Now().After(d.PairExpires) || d.PairCode != digest(req.Code) || req.Fingerprint != digestPublic(d.PublicKey) {
		failure(c, 400, "pair_verification_failed")
		return
	}
	if d.Status == "recovery" && !s.reauth(c, req.Password, req.MFACode) {
		return
	}
	n, err := s.Store.Device.Update().Where(device.IDEQ(id), device.StatusEQ(d.Status), device.PairCodeEQ(d.PairCode)).SetStatus("approved").SetPairCode("").Save(c.Request.Context())
	if err != nil || n != 1 {
		failure(c, 409, "pair_already_consumed")
		return
	}
	s.audit(c, "device.approve", fmt.Sprint(id), "ok")
	c.JSON(200, gin.H{"approved": true})
}
func (s *Server) revokeDevice(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	err := s.markDeviceRevoked(c.Request.Context(), id)
	if err != nil {
		failure(c, 404, "device_not_found")
		return
	}
	s.audit(c, "device.revoke", fmt.Sprint(id), "ok")
	c.JSON(200, gin.H{"revoked": true})
}
func (s *Server) enqueue(ctx context.Context, id int, op, key string, revision int64, payload map[string]any) (*ent.Task, error) {
	return s.Store.Task.Create().SetDeviceID(id).SetOperation(op).SetKey(key).SetRevision(revision).SetPayload(payload).SetExpiresAt(time.Now().Add(24 * time.Hour)).SetLeasedUntil(time.Unix(0, 0)).Save(ctx)
}
func allowedOperation(op string) bool {
	if op == "outbound.catalog.probe" {
		return true
	}
	switch op {
	case "inbound.upsert", "inbound.delete", "outbound.upsert", "outbound.delete", "outbound.reorder", "outbound.probe", "routing.upsert", "routing.delete", "routing.reorder", "certificate.upsert", "certificate.delete", "certificate.clean", "certificate.bind", "grant.upsert", "grant.revoke", "core.status", "core.inspect", "core.restart", "core.start", "core.stop", "core.install", "core.rollback", "core.clean":
		return true
	}
	return false
}
func (s *Server) createTask(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Operation string         `json:"operation"`
		Payload   map[string]any `json:"payload"`
		Confirm   bool           `json:"confirm"`
	}
	if !bind(c, &req) {
		return
	}
	if !allowedOperation(req.Operation) || strings.HasPrefix(req.Operation, "grant.") || !req.Confirm {
		failure(c, 400, "operation_or_confirmation_invalid")
		return
	}
	if d, err := s.Store.Device.Get(c.Request.Context(), id); err != nil || d.Status != "approved" {
		failure(c, 404, "device_not_found")
		return
	}
	if req.Operation == "core.inspect" || req.Operation == "core.start" || req.Operation == "core.stop" || req.Operation == "core.install" || req.Operation == "core.rollback" || req.Operation == "core.clean" {
		capability := "core.control.v1"
		if req.Operation == "core.inspect" {
			capability = "core.diagnostics.v1"
		}
		if req.Operation == "core.install" || req.Operation == "core.rollback" || req.Operation == "core.clean" {
			capability = "core.versions.v1"
		}
		d, _ := s.Store.Device.Get(c.Request.Context(), id)
		capable := false
		caps, _ := d.Metadata["capabilities"].([]any)
		for _, cap := range caps {
			capable = capable || cap == capability
		}
		if !capable {
			failure(c, 400, "agent_capability_required")
			return
		}
		if core := text(req.Payload["core"]); core != "xray" && core != "sing-box" {
			failure(c, 400, "invalid_managed_core")
			return
		}
		if req.Operation == "core.install" && !safeCoreVersion.MatchString(text(req.Payload["version"])) {
			failure(c, 400, "invalid_core_version")
			return
		}
		for key := range req.Payload {
			if key != "core" && (key != "version" || req.Operation != "core.install") {
				failure(c, 400, "invalid_core_payload")
				return
			}
		}
	}
	if req.Operation == "certificate.bind" && !s.checkCertificateBinding(c, id, req.Payload) {
		return
	}
	if req.Operation == "certificate.clean" {
		d, _ := s.Store.Device.Get(c.Request.Context(), id)
		capable := false
		caps, _ := d.Metadata["capabilities"].([]any)
		for _, cap := range caps {
			capable = capable || cap == "certificate.history.v1"
		}
		if !capable {
			failure(c, 400, "agent_capability_required")
			return
		}
		if len(req.Payload) != 1 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(text(req.Payload["preview_digest"])) {
			failure(c, 400, "invalid_certificate_cleanup")
			return
		}
	}
	if req.Operation == "certificate.upsert" {
		if value, supplied := req.Payload["name"]; supplied {
			name, ok := value.(string)
			if !ok || len(name) > 200 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
				failure(c, 400, "invalid_certificate_name")
				return
			}
		} else if certificateID := int(number(req.Payload["id"])); certificateID > 0 {
			old, err := s.Store.Resource.Query().Where(resource.KindEQ("device-certificate"), resource.KeyEQ(fmt.Sprintf("%d:%d", id, certificateID))).Only(c.Request.Context())
			if err != nil && !ent.IsNotFound(err) {
				failure(c, 503, "query_failed")
				return
			}
			if old != nil {
				req.Payload["name"] = text(old.Data["name"])
			}
		}
	}
	if req.Operation == "routing.reorder" && !s.checkRoutingOrder(c, id, req.Payload) {
		return
	}
	if req.Operation == "outbound.upsert" || req.Operation == "outbound.delete" {
		old, err := s.Store.Resource.Query().Where(resource.KindEQ("device-outbound"), resource.KeyEQ(fmt.Sprintf("%d:%d", id, int(number(req.Payload["id"]))))).Only(c.Request.Context())
		if err != nil && !ent.IsNotFound(err) {
			failure(c, 503, "query_failed")
			return
		}
		if old != nil && text(old.Data["source"]) == "subscription" {
			failure(c, 409, "managed_pool_member_use_pool")
			return
		}
		for _, key := range []string{"source", "subscription_id", "subscription_identity"} {
			delete(req.Payload, key)
		}
	}
	if req.Operation == "inbound.upsert" || req.Operation == "outbound.upsert" {
		if req.Payload == nil {
			req.Payload = map[string]any{}
		}
		kind := strings.SplitN(req.Operation, ".", 2)[0]
		old, err := s.Store.Resource.Query().Where(resource.KindEQ("device-"+kind), resource.KeyEQ(fmt.Sprintf("%d:%d", id, int(number(req.Payload["id"]))))).Only(c.Request.Context())
		if err != nil && !ent.IsNotFound(err) {
			failure(c, 500, "query_failed")
			return
		}
		if old != nil {
			// Sorting is a separate whole-inventory task, never an edit side effect.
			if req.Operation == "outbound.upsert" {
				req.Payload["sort"] = old.Data["sort"]
			}
			if req.Operation == "outbound.upsert" && text(req.Payload["protocol"]) == text(old.Data["protocol"]) {
				if text(req.Payload["username"]) == "" {
					req.Payload["username"] = old.Data["username"]
				}
				if fields, ok := req.Payload["transport_options"].(map[string]any); ok && text(fields["obfs"]) == "salamander" && text(fields["obfs_password"]) == "" {
					var settings struct {
						Password string `json:"obfs_password"`
					}
					json.Unmarshal([]byte(text(old.Data["settings_json"])), &settings)
					fields["obfs_password"] = settings.Password
				}
			}
			// Generated public handshake parameters travel with the preserved
			// private identity. Older callers need not echo them on every edit.
			if req.Operation == "inbound.upsert" && text(req.Payload["reality_private_key"]) == "" {
				for _, key := range []string{"reality_public_key", "reality_short_id"} {
					if _, supplied := req.Payload[key]; !supplied {
						if value, exists := old.Data[key]; exists {
							req.Payload[key] = value
						}
					}
				}
			}
			for _, key := range []string{"password", "reality_private_key", "hy2_obfs_password", "wg_private_key", "wg_preshared_key", "shadowtls_password", "raw_link", "settings_json"} {
				if text(req.Payload[key]) == "" && text(old.Data[key]) != "" {
					req.Payload[key] = old.Data[key]
				}
			}
		}
		if req.Operation == "inbound.upsert" && text(req.Payload["security"]) == "reality" && text(req.Payload["reality_private_key"]) == "" {
			key, e := ecdh.X25519().GenerateKey(rand.Reader)
			if e != nil {
				failure(c, 500, "key_generation_failed")
				return
			}
			req.Payload["reality_private_key"] = base64.RawURLEncoding.EncodeToString(key.Bytes())
			req.Payload["reality_public_key"] = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
			req.Payload["reality_short_id"] = randomToken(4)
		}
	}
	if !s.prepareResourceEnabled(c, id, req.Operation, req.Payload) {
		return
	}
	if req.Operation == "outbound.upsert" && !s.prepareOutboundTransport(c, id, req.Payload) {
		return
	}
	ctx := c.Request.Context()
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		failure(c, 500, "task_create_failed")
		return
	}
	defer tx.Rollback()
	if strings.HasPrefix(req.Operation, "outbound.") || strings.HasPrefix(req.Operation, "routing.") {
		if _, err := lockPoolDevice(ctx, tx, id); err != nil {
			failure(c, 409, "device_changed")
			return
		}
		if pending, err := pendingPoolConfiguration(ctx, tx, id, true); err != nil || pending {
			failure(c, 409, "pool_configuration_pending")
			return
		}
	}
	if req.Operation == "outbound.reorder" || req.Operation == "outbound.probe" || req.Operation == "outbound.catalog.probe" {
		var ok bool
		req.Payload, ok = s.prepareOutboundTool(c, tx, id, req.Operation, req.Payload)
		if !ok {
			return
		}
	}
	t, err := tx.Task.Create().SetDeviceID(id).SetOperation(req.Operation).SetKey(randomToken(16)).SetPayload(req.Payload).SetExpiresAt(time.Now().Add(24 * time.Hour)).SetLeasedUntil(time.Unix(0, 0)).Save(ctx)
	if err == nil && (req.Operation == "outbound.probe" || req.Operation == "outbound.catalog.probe") {
		t, err = tx.Task.UpdateOne(t).SetExpiresAt(time.Now().Add(90 * time.Second)).Save(ctx)
	}
	if err == nil && strings.HasSuffix(req.Operation, ".upsert") && int(number(req.Payload["id"])) == 0 {
		kind := strings.SplitN(req.Operation, ".", 2)[0]
		resourceID := 1000000000 + t.ID
		exists, e := tx.Resource.Query().Where(resource.KindEQ("device-"+kind), resource.KeyEQ(fmt.Sprintf("%d:%d", id, resourceID))).Exist(ctx)
		if e != nil || exists {
			failure(c, 409, "resource_id_conflict")
			return
		}
		if req.Payload == nil {
			req.Payload = map[string]any{}
		}
		req.Payload["id"] = resourceID
		t, err = tx.Task.UpdateOne(t).SetPayload(req.Payload).Save(ctx)
	}
	if req.Operation == "certificate.upsert" {
		key := text(req.Payload["key_pem"])
		if key == "" || len(key) > 16384 || len(text(req.Payload["certificate_pem"])) > 65536 {
			failure(c, 400, "invalid_certificate")
			return
		}
		sealed, err := s.Crypto.Encrypt(fmt.Sprintf("certificate:%d:%d:", id, int(number(req.Payload["id"]))) + key)
		if err != nil {
			failure(c, 500, "save_failed")
			return
		}
		delete(req.Payload, "key_pem")
		req.Payload["sealed_key"] = sealed
	}
	if err == nil {
		t, err = tx.Task.UpdateOne(t).SetPayload(req.Payload).Save(ctx)
	}
	if err == nil {
		t, err = tx.Task.UpdateOne(t).SetRevision(int64(t.ID)).Save(ctx)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(c, 500, "task_create_failed")
		return
	}
	s.audit(c, "task."+req.Operation, fmt.Sprint(t.ID), "queued")
	c.JSON(202, gin.H{"id": t.ID, "status": t.Status, "resource_id": t.Payload["id"]})
}
func (s *Server) pollTasks(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.GetInt("device_id")
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		d, err := s.Store.Device.Get(ctx, id)
		if err != nil || d.Status != "approved" {
			failure(c, 403, "device_revoked")
			return
		}
		items, err := s.Store.Task.Query().Where(task.DeviceIDEQ(id), task.ExpiresAtGT(time.Now()), task.Or(task.StatusEQ("pending"), task.And(task.StatusEQ("running"), task.LeasedUntilLT(time.Now())))).Order(ent.Asc("id")).Limit(1).All(ctx)
		if err != nil {
			failure(c, 500, "task_query_failed")
			return
		}
		if len(items) > 0 {
			t := items[0]
			payload := t.Payload
			if t.Operation == "certificate.upsert" && text(payload["sealed_key"]) != "" {
				plain, err := s.Crypto.Decrypt(text(payload["sealed_key"]))
				prefix := fmt.Sprintf("certificate:%d:%d:", id, int(number(payload["id"])))
				if err != nil || !strings.HasPrefix(plain, prefix) {
					failure(c, 500, "task_secret_unavailable")
					return
				}
				payload = make(map[string]any, len(t.Payload))
				for k, v := range t.Payload {
					if k != "sealed_key" {
						payload[k] = v
					}
				}
				payload["key_pem"] = strings.TrimPrefix(plain, prefix)
			}
			n, err := s.Store.Task.Update().Where(task.IDEQ(t.ID), task.LeasedUntilLT(time.Now()), task.StatusIn("pending", "running")).SetStatus("running").SetLeasedUntil(time.Now().Add(2 * time.Minute)).AddAttempts(1).Save(ctx)
			if err == nil && n == 1 {
				c.JSON(200, gin.H{"items": []contracts.Task{{ID: t.ID, DeviceID: id, Operation: t.Operation, Revision: t.Revision, Payload: payload, ExpiresAt: t.ExpiresAt}}})
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			c.JSON(200, gin.H{"items": []contracts.Task{}})
			return
		case <-tick.C:
		}
	}
}
func (s *Server) taskResult(c *gin.Context) {
	var req contracts.Result
	if !bind(c, &req) {
		return
	}
	if req.Status != "applied" && req.Status != "failed" {
		failure(c, 400, "invalid_result")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	defer tx.Rollback()
	// Lock before reading status: concurrent duplicate receipts must observe
	// the committed terminal state without duplicating resources or audit rows.
	_, err = tx.Task.UpdateOneID(req.TaskID).AddAttempts(0).Save(ctx)
	if err != nil {
		failure(c, 403, "task_mismatch")
		return
	}
	t, err := tx.Task.Get(ctx, req.TaskID)
	if err != nil || t.DeviceID != c.GetInt("device_id") || t.Revision != req.Revision {
		failure(c, 403, "task_mismatch")
		return
	}
	if t.Status == "applied" || t.Status == "failed" {
		c.JSON(200, gin.H{"ok": true})
		return
	}
	if t.Status != "running" {
		failure(c, 409, "task_not_running")
		return
	}
	if (t.Operation == "outbound.probe" || t.Operation == "outbound.catalog.probe") && req.Status == "applied" {
		payload := t.Payload
		if t.Operation == "outbound.catalog.probe" {
			payload = map[string]any{"items": []contracts.OutboundProbeItem{{ID: 1, Digest: text(t.Payload["key"])}}}
		}
		req.Data, err = outboundProbeResult(payload, req.Data)
		if err != nil {
			failure(c, 400, "invalid_probe_result")
			return
		}
	}
	if (t.Operation == "outbound.probe" || t.Operation == "outbound.catalog.probe") && req.Status == "failed" {
		req.Data = map[string]any{"message": "outbound_probe_failed"}
	}
	if _, err = tx.Task.UpdateOne(t).SetStatus(req.Status).SetResult(req.Data).Save(ctx); err != nil {
		failure(c, 500, "save_failed")
		return
	}
	if strings.HasPrefix(t.Operation, "grant.") {
		gid := int(number(t.Payload["grant_id"]))
		g, e := tx.Grant.Get(ctx, gid)
		if e != nil || g.DeviceID != t.DeviceID {
			failure(c, 409, "grant_task_mismatch")
			return
		}
		if g.Revision == t.Revision {
			status := req.Status
			if t.Operation == "grant.revoke" && status == "applied" {
				status = "revoked"
			}
			if _, err = tx.Grant.UpdateOne(g).SetStatus(status).Save(ctx); err != nil {
				failure(c, 500, "save_failed")
				return
			}
		}
	}
	for _, kind := range []string{"inbound", "outbound", "routing", "certificate"} {
		if t.Operation == kind+".upsert" && req.Status == "applied" {
			data := t.Payload
			if kind == "certificate" {
				data = certificateResourceData(t.Payload)
			}
			err = saveDeviceResource(ctx, tx, t.DeviceID, kind, data)
		}
		if t.Operation == kind+".delete" && req.Status == "applied" {
			_, err = tx.Resource.Delete().Where(resource.KindEQ("device-"+kind), resource.KeyEQ(fmt.Sprintf("%d:%d", t.DeviceID, int(number(t.Payload["id"]))))).Exec(ctx)
		}
		if err != nil {
			failure(c, 500, "save_failed")
			return
		}
	}
	if t.Operation == "certificate.bind" && req.Status == "applied" {
		if err = saveCertificateBinding(ctx, tx, t.DeviceID, t.Payload); err != nil {
			failure(c, 409, "certificate_binding_resource_changed")
			return
		}
	}
	if t.Operation == "routing.reorder" && req.Status == "applied" {
		if err = saveRoutingOrder(ctx, tx, t.DeviceID, t.Payload); err != nil {
			failure(c, 500, "save_failed")
			return
		}
	}
	if t.Operation == "outbound.reorder" && req.Status == "applied" {
		if err = saveOutboundOrder(ctx, tx, t.DeviceID, t.Payload); err != nil {
			failure(c, 409, "outbound_order_changed")
			return
		}
	}
	if t.Operation == "outbound.pool.apply" {
		if err = savePoolResult(ctx, tx, t, req.Status); err != nil {
			failure(c, 409, "pool_result_conflict")
			return
		}
	}
	_, err = tx.Audit.Create().SetActor(c.GetInt("user_id")).SetAction("task.result").SetTarget(fmt.Sprint(t.ID)).SetResult(req.Status).SetIP(c.ClientIP()).Save(ctx)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func saveDeviceResource(ctx context.Context, tx *ent.Tx, id int, kind string, data map[string]any) error {
	key := fmt.Sprintf("%d:%d", id, int(number(data["id"])))
	r, err := tx.Resource.Query().Where(resource.KindEQ("device-"+kind), resource.KeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		_, err = tx.Resource.Create().SetKind("device-" + kind).SetKey(key).SetOwnerID(id).SetData(data).Save(ctx)
	} else if err == nil {
		_, err = tx.Resource.UpdateOne(r).SetData(data).Save(ctx)
	}
	return err
}

func certificateResourceData(payload map[string]any) map[string]any {
	return map[string]any{"id": payload["id"], "name": text(payload["name"]), "certificate_pem": payload["certificate_pem"]}
}
func (s *Server) heartbeat(c *gin.Context) {
	var req map[string]any
	if !bind(c, &req) {
		return
	}
	safe := map[string]any{}
	for _, key := range []string{"version", "hostname", "traffic_status", "inbounds", "core_status", "capabilities"} {
		if v, ok := req[key]; ok {
			safe[key] = v
		}
	}
	safe["host_status"] = sanitizeHostObservation(req["host_status"], time.Now())
	_, err := s.Store.Device.UpdateOneID(c.GetInt("device_id")).SetLastSeen(time.Now()).SetMetadata(safe).Save(c.Request.Context())
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) listTasks(c *gin.Context) {
	query := s.Store.Task.Query()
	if value := c.Query("device_id"); value != "" {
		id, err := strconv.Atoi(value)
		if err != nil || id <= 0 {
			failure(c, 400, "invalid_device")
			return
		}
		query.Where(task.DeviceIDEQ(id))
	}
	if value := c.Query("status"); value != "" {
		switch value {
		case "pending":
			query.Where(task.StatusEQ(value), task.ExpiresAtGT(time.Now()))
		case "expired":
			query.Where(task.StatusEQ("pending"), task.ExpiresAtLTE(time.Now()))
		case "running", "applied", "failed":
			query.Where(task.StatusEQ(value))
		default:
			failure(c, 400, "invalid_status")
			return
		}
	}
	if value := strings.TrimSpace(c.Query("q")); value != "" {
		if len(value) > 100 {
			failure(c, 400, "invalid_query")
			return
		}
		if id, err := strconv.Atoi(strings.TrimPrefix(value, "#")); err == nil {
			query.Where(task.IDEQ(id))
		} else {
			query.Where(task.OperationContainsFold(value))
		}
	}
	page, size := 1, 200
	if c.Query("page") != "" || c.Query("page_size") != "" {
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
	for _, t := range items {
		status := t.Status
		if t.Status == "pending" && time.Now().After(t.ExpiresAt) {
			status = "expired"
		}
		out = append(out, gin.H{"id": t.ID, "device_id": t.DeviceID, "operation": t.Operation, "revision": t.Revision, "status": status, "attempts": t.Attempts, "result": t.Result, "created_at": t.CreatedAt, "expires_at": t.ExpiresAt, "leased_until": t.LeasedUntil})
	}
	c.JSON(200, gin.H{"items": out, "total": total, "page": page, "page_size": size})
}
func (s *Server) listGrants(c *gin.Context) {
	items, err := s.Store.Grant.Query().Order(ent.Desc("id")).All(c.Request.Context())
	if err != nil {
		failure(c, 500, "query_failed")
		return
	}
	emails := map[int]string{}
	ids := []int{}
	seen := map[int]bool{}
	for _, g := range items {
		if !seen[g.UserID] {
			ids = append(ids, g.UserID)
			seen[g.UserID] = true
		}
	}
	for offset := 0; offset < len(ids); offset += 500 {
		end := offset + 500
		if end > len(ids) {
			end = len(ids)
		}
		users, err := s.Store.User.Query().Where(user.IDIn(ids[offset:end]...)).Select(user.FieldID, user.FieldEmail).All(c.Request.Context())
		if err != nil {
			failure(c, 503, "query_failed")
			return
		}
		for _, u := range users {
			emails[u.ID] = u.Email
		}
	}
	out := []gin.H{}
	for _, g := range items {
		out = append(out, gin.H{"id": g.ID, "user_id": g.UserID, "user_email": emails[g.UserID], "device_id": g.DeviceID, "inbound_id": g.InboundID, "status": g.Status, "enabled": g.Enabled, "allocated": g.Allocated, "used": g.Used})
	}
	c.JSON(200, gin.H{"items": out})
}
func (s *Server) createGrant(c *gin.Context) {
	var req struct {
		UserID    int `json:"user_id"`
		DeviceID  int `json:"device_id"`
		InboundID int `json:"inbound_id"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	dvc, de := s.Store.Device.Get(ctx, req.DeviceID)
	if de != nil || dvc.Status != "approved" {
		failure(c, 400, "approved_device_required")
		return
	}
	u, err := s.Store.User.Get(ctx, req.UserID)
	if err != nil || !u.Enabled || u.Quota <= 0 {
		failure(c, 400, "active_user_with_quota_required")
		return
	}
	r, err := s.Store.Resource.Query().Where(resource.KindEQ("device-inbound"), resource.KeyEQ(fmt.Sprintf("%d:%d", req.DeviceID, req.InboundID))).Only(ctx)
	if err != nil {
		failure(c, 400, "applied_inbound_required")
		return
	}
	if text(r.Data["protocol"]) != "vless" && text(r.Data["protocol"]) != "vmess" && text(r.Data["protocol"]) != "trojan" && text(r.Data["protocol"]) != "hysteria2" && text(r.Data["protocol"]) != "tuic" {
		failure(c, 400, "metered_protocol_required")
		return
	}
	credential := map[string]any{"uuid": uuid(), "password": randomToken(24)}
	g, err := s.Store.Grant.Create().SetUserID(u.ID).SetDeviceID(req.DeviceID).SetInboundID(req.InboundID).SetCredential(credential).Save(ctx)
	if err != nil {
		failure(c, 409, "grant_exists")
		return
	}
	budget, err := s.allocate(ctx, g.ID, 0, 0, "")
	if err != nil {
		failure(c, 500, "budget_allocation_failed")
		return
	}
	_, err = s.enqueue(ctx, g.DeviceID, "grant.upsert", fmt.Sprintf("grant:%d:%d", g.ID, g.Revision), g.Revision, map[string]any{"grant_id": g.ID, "inbound_id": g.InboundID, "credential": credential, "allocated": budget.Allocated, "expires": u.Expires, "enabled": true})
	if err != nil {
		failure(c, 500, "task_create_failed")
		return
	}
	s.audit(c, "grant.create", fmt.Sprint(g.ID), "queued")
	c.JSON(202, gin.H{"id": g.ID, "status": "pending"})
}
func uuid() string {
	h := randomToken(16)
	return h[:8] + "-" + h[8:12] + "-4" + h[13:16] + "-a" + h[17:20] + "-" + h[20:]
}
func (s *Server) deleteGrant(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	g, err := s.Store.Grant.Get(c.Request.Context(), id)
	if err != nil {
		failure(c, 404, "grant_not_found")
		return
	}
	g, err = s.Store.Grant.UpdateOne(g).SetEnabled(false).SetStatus("pending").AddRevision(1).Save(c.Request.Context())
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	_, err = s.enqueue(c.Request.Context(), g.DeviceID, "grant.revoke", fmt.Sprintf("grant:%d:%d", g.ID, g.Revision), g.Revision, map[string]any{"grant_id": g.ID, "inbound_id": g.InboundID})
	if err != nil {
		failure(c, 500, "task_create_failed")
		return
	}
	s.audit(c, "grant.revoke", fmt.Sprint(id), "queued")
	c.JSON(202, gin.H{"status": "pending"})
}
func (s *Server) syncUserGrants(ctx context.Context, u *ent.User) {
	items, _ := s.Store.Grant.Query().Where(grant.UserIDEQ(u.ID), grant.EnabledEQ(true)).All(ctx)
	for _, g := range items {
		g, err := s.Store.Grant.UpdateOne(g).AddRevision(1).SetStatus("pending").Save(ctx)
		if err != nil {
			continue
		}
		s.enqueue(ctx, g.DeviceID, "grant.upsert", fmt.Sprintf("grant:%d:%d", g.ID, g.Revision), g.Revision, map[string]any{"grant_id": g.ID, "inbound_id": g.InboundID, "credential": g.Credential, "allocated": g.Allocated, "expires": u.Expires, "enabled": u.Enabled})
	}
}
func (s *Server) allocate(ctx context.Context, gid int, used, sequence int64, epoch string) (contracts.Budget, error) {
	var result contracts.Budget
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	g, err := tx.Grant.Get(ctx, gid)
	if err != nil {
		return result, err
	}
	// A no-op UPDATE acquires a row lock, serializing budgets for every Gate of this user.
	u, err := tx.User.UpdateOneID(g.UserID).AddUsed(0).Save(ctx)
	if err != nil {
		return result, err
	}
	g, err = tx.Grant.Get(ctx, gid)
	if err != nil {
		return result, err
	}
	if used < g.Used || sequence < g.Sequence || sequence == g.Sequence && used != g.Used {
		return result, fmt.Errorf("invalid meter")
	}
	if sequence > g.Sequence {
		if epoch == "" {
			return result, fmt.Errorf("meter epoch required")
		}
		if g.MeterEpoch != "" && g.MeterEpoch != epoch {
			return result, fmt.Errorf("meter epoch changed")
		}
		delta := used - g.Used
		if delta > math.MaxInt64-u.Used {
			return result, fmt.Errorf("meter overflow")
		}
		u, err = tx.User.UpdateOne(u).AddUsed(delta).Save(ctx)
		if err != nil {
			return result, err
		}
		g, err = tx.Grant.UpdateOne(g).SetUsed(used).SetSequence(sequence).SetMeterEpoch(epoch).Save(ctx)
		if err != nil {
			return result, err
		}
	}
	grants, err := tx.Grant.Query().Where(grant.UserIDEQ(u.ID)).All(ctx)
	if err != nil {
		return result, err
	}
	reserved := int64(0)
	for _, item := range grants {
		remaining := max(int64(0), item.Allocated-item.Used)
		if remaining > math.MaxInt64-reserved {
			return result, fmt.Errorf("budget overflow")
		}
		reserved += remaining
	}
	available := max(int64(0), u.Quota-u.Used)
	available = max(int64(0), available-reserved)
	enabled := u.Enabled && g.Enabled && (u.Expires == 0 || time.Now().Unix() < u.Expires) && u.Quota > u.Used
	if enabled && g.Allocated-g.Used < contracts.BudgetBlock/4 && available > 0 {
		block := min(contracts.BudgetBlock, available)
		g, err = tx.Grant.UpdateOne(g).AddAllocated(block).Save(ctx)
		if err != nil {
			return result, err
		}
	}
	result = contracts.Budget{GrantID: g.ID, Allocated: g.Allocated, Enabled: enabled, Expires: u.Expires}
	return result, tx.Commit()
}
func (s *Server) meter(c *gin.Context) {
	var req struct {
		Items []contracts.Meter `json:"items"`
	}
	if !bind(c, &req) {
		return
	}
	if len(req.Items) > 1000 {
		failure(c, 400, "too_many_items")
		return
	}
	out := []contracts.Budget{}
	for _, m := range req.Items {
		g, err := s.Store.Grant.Get(c.Request.Context(), m.GrantID)
		if err != nil || g.DeviceID != c.GetInt("device_id") {
			failure(c, 403, "grant_device_mismatch")
			return
		}
		b, err := s.allocate(c.Request.Context(), m.GrantID, m.Used, m.Sequence, m.Epoch)
		if err != nil {
			failure(c, 409, "meter_conflict")
			return
		}
		out = append(out, b)
	}
	observed, _ := json.Marshal(map[string]any{"at": time.Now().UTC()})
	if _, err := s.DB.ExecContext(c.Request.Context(), `INSERT INTO migate.resources(kind,key,owner_id,data,created_at) VALUES('meter-observation',$1,$2,$3,now()) ON CONFLICT(kind,key) DO UPDATE SET data=excluded.data`, strconv.Itoa(c.GetInt("device_id")), c.GetInt("device_id"), observed); err != nil {
		failure(c, 503, "observation_unavailable")
		return
	}
	c.JSON(200, gin.H{"items": out})
}

var _ = json.Marshal
var _ = user.ID
