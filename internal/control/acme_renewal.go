package control

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"strconv"
	"time"
)

func (s *Server) certificatePolicyRoutes(admin *gin.RouterGroup) {
	admin.GET("/devices/:id/certificate-policies", s.listCertificatePolicies)
	admin.PUT("/devices/:id/certificate-policies/:certificate", s.toggleCertificatePolicy)
}
func (s *Server) listCertificatePolicies(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		failure(c, 400, "invalid_device")
		return
	}
	items, err := s.Store.Resource.Query().Where(resource.KindEQ("certificate-policy"), func(q *sql.Selector) {
		q.Where(sqljson.ValueEQ(resource.FieldData, id, sqljson.Path("device_id")))
	}).Order(ent.Asc("id")).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := []gin.H{}
	for _, item := range items {
		if int(number(item.Data["device_id"])) != id {
			continue
		}
		certID := int(number(item.Data["certificate_id"]))
		exists, e := s.Store.Resource.Query().Where(resource.KindEQ("device-certificate"), resource.KeyEQ(fmt.Sprintf("%d:%d", id, certID))).Exist(c.Request.Context())
		state := "enabled"
		if e != nil {
			state = "unknown"
		} else if !exists {
			state = "certificate_missing"
		} else if item.Data["enabled"] != true {
			state = "disabled"
		}
		out = append(out, gin.H{"certificate_id": certID, "name": item.Data["name"], "enabled": item.Data["enabled"], "staging": item.Data["staging"], "domains": item.Data["domains"], "state": state})
	}
	c.JSON(200, gin.H{"items": out})
}
func (s *Server) toggleCertificatePolicy(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
		Confirm bool `json:"confirm"`
	}
	if !bind(c, &req) {
		return
	}
	id, e := strconv.Atoi(c.Param("id"))
	certID, err := strconv.Atoi(c.Param("certificate"))
	if e != nil || err != nil || id <= 0 || certID <= 0 || !req.Confirm {
		failure(c, 400, "invalid_certificate_policy")
		return
	}
	result, err := s.DB.ExecContext(c.Request.Context(), `UPDATE migate.resources SET data=jsonb_set(data,'{enabled}',$1::jsonb,true) WHERE kind='certificate-policy' AND key=$2`, []byte(strconv.FormatBool(req.Enabled)), fmt.Sprintf("%d:%d", id, certID))
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		failure(c, 404, "not_found")
		return
	}
	s.audit(c, "certificate.renewal-policy", fmt.Sprintf("%d:%d", id, certID), "ok")
	c.JSON(200, gin.H{"ok": true})
}
func saveCertificatePolicy(ctx context.Context, tx *ent.Tx, job *ent.Resource) error {
	key := fmt.Sprintf("%d:%d", int(number(job.Data["device_id"])), int(number(job.Data["certificate_id"])))
	data := map[string]any{}
	for _, field := range []string{"device_id", "certificate_id", "name", "domains", "email", "provider_id", "staging"} {
		data[field] = job.Data[field]
	}
	data["enabled"] = job.Data["auto_renew"] == true
	old, err := tx.Resource.Query().Where(resource.KindEQ("certificate-policy"), resource.KeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		_, err = tx.Resource.Create().SetKind("certificate-policy").SetKey(key).SetData(data).Save(ctx)
	} else if err == nil {
		if number(job.Data["renewal_policy_id"]) > 0 {
			data["enabled"] = old.Data["enabled"]
		}
		_, err = tx.Resource.UpdateOne(old).SetData(data).Save(ctx)
	}
	return err
}
func (s *Server) scheduleCertificateRenewals(ctx context.Context, now time.Time) {
	after := 0
	for ctx.Err() == nil {
		items, err := s.Store.Resource.Query().Where(resource.KindEQ("certificate-policy"), resource.IDGT(after)).Order(ent.Asc("id")).Limit(100).All(ctx)
		if err != nil || len(items) == 0 {
			return
		}
		for _, policy := range items {
			after = policy.ID
			if policy.Data["enabled"] != true {
				continue
			}
			s.queueCertificateRenewal(ctx, policy, now)
		}
	}
}
func (s *Server) queueCertificateRenewal(ctx context.Context, policy *ent.Resource, now time.Time) {
	deviceID := int(number(policy.Data["device_id"]))
	certID := int(number(policy.Data["certificate_id"]))
	certificate, err := s.Store.Resource.Query().Where(resource.KindEQ("device-certificate"), resource.KeyEQ(fmt.Sprintf("%d:%d", deviceID, certID))).Only(ctx)
	if err != nil {
		return
	}
	block, _ := pem.Decode([]byte(text(certificate.Data["certificate_pem"])))
	if block == nil {
		return
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil || leaf.NotAfter.Sub(now) > 30*24*time.Hour {
		return
	}
	d, err := s.Store.Device.Get(ctx, deviceID)
	if err != nil || d.Status != "approved" {
		return
	}
	capable := false
	caps, _ := d.Metadata["capabilities"].([]any)
	for _, cap := range caps {
		capable = capable || cap == "certificate.delivery.v1"
	}
	if !capable {
		return
	}
	key := fmt.Sprintf("renew:%d:%d:%d", deviceID, certID, leaf.NotAfter.Unix())
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(8400000000)+int64(deviceID)); err != nil {
		return
	}
	// The scheduler's scan may precede an administrator disabling the policy.
	// Lock and re-read it at the queue boundary; UPDATE uses the same row lock.
	var currentRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE id=$1 AND kind='certificate-policy' FOR UPDATE`, policy.ID).Scan(&currentRaw); err != nil {
		return
	}
	var current map[string]any
	if json.Unmarshal(currentRaw, &current) != nil || current["enabled"] != true || int(number(current["device_id"])) != deviceID || int(number(current["certificate_id"])) != certID {
		return
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM migate.resources WHERE kind='certificate-order' AND data->>'device_id'=$1 AND data->>'status' IN ('pending','running','deploying'))`, strconv.Itoa(deviceID)).Scan(&active); err != nil || active {
		return
	}
	data := map[string]any{}
	for field, value := range current {
		if field != "enabled" {
			data[field] = value
		}
	}
	data["auto_renew"] = true
	data["status"] = "pending"
	data["attempts"] = 0
	data["renewal_policy_id"] = policy.ID
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	// A failed/paused renewal for this exact expiry is retained, never recreated
	// every poll. An administrator may use the bounded explicit retry operation.
	if _, err = tx.ExecContext(ctx, `INSERT INTO migate.resources(kind,key,owner_id,data,created_at) VALUES('certificate-order',$1,0,$2,$3) ON CONFLICT(kind,key) DO NOTHING`, key, raw, now); err != nil {
		return
	}
	tx.Commit()
}
