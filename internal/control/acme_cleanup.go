package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	certservice "github.com/imzyb/MiGate/internal/service/cert"
)

type durableTXTProvider struct {
	server   *Server
	job      *ent.Resource
	provider *ent.Resource
	client   *http.Client
}

func (p durableTXTProvider) Present(ctx context.Context, name, value string) (func(context.Context) error, error) {
	key := randomToken(24)
	sealed, err := p.server.Crypto.Encrypt(value)
	if err != nil {
		return nil, errors.New("dns01_ledger_unavailable")
	}
	// Commit intent before the external POST. The ownership marker recovers an
	// ID lost to process death or a provider response timeout.
	data := map[string]any{"job_id": p.job.ID, "device_id": p.job.Data["device_id"], "provider_id": p.provider.ID, "zone_id": p.provider.Data["zone_id"], "token": p.provider.Data["token"], "value": sealed, "name": name, "marker": "migate-dns01:" + key, "status": "pending", "retry_after": time.Now().Add(15 * time.Minute).Unix(), "attempts": 0}
	item, err := p.server.Store.Resource.Create().SetKind("dns-challenge").SetKey(key).SetData(data).Save(ctx)
	if err != nil {
		return nil, errors.New("dns01_ledger_unavailable")
	}
	provider, expected, err := p.server.challengeMaterial(item, p.client)
	if err != nil {
		return nil, err
	}
	id, err := provider.CreateOwnedTXT(ctx, expected)
	if err != nil {
		return nil, errors.New("dns01_present_failed")
	}
	data["record_id"] = id
	data["status"] = "presented"
	if _, err = p.server.Store.Resource.UpdateOne(item).SetData(data).Save(ctx); err != nil {
		// The durable intent remains available even if this attempt cannot save ID.
		clean, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		expected.ID = id
		_ = provider.DeleteOwnedTXT(clean, expected)
		return nil, errors.New("dns01_ledger_unavailable")
	}
	return func(clean context.Context) error { return p.server.cleanupDNSChallenge(clean, item.ID, p.client) }, nil
}

func (s *Server) challengeMaterial(item *ent.Resource, client *http.Client) (certservice.CloudflareDNS, certservice.OwnedTXT, error) {
	var provider certservice.CloudflareDNS
	var expected certservice.OwnedTXT
	if item.Kind != "dns-challenge" {
		return provider, expected, errors.New("dns01_cleanup_identity_invalid")
	}
	zone := text(item.Data["zone_id"])
	plain, err := s.Crypto.Decrypt(text(item.Data["token"]))
	prefix := "dns-provider:" + zone + ":"
	if err != nil || !strings.HasPrefix(plain, prefix) || !dnsZoneID.MatchString(zone) {
		return provider, expected, errors.New("dns01_cleanup_credentials_unavailable")
	}
	value, err := s.Crypto.Decrypt(text(item.Data["value"]))
	if err != nil {
		return provider, expected, errors.New("dns01_cleanup_material_unavailable")
	}
	provider = certservice.CloudflareDNS{Token: strings.TrimPrefix(plain, prefix), ZoneID: zone, Client: client}
	expected = certservice.OwnedTXT{ID: text(item.Data["record_id"]), Type: "TXT", Name: text(item.Data["name"]), Content: value, Comment: text(item.Data["marker"])}
	if expected.Comment != "migate-dns01:"+item.Key {
		return provider, expected, errors.New("dns01_cleanup_identity_invalid")
	}
	return provider, expected, nil
}

