package control

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	certservice "github.com/imzyb/MiGate/internal/service/cert"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var dnsZoneID = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

func (s *Server) acmeRoutes(admin *gin.RouterGroup) {
	s.certificatePolicyRoutes(admin)
	admin.GET("/devices/:id/dns-challenges", s.listDNSChallenges)
	admin.POST("/devices/:id/dns-challenges/:challenge/retry", s.retryDNSCleanup)
	admin.GET("/dns-providers", s.listDNSProviders)
	admin.POST("/dns-providers", s.saveDNSProvider)
	admin.DELETE("/dns-providers/:provider", s.deleteDNSProvider)
	admin.GET("/devices/:id/certificate-orders", s.listCertificateOrders)
	admin.POST("/devices/:id/certificate-orders", s.createCertificateOrder)
	admin.POST("/devices/:id/certificate-orders/:order/retry", s.retryCertificateOrder)
}
func (s *Server) listDNSProviders(c *gin.Context) {
	items, err := s.Store.Resource.Query().Where(resource.KindEQ("dns-provider")).Order(ent.Asc("id")).Limit(100).All(c.Request.Context())
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	out := []gin.H{}
	for _, item := range items {
		out = append(out, gin.H{"id": item.ID, "name": item.Data["name"], "provider": "cloudflare", "zone_id": item.Data["zone_id"], "token_set": text(item.Data["token"]) != ""})
	}
	c.JSON(200, gin.H{"items": out})
}
func (s *Server) saveDNSProvider(c *gin.Context) {
	var req struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		ZoneID   string `json:"zone_id"`
		Token    string `json:"token"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 100 || !dnsZoneID.MatchString(req.ZoneID) || len(req.Token) > 4096 {
		failure(c, 400, "invalid_dns_provider")
		return
	}
	data := map[string]any{"name": req.Name, "provider": "cloudflare", "zone_id": req.ZoneID}
	var old *ent.Resource
	if req.ID > 0 {
		var err error
		old, err = s.Store.Resource.Get(c.Request.Context(), req.ID)
		if err != nil || old.Kind != "dns-provider" {
			failure(c, 404, "not_found")
			return
		}
		if old.Data["zone_id"] != req.ZoneID && req.Token == "" {
			failure(c, 400, "dns_token_required_for_zone_change")
			return
		}
		data["token"] = old.Data["token"]
	}
	if req.Token != "" {
		sealed, err := s.Crypto.Encrypt("dns-provider:" + req.ZoneID + ":" + req.Token)
		if err != nil {
			failure(c, 500, "encryption_failed")
			return
		}
		data["token"] = sealed
	}
	if text(data["token"]) == "" {
		failure(c, 400, "dns_credentials_required")
		return
	}
	var item *ent.Resource
	var err error
	if old != nil {
		item, err = s.Store.Resource.UpdateOne(old).SetData(data).Save(c.Request.Context())
	} else {
		count, e := s.Store.Resource.Query().Where(resource.KindEQ("dns-provider")).Count(c.Request.Context())
		if e != nil || count >= 100 {
			failure(c, 400, "dns_provider_limit")
			return
		}
		item, err = s.Store.Resource.Create().SetKind("dns-provider").SetKey(randomToken(16)).SetData(data).Save(c.Request.Context())
	}
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	s.audit(c, "dns-provider.save", fmt.Sprint(item.ID), "ok")
	c.JSON(200, gin.H{"id": item.ID, "ok": true})
}
func (s *Server) deleteDNSProvider(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	id, err := strconv.Atoi(c.Param("provider"))
	if err != nil || id <= 0 {
		failure(c, 400, "invalid_dns_provider")
		return
	}
	result, err := s.DB.ExecContext(c.Request.Context(), `DELETE FROM migate.resources WHERE id=$1 AND kind='dns-provider' AND NOT EXISTS (SELECT 1 FROM migate.resources AS jobs WHERE jobs.kind='certificate-order' AND jobs.data->>'provider_id'=$2 AND jobs.data->>'status' IN ('pending','running','deploying')) AND NOT EXISTS (SELECT 1 FROM migate.resources challenges WHERE challenges.kind='dns-challenge' AND challenges.data->>'provider_id'=$2 AND challenges.data->>'status'<>'cleaned')`, id, strconv.Itoa(id))
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		failure(c, 409, "dns_provider_in_use_or_missing")
		return
	}
	s.audit(c, "dns-provider.delete", fmt.Sprint(id), "ok")
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) listCertificateOrders(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		failure(c, 400, "invalid_device")
		return
	}
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 1000000 {
		failure(c, 400, "invalid_page")
		return
	}
	size, err := strconv.Atoi(c.DefaultQuery("page_size", "200"))
	if err != nil || size < 1 || size > 200 {
		failure(c, 400, "invalid_page_size")
		return
	}
	var total int
	if err = s.DB.QueryRowContext(c.Request.Context(), `SELECT count(*) FROM migate.resources WHERE kind='certificate-order' AND data->>'device_id'=$1`, strconv.Itoa(id)).Scan(&total); err != nil {
		failure(c, 503, "query_failed")
		return
	}
	rows, err := s.DB.QueryContext(c.Request.Context(), `SELECT id,data FROM migate.resources WHERE kind='certificate-order' AND data->>'device_id'=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`, strconv.Itoa(id), size, (page-1)*size)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int
		var raw []byte
		if rows.Scan(&id, &raw) != nil {
			failure(c, 503, "query_failed")
			return
		}
		var data map[string]any
		if json.Unmarshal(raw, &data) != nil {
			failure(c, 503, "query_failed")
			return
		}
		view := gin.H{"id": id}
		for _, key := range []string{"device_id", "certificate_id", "name", "domains", "email", "provider_id", "staging", "status", "attempts", "last_error", "started_at", "finished_at", "delivery_task_id", "auto_renew"} {
			view[key] = data[key]
		}
		out = append(out, view)
	}
	if rows.Err() != nil {
		failure(c, 503, "query_failed")
		return
	}
	c.JSON(200, gin.H{"items": out, "total": total, "page": page, "page_size": size})
}
func (s *Server) createCertificateOrder(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		failure(c, 400, "invalid_device")
		return
	}
	var req struct {
		ProviderID    int      `json:"provider_id"`
		CertificateID int      `json:"certificate_id"`
		Name          string   `json:"name"`
		Domains       []string `json:"domains"`
		Email         string   `json:"email"`
		Staging       bool     `json:"staging"`
		Confirm       bool     `json:"confirm"`
		AutoRenew     bool     `json:"auto_renew"`
	}
	if !bind(c, &req) {
		return
	}
	domains, err := certservice.ValidateDNS01Request(certservice.IssueRequest{Domains: req.Domains, Email: req.Email})
	if err != nil || len(req.Name) > 100 || strings.TrimSpace(req.Name) == "" || !req.Confirm {
		failure(c, 400, "invalid_dns01_order")
		return
	}
	d, err := s.Store.Device.Get(c.Request.Context(), id)
	if err != nil || d.Status != "approved" {
		failure(c, 404, "device_not_found")
		return
	}
	capable := false
	caps, _ := d.Metadata["capabilities"].([]any)
	for _, cap := range caps {
		capable = capable || cap == "certificate.delivery.v1"
	}
	if !capable {
		failure(c, 400, "agent_capability_required")
		return
	}
	provider, err := s.Store.Resource.Get(c.Request.Context(), req.ProviderID)
	if err != nil || provider.Kind != "dns-provider" {
		failure(c, 400, "dns_credentials_required")
		return
	}
	if req.CertificateID != 0 {
		exists, e := s.Store.Resource.Query().Where(resource.KindEQ("device-certificate"), resource.KeyEQ(fmt.Sprintf("%d:%d", id, req.CertificateID))).Exist(c.Request.Context())
		if e != nil || !exists {
			failure(c, 404, "certificate_not_found")
			return
		}
	}
	data := map[string]any{"device_id": id, "certificate_id": req.CertificateID, "name": req.Name, "domains": domains, "email": req.Email, "provider_id": provider.ID, "staging": req.Staging, "status": "pending", "attempts": 0, "auto_renew": req.AutoRenew}
	tx, err := s.DB.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(c.Request.Context(), `SELECT pg_advisory_xact_lock($1)`, int64(8400000000)+int64(id)); err != nil {
		failure(c, 503, "save_failed")
		return
	}
	var active bool
	if err = tx.QueryRowContext(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM migate.resources WHERE kind='certificate-order' AND data->>'device_id'=$1 AND data->>'status' IN ('pending','running','deploying'))`, strconv.Itoa(id)).Scan(&active); err != nil {
		failure(c, 503, "save_failed")
		return
	}
	if active {
		failure(c, 409, "certificate_order_active")
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		failure(c, 400, "invalid_dns01_order")
		return
	}
	var jobID int
	if err = tx.QueryRowContext(c.Request.Context(), `INSERT INTO migate.resources(kind,key,owner_id,data,created_at) VALUES('certificate-order',$1,0,$2,$3) RETURNING id`, randomToken(16), raw, time.Now()).Scan(&jobID); err != nil {
		failure(c, 503, "save_failed")
		return
	}
	if err = tx.Commit(); err != nil {
		failure(c, 503, "save_failed")
		return
	}
	s.audit(c, "certificate.dns01.queue", fmt.Sprint(jobID), "pending")
	c.JSON(202, gin.H{"task_id": jobID, "status": "pending"})
}
func (s *Server) retryCertificateOrder(c *gin.Context) {
	id, e := strconv.Atoi(c.Param("id"))
	order, err := strconv.Atoi(c.Param("order"))
	if e != nil || err != nil {
		failure(c, 400, "invalid_order")
		return
	}
	status, err := s.queueCertificateRetry(c.Request.Context(), id, order)
	if errors.Is(err, errCertificateRetryConflict) {
		failure(c, 409, "order_not_retryable")
		return
	}
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	s.audit(c, "certificate.dns01.retry", fmt.Sprint(order), status)
	c.JSON(200, gin.H{"task_id": order, "status": status})
}
func (s *Server) acmeAccount(ctx context.Context, staging bool) (*ecdsa.PrivateKey, error) {
	name := "acme-account-production"
	if staging {
		name = "acme-account-staging"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var data struct {
		Key string `json:"key"`
	}
	stored, readErr := s.Store.Resource.Query().Where(resource.KindEQ("settings"), resource.KeyEQ(name)).Only(ctx)
	if readErr != nil && !ent.IsNotFound(readErr) {
		return nil, readErr
	}
	if readErr == nil {
		raw, e := json.Marshal(stored.Data)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &data); e != nil {
			return nil, e
		}
	}
	if ent.IsNotFound(readErr) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, err
		}
		sealed, err := s.Crypto.Encrypt(string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})))
		if err != nil {
			return nil, err
		}
		data.Key = sealed
		if _, err = s.Store.Resource.Create().SetKind("settings").SetKey(name).SetData(map[string]any{"key": sealed}).Save(ctx); err != nil {
			stored, e := s.Store.Resource.Query().Where(resource.KindEQ("settings"), resource.KeyEQ(name)).Only(ctx)
			if e != nil {
				return nil, e
			}
			data.Key = text(stored.Data["key"])
		}
	}
	plain, err := s.Crypto.Decrypt(data.Key)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode([]byte(plain))
	if block == nil {
		return nil, errors.New("account_key_invalid")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}
