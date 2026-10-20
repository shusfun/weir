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

func testCertificateHistoryIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	d, err := s.Store.Device.Create().SetName("certificate-history-fixture").SetPublicKey(randomToken(32)).SetPairExpires(time.Now()).SetStatus("approved").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Device.DeleteOne(d).Exec(ctx)
	path := fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID)
	body := map[string]any{"operation": "certificate.clean", "confirm": true, "payload": map[string]any{"preview_digest": strings.Repeat("a", 64)}}
	if w := request("POST", path, body, "", alice); w.Code != 403 {
		t.Fatal("ordinary user can clean certs", w.Code)
	}
	if w := request("POST", path, body, "", admin); w.Code != 400 {
		t.Fatal("old agent accepts cert cleanup", w.Code)
	}
	if _, err = s.Store.Device.UpdateOne(d).SetMetadata(map[string]any{"capabilities": []string{"certificate.history.v1"}}).Save(ctx); err != nil {
		t.Fatal(err)
	}
	for _, payload := range []map[string]any{{"preview_digest": "bad"}, {"preview_digest": strings.Repeat("a", 64), "path": "/etc/x-ui"}} {
		bad := map[string]any{"operation": "certificate.clean", "confirm": true, "payload": payload}
		if w := request("POST", path, bad, "", admin); w.Code != 400 {
			t.Fatal("unsafe cleanup payload accepted", w.Code)
		}
	}
	w := request("POST", path, body, "", admin)
	var task struct{ ID int }
	if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &task) != nil || task.ID == 0 {
		t.Fatal("cert cleanup enqueue failed", w.Code)
	}
	defer s.Store.Task.DeleteOneID(task.ID).Exec(ctx)
	saved, err := s.Store.Task.Get(ctx, task.ID)
	if err != nil || saved.Status != "pending" || len(saved.Payload) != 1 {
		t.Fatal("queued cleanup reported applied or payload expanded")
	}
	t.Log("certificate cleanup admin-only, old Agent rejected, arbitrary path/extra payload rejected; compatible Agent queued task, not applied")
	old, err := s.Store.Resource.Create().SetKind("device-certificate").SetKey(fmt.Sprintf("%d:77", d.ID)).SetOwnerID(d.ID).SetData(map[string]any{"id": 77, "name": "Existing certificate"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(old).Exec(ctx)
	payload := map[string]any{"id": 77, "certificate_pem": "synthetic-public", "key_pem": "synthetic-private"}
	for _, name := range []any{true, strings.Repeat("a", 201), "bad\tname"} {
		payload["name"] = name
		if w := request("POST", path, map[string]any{"operation": "certificate.upsert", "confirm": true, "payload": payload}, "", admin); w.Code != 400 {
			t.Fatal("invalid certificate name accepted", w.Code)
		}
	}
	delete(payload, "name")
	w = request("POST", path, map[string]any{"operation": "certificate.upsert", "confirm": true, "payload": payload}, "", admin)
	if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
		t.Fatal("compatible old certificate update rejected", w.Code)
	}
	defer s.Store.Task.DeleteOneID(task.ID).Exec(ctx)
	saved, err = s.Store.Task.Get(ctx, task.ID)
	if err != nil || saved.Payload["name"] != "Existing certificate" || saved.Payload["key_pem"] != nil || text(saved.Payload["sealed_key"]) == "" {
		t.Fatal("old request lost stored certificate name or leaked key")
	}
	t.Log("certificate name type/length/control checks, omitted-name compatibility preservation and encrypted task key passed")
}
