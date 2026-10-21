package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testDNS01Contracts(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	call := func(method, url string, body any, cookie string, status int) map[string]any {
		w := request(method, "/api/v1/admin"+url, body, "", cookie)
		if w.Code != status {
			t.Fatalf("DNS01 %s HTTP %d want %d", url, w.Code, status)
		}
		if strings.Contains(w.Body.String(), "private-dns-token") {
			t.Fatal("DNS credential returned")
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	call("GET", "/dns-providers", nil, alice, 403)
	call("GET", "/dns-providers", nil, "", 401)
	body := map[string]any{"name": "test-provider", "zone_id": strings.Repeat("a", 32), "token": "private-dns-token", "password": "wrong"}
	call("POST", "/dns-providers", body, admin, 401)
	body["password"] = "Local1-testing-password"
	saved := call("POST", "/dns-providers", body, admin, 200)
	providerID := int(number(saved["id"]))
	defer s.Store.Resource.DeleteOneID(providerID).Exec(ctx)
	stored, err := s.Store.Resource.Get(ctx, providerID)
	if err != nil || text(stored.Data["token"]) == "private-dns-token" {
		t.Fatal("DNS token not encrypted")
	}
	listing := call("GET", "/dns-providers", nil, admin, 200)
	if len(listing["items"].([]any)) != 1 {
		t.Fatal("provider listing")
	}
	body["id"] = providerID
	body["token"] = ""
	call("POST", "/dns-providers", body, admin, 200)
	again, _ := s.Store.Resource.Get(ctx, providerID)
	if text(again.Data["token"]) != text(stored.Data["token"]) {
		t.Fatal("blank token erased credential")
	}
	d, err := s.Store.Device.Create().SetName("dns-fixture").SetPublicKey("dns-fixture").SetStatus("approved").SetPairExpires(time.Now()).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Device.DeleteOne(d).Exec(ctx)
	path := fmt.Sprintf("/devices/%d/certificate-orders", d.ID)
	order := map[string]any{"provider_id": providerID, "name": "test-cert", "domains": []string{"example.com"}, "email": "a@example.com", "staging": true, "confirm": true}
	call("POST", path, order, admin, 400)
	s.Store.Device.UpdateOne(d).SetMetadata(map[string]any{"capabilities": []string{"certificate.delivery.v1"}}).Save(ctx)
	call("POST", path, order, alice, 403)
	order["domains"] = []string{"127.0.0.1"}
	call("POST", path, order, admin, 400)
	order["domains"] = []string{"example.com"}
	queued := call("POST", path, order, admin, 202)
	jobID := int(number(queued["task_id"]))
	defer s.Store.Resource.DeleteOneID(jobID).Exec(ctx)
	call("POST", path, order, admin, 409)
	call("DELETE", fmt.Sprintf("/dns-providers/%d", providerID), map[string]any{"password": "Local1-testing-password"}, admin, 409)
	// Simulate lost configuration inside the isolated database. No CA/DNS request.
	s.Store.Resource.DeleteOneID(providerID).Exec(ctx)
	job, _ := s.Store.Resource.Get(ctx, jobID)
	s.processCertificateOrder(ctx, job)
	job, _ = s.Store.Resource.Get(ctx, jobID)
	if job.Data["status"] != "failed" || job.Data["last_error"] != "dns01_credentials_required" {
		t.Fatal("missing credentials did not persist failure")
	}
	call("POST", path+fmt.Sprintf("/%d/retry", jobID), nil, admin, 200)
	job, _ = s.Store.Resource.Get(ctx, jobID)
	s.processCertificateOrder(ctx, job)
	job, _ = s.Store.Resource.Get(ctx, jobID)
	if number(job.Data["attempts"]) != 2 {
		t.Fatal("retry attempt not persisted")
	}
	out := call("GET", path, nil, admin, 200)
	if len(out["items"].([]any)) != 1 {
		t.Fatal("job not visible")
	}
	call("GET", path+"?page=0", nil, admin, 400)
	call("GET", path+"?page_size=201", nil, admin, 400)
	page := call("GET", path+"?page=2&page_size=1", nil, admin, 200)
	if number(page["total"]) != 1 || len(page["items"].([]any)) != 0 {
		t.Fatal("certificate order pagination lost total or offset")
	}
	// Persisted running work interrupted by restart becomes failed, not success.
	job.Data["status"] = "running"
	job.Data["started_at"] = time.Now().Add(-20 * time.Minute).UTC().Format(time.RFC3339)
	s.Store.Resource.UpdateOne(job).SetData(job.Data).Save(ctx)
	s.processCertificateOrder(ctx, job)
	job, _ = s.Store.Resource.Get(ctx, jobID)
	if job.Data["last_error"] != "dns01_interrupted_retry_required" {
		t.Fatal("interrupted work not marked failed")
	}
}