func (s *Server) cleanupDNSChallenge(ctx context.Context, id int, client *http.Client) error {
	lock, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer lock.Rollback()
	var acquired bool
	if err = lock.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(69443000,$1)`, id).Scan(&acquired); err != nil || !acquired {
		return errors.New("dns01_cleanup_busy")
	}
	item, err := s.Store.Resource.Get(ctx, id)
	if err != nil {
		return err
	}
	if item.Kind != "dns-challenge" {
		return errors.New("dns01_cleanup_identity_invalid")
	}
	if item.Data["status"] == "cleaned" {
		return nil
	}
	original, err := json.Marshal(item.Data)
	if err != nil {
		return err
	}
	provider, expected, cleanupErr := s.challengeMaterial(item, client)
	if cleanupErr == nil {
		ids := []string{expected.ID}
		if expected.ID == "" {
			ids, cleanupErr = provider.FindOwnedTXT(ctx, expected)
		}
		if cleanupErr == nil {
			for _, recordID := range ids {
				expected.ID = recordID
				if cleanupErr = provider.DeleteOwnedTXT(ctx, expected); cleanupErr != nil {
					break
				}
			}
		}
	}
	data := item.Data
	data["attempts"] = number(data["attempts"]) + 1
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	if cleanupErr == nil {
		data["status"] = "cleaned"
		delete(data, "token")
		delete(data, "value")
		delete(data, "last_error")
		data["cleaned_at"] = time.Now().UTC().Format(time.RFC3339)
	} else {
		data["status"] = "cleanup_failed"
		data["last_error"] = "dns01_cleanup_failed"
		data["retry_after"] = time.Now().Add(5 * time.Minute).Unix()
	}
	save, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updated, err := json.Marshal(data)
	if err != nil {
		return err
	}
	result, err := s.DB.ExecContext(save, `UPDATE migate.resources SET data=$1 WHERE id=$2 AND kind='dns-challenge' AND data=$3`, updated, item.ID, original)
	if err != nil {
		return errors.New("dns01_cleanup_ledger_unavailable")
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return errors.New("dns01_cleanup_ledger_changed")
	}
	if cleanupErr != nil {
		return errors.New("dns01_cleanup_failed")
	}
	return nil
}

func (s *Server) recoverDNSChallenges(ctx context.Context) {
	s.recoverDNSChallengesWithClient(ctx, nil)
}

func (s *Server) recoverDNSChallengesWithClient(ctx context.Context, client *http.Client) {
	// Select due rows in SQL; old cleaned history cannot starve pending cleanup.
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id FROM migate.resources c WHERE c.kind='dns-challenge' AND c.data->>'status'<>'cleaned' AND (c.data->>'retry_after')::bigint<=$1 AND NOT EXISTS (SELECT 1 FROM migate.resources j WHERE j.kind='certificate-order' AND j.id=(c.data->>'job_id')::bigint AND j.data->>'status'='running' AND j.data->>'started_at'>=$2) ORDER BY c.id LIMIT 100`, time.Now().Unix(), time.Now().Add(-15*time.Minute).UTC().Format(time.RFC3339))
	if err != nil {
		return
	}
	ids := []int{}
	for rows.Next() {
		var id int
		if rows.Scan(&id) != nil {
			rows.Close()
			return
		}
		ids = append(ids, id)
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		work, cancel := context.WithTimeout(ctx, 45*time.Second)
		_ = s.cleanupDNSChallenge(work, id, client)
		cancel()
	}
}

func (s *Server) listDNSChallenges(c *gin.Context) {
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
	ctx := c.Request.Context()
	var total int
	if err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM migate.resources WHERE kind='dns-challenge' AND data->>'device_id'=$1`, strconv.Itoa(id)).Scan(&total); err != nil {
		failure(c, 503, "query_failed")
		return
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,data,created_at FROM migate.resources WHERE kind='dns-challenge' AND data->>'device_id'=$1 ORDER BY id DESC LIMIT 20 OFFSET $2`, strconv.Itoa(id), (page-1)*20)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var rowID int
		var raw []byte
		var created time.Time
		var data map[string]any
		if rows.Scan(&rowID, &raw, &created) != nil || json.Unmarshal(raw, &data) != nil {
			failure(c, 503, "query_failed")
			return
		}
		items = append(items, gin.H{"id": rowID, "job_id": data["job_id"], "name": data["name"], "status": data["status"], "attempts": data["attempts"], "last_error": data["last_error"], "retry_after": data["retry_after"], "cleaned_at": data["cleaned_at"], "created_at": created})
	}
	if rows.Err() != nil {
		failure(c, 503, "query_failed")
		return
	}
	c.JSON(200, gin.H{"items": items, "total": total, "page": page, "page_size": 20})
}

func (s *Server) retryDNSCleanup(c *gin.Context) {
	device, err := strconv.Atoi(c.Param("id"))
	id, e := strconv.Atoi(c.Param("challenge"))
	if err != nil || e != nil || device <= 0 || id <= 0 {
		failure(c, 400, "invalid_order")
		return
	}
	if !s.rate(c, "dns-cleanup", 20) {
		return
	}
	result, err := s.DB.ExecContext(c.Request.Context(), `UPDATE migate.resources c SET data=c.data||jsonb_build_object('retry_after',$1::bigint) WHERE c.id=$2 AND c.kind='dns-challenge' AND c.data->>'device_id'=$3 AND c.data->>'status'<>'cleaned' AND NOT EXISTS (SELECT 1 FROM migate.resources j WHERE j.id=(c.data->>'job_id')::bigint AND j.kind='certificate-order' AND j.data->>'status' IN ('pending','running'))`, time.Now().Unix(), id, strconv.Itoa(device))
	if err != nil {
		failure(c, 503, "save_failed")
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		failure(c, 409, "dns_cleanup_not_retryable")
		return
	}
	s.audit(c, "certificate.dns01.cleanup", fmt.Sprint(id), "queued")
	c.JSON(202, gin.H{"status": "queued"})
}
