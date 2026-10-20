package control

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/ent/task"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"
)

func testDNS01Renewal(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	d, err := s.Store.Device.Create().SetName("renewal-fixture").SetPublicKey("renewal-fixture").SetStatus("approved").SetPairExpires(now).SetMetadata(map[string]any{"capabilities": []string{"certificate.delivery.v1"}}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Device.DeleteOne(d).Exec(ctx)
	certID := 90001
	key := fmt.Sprintf("%d:%d", d.ID, certID)
	keyPair, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	expires := now.Add(10 * 24 * time.Hour)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Hour), NotAfter: expires, DNSNames: []string{"renewal.invalid"}}
	der, _ := x509.CreateCertificate(rand.Reader, leaf, leaf, &keyPair.PublicKey, keyPair)
	cert, err := s.Store.Resource.Create().SetKind("device-certificate").SetKey(key).SetOwnerID(d.ID).SetData(map[string]any{"id": certID, "certificate_pem": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(cert).Exec(ctx)
	policy, err := s.Store.Resource.Create().SetKind("certificate-policy").SetKey(key).SetData(map[string]any{"device_id": d.ID, "certificate_id": certID, "enabled": true, "domains": []string{"renewal.invalid"}, "email": "a@example.com", "provider_id": 999, "staging": true, "name": "renewal-fixture"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(policy).Exec(ctx)
	renewalKey := fmt.Sprintf("renew:%d:%d:%d", d.ID, certID, expires.Unix())
	defer s.Store.Resource.Delete().Where(resource.KindEQ("certificate-order"), resource.KeyEQ(renewalKey)).Exec(ctx)
	// Scheduling persists a single order; it never calls the external issuer.
	s.queueCertificateRenewal(ctx, policy, now)
	s.queueCertificateRenewal(ctx, policy, now)
	jobs, err := s.Store.Resource.Query().Where(resource.KindEQ("certificate-order"), resource.KeyEQ(renewalKey)).All(ctx)
	if err != nil || len(jobs) != 1 {
		t.Fatal("renewal scheduling duplicated order")
	}
	job := jobs[0]
	if int(number(job.Data["certificate_id"])) != certID || job.Data["auto_renew"] != true {
		t.Fatal("renewal changed certificate target")
	}
	job.Data["status"] = "failed"
	s.Store.Resource.UpdateOne(job).SetData(job.Data).Save(ctx)
	s.queueCertificateRenewal(ctx, policy, now)
	count, _ := s.Store.Resource.Query().Where(resource.KindEQ("certificate-order"), resource.KeyEQ(renewalKey)).Count(ctx)
	if count != 1 {
		t.Fatal("failed renewal was automatically recreated")
	}
	path := fmt.Sprintf("/api/v1/admin/devices/%d/certificate-policies/%d", d.ID, certID)
	body := map[string]any{"enabled": false, "confirm": true}
	if w := request("PUT", path, body, "", alice); w.Code != 403 {
		t.Fatal("ordinary user changed renewal")
	}
	if w := request("PUT", path, body, "", admin); w.Code != 200 {
		t.Fatal("renewal toggle failed")
	}
	policy, _ = s.Store.Resource.Get(ctx, policy.ID)
	if policy.Data["enabled"] != false {
		t.Fatal("renewal disabled state not persisted")
	}
	// Simulate a scheduler that scanned before the administrator's update.
	if err := s.Store.Resource.DeleteOne(job).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	stale := *policy
	stale.Data = map[string]any{}
	for k, v := range policy.Data {
		stale.Data[k] = v
	}
	stale.Data["enabled"] = true
	s.queueCertificateRenewal(ctx, &stale, now)
	count, err = s.Store.Resource.Query().Where(resource.KindEQ("certificate-order"), resource.KeyEQ(renewalKey)).Count(ctx)
	if err != nil || count != 0 {
		t.Fatal("stale scheduler queued a disabled policy")
	}
	// An already submitted automatic renewal cannot re-enable a policy disabled
	// while CA work was running.
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	job.Data["renewal_policy_id"] = policy.ID
	if err = saveCertificatePolicy(ctx, tx, job); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	policy, _ = s.Store.Resource.Get(ctx, policy.ID)
	if policy.Data["enabled"] != false {
		t.Fatal("in-flight renewal re-enabled disabled policy")
	}
	// Delivery retries retain the issued certificate and encrypted key without
	// invoking the issuer (the fixture deliberately has no usable DNS provider).
	sealed, err := s.Crypto.Encrypt(fmt.Sprintf("certificate:%d:%d:", d.ID, certID) + "private-key-fixture")
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"id": certID, "name": "renewal-fixture", "certificate_pem": cert.Data["certificate_pem"], "sealed_key": sealed}
	delivery, err := s.Store.Task.Create().SetDeviceID(d.ID).SetOperation("certificate.upsert").SetKey("dns-delivery-failed-fixture").SetPayload(payload).SetStatus("failed").SetExpiresAt(now.Add(time.Hour)).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Task.DeleteOne(delivery).Exec(ctx)
	failed, err := s.Store.Resource.Create().SetKind("certificate-order").SetKey("dns-delivery-failed-fixture").SetData(map[string]any{"device_id": d.ID, "certificate_id": certID, "status": "failed", "last_error": "certificate_delivery_failed", "attempts": 1, "delivery_task_id": delivery.ID}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(failed).Exec(ctx)
	retryPath := fmt.Sprintf("/api/v1/admin/devices/%d/certificate-orders/%d/retry", d.ID, failed.ID)
	if w := request("POST", retryPath, nil, "", alice); w.Code != 403 {
		t.Fatal("ordinary user retried certificate delivery")
	}
	if w := request("POST", retryPath, nil, "", admin); w.Code != 200 {
		t.Fatalf("delivery retry HTTP %d", w.Code)
	}
	updated, err := s.Store.Resource.Get(ctx, failed.ID)
	if err != nil || updated.Data["status"] != "deploying" || number(updated.Data["attempts"]) != 2 {
		t.Fatal("delivery retry did not persist")
	}
	retried, err := s.Store.Task.Get(ctx, int(number(updated.Data["delivery_task_id"])))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Task.DeleteOne(retried).Exec(ctx)
	if retried.ID == delivery.ID || retried.DeviceID != d.ID || retried.Operation != "certificate.upsert" || retried.Payload["sealed_key"] != sealed || retried.Payload["certificate_pem"] != payload["certificate_pem"] || retried.Revision != int64(retried.ID) {
		t.Fatal("retry changed issued material or device binding")
	}
	if w := request("POST", retryPath, nil, "", admin); w.Code != 409 {
		t.Fatal("duplicate delivery retry accepted")
	}
	count, err = s.Store.Task.Query().Where(task.DeviceIDEQ(d.ID)).Count(ctx)
	if err != nil || count != 2 {
		t.Fatal("duplicate delivery task created")
	}
	updated.Data["status"], updated.Data["last_error"] = "failed", "certificate_delivery_failed"
	s.Store.Resource.UpdateOne(updated).SetData(updated.Data).Save(ctx)
	s.Store.Device.UpdateOne(d).SetStatus("revoked").Save(ctx)
	if w := request("POST", retryPath, nil, "", admin); w.Code != 409 {
		t.Fatal("revoked device accepted retry")
	}
}