func (s *Server) certificateWorker(ctx context.Context) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.configMu.RLock()
			installed := s.Config.Installed
			s.configMu.RUnlock()
			if !installed {
				continue
			}
			after := 0
			for ctx.Err() == nil {
				jobs, err := s.Store.Resource.Query().Where(resource.KindEQ("certificate-order"), resource.IDGT(after)).Order(ent.Asc("id")).Limit(100).All(ctx)
				if err != nil || len(jobs) == 0 {
					break
				}
				for _, job := range jobs {
					after = job.ID
					s.processCertificateOrder(ctx, job)
				}
			}
			s.recoverDNSChallenges(ctx)
			s.scheduleCertificateRenewals(ctx, time.Now())
		}
	}
}
func (s *Server) processCertificateOrder(ctx context.Context, job *ent.Resource) {
	status := text(job.Data["status"])
	if status == "deploying" {
		t, err := s.Store.Task.Get(ctx, int(number(job.Data["delivery_task_id"])))
		if err != nil {
			return
		}
		if t.Status == "applied" || t.Status == "failed" || time.Now().After(t.ExpiresAt) {
			data := job.Data
			data["status"] = t.Status
			if t.Status != "applied" {
				data["status"] = "failed"
			}
			data["finished_at"] = time.Now().UTC().Format(time.RFC3339)
			if t.Status != "applied" {
				data["last_error"] = "certificate_delivery_failed"
			}
			tx, err := s.Store.Tx(ctx)
			if err != nil {
				return
			}
			defer tx.Rollback()
			if t.Status == "applied" {
				if err = saveCertificatePolicy(ctx, tx, job); err != nil {
					return
				}
			}
			if _, err = tx.Resource.UpdateOne(job).SetData(data).Save(ctx); err != nil {
				return
			}
			tx.Commit()
		}
		return
	}
	if status == "running" {
		started, err := time.Parse(time.RFC3339, text(job.Data["started_at"]))
		if err != nil || time.Since(started) > 15*time.Minute {
			_, _ = s.DB.ExecContext(ctx, `UPDATE migate.resources SET data=data||'{"status":"failed","last_error":"dns01_interrupted_retry_required"}'::jsonb WHERE id=$1 AND data->>'status'='running'`, job.ID)
		}
		return
	}
	if status != "pending" {
		return
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE migate.resources SET data=data||jsonb_build_object('status','running','started_at',$1::text,'attempts',COALESCE((data->>'attempts')::int,0)+1) WHERE id=$2 AND data->>'status'='pending'`, time.Now().UTC().Format(time.RFC3339), job.ID)
	if err != nil {
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return
	}
	job, err = s.Store.Resource.Get(ctx, job.ID)
	if err != nil {
		return
	}
	work, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	failureCode := "dns01_configuration_unavailable"
	err = s.issueCertificateOrder(work, job)
	if err == nil {
		return
	}
	if strings.HasPrefix(err.Error(), "dns01_") {
		failureCode = err.Error()
	}
	// The issuer and provider return bounded error codes, never remote response bodies.
	data := job.Data
	data["status"] = "failed"
	data["last_error"] = failureCode
	data["finished_at"] = time.Now().UTC().Format(time.RFC3339)
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer saveCancel()
	s.Store.Resource.UpdateOne(job).SetData(data).Save(saveCtx)
}
func (s *Server) issueCertificateOrder(ctx context.Context, job *ent.Resource) error {
	d, err := s.Store.Device.Get(ctx, int(number(job.Data["device_id"])))
	if err != nil || d.Status != "approved" {
		return errors.New("dns01_device_unavailable")
	}
	provider, err := s.Store.Resource.Get(ctx, int(number(job.Data["provider_id"])))
	if err != nil || provider.Kind != "dns-provider" {
		return errors.New("dns01_credentials_required")
	}
	zone := text(provider.Data["zone_id"])
	plain, err := s.Crypto.Decrypt(text(provider.Data["token"]))
	prefix := "dns-provider:" + zone + ":"
	if err != nil || !strings.HasPrefix(plain, prefix) {
		return errors.New("dns01_credentials_required")
	}
	account, err := s.acmeAccount(ctx, job.Data["staging"] == true)
	if err != nil {
		return errors.New("dns01_account_key_unavailable")
	}
	domains := []string{}
	if list, ok := job.Data["domains"].([]any); ok {
		for _, value := range list {
			domains = append(domains, text(value))
		}
	}
	issuer := certservice.DNS01Issuer{AccountKey: account, Provider: durableTXTProvider{server: s, job: job, provider: provider}, Staging: job.Data["staging"] == true}
	issued, err := issuer.Issue(ctx, certservice.IssueRequest{Domains: domains, Email: text(job.Data["email"])})
	if err != nil {
		return err
	}
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Recheck revocation after the potentially long CA interaction.
	d, err = tx.Device.Get(ctx, d.ID)
	if err != nil || d.Status != "approved" {
		return errors.New("dns01_device_unavailable")
	}
	certificateID := int(number(job.Data["certificate_id"]))
	if certificateID == 0 {
		certificateID = 1000000000 + job.ID
	}
	sealed, err := s.Crypto.Encrypt(fmt.Sprintf("certificate:%d:%d:", d.ID, certificateID) + string(issued.KeyPEM))
	if err != nil {
		return err
	}
	payload := map[string]any{"id": certificateID, "name": text(job.Data["name"]), "certificate_pem": string(issued.CertPEM), "sealed_key": sealed}
	t, err := tx.Task.Create().SetDeviceID(d.ID).SetOperation("certificate.upsert").SetKey(fmt.Sprintf("dns01:%d:%d", job.ID, int(number(job.Data["attempts"])))).SetPayload(payload).SetExpiresAt(time.Now().Add(24 * time.Hour)).SetLeasedUntil(time.Unix(0, 0)).Save(ctx)
	if err != nil {
		return err
	}
	if _, err = tx.Task.UpdateOne(t).SetRevision(int64(t.ID)).Save(ctx); err != nil {
		return err
	}
	data := job.Data
	data["status"] = "deploying"
	data["delivery_task_id"] = t.ID
	data["certificate_id"] = certificateID
	delete(data, "last_error")
	if _, err = tx.Resource.UpdateOne(job).SetData(data).Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
}
