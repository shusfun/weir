package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imzyb/MiGate/ent/task"
)

func testOutboundTransportIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, user string) {
	ctx := context.Background()
	d, err := s.Store.Device.Create().SetName("outbound-transport-fixture").SetPublicKey("outbound-transport-fixture").SetStatus("approved").SetPairExpires(time.Now()).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Device.DeleteOneID(d.ID).Exec(ctx)
	defer s.Store.Task.Delete().Where(task.DeviceIDEQ(d.ID)).Exec(ctx)
	endpoint := fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID)
	payload := map[string]any{"tag": "fixture", "protocol": "vmess", "address": "example.com", "port": 443, "username": "12345678-1234-1234-1234-123456789abc", "transport_options": map[string]any{"network": "ws", "security": "tls", "sni": "example.com", "path": "/ws", "host": "cdn.example.com"}}
	body := map[string]any{"operation": "outbound.upsert", "confirm": true, "payload": payload}
	if w := request("POST", endpoint, body, "", user); w.Code != 403 {
		t.Fatal("ordinary user queued outbound")
	}
	if w := request("POST", endpoint, body, "", admin); w.Code != 400 {
		t.Fatal("new transport sent to old Agent")
	}
	if _, err = s.Store.Device.UpdateOne(d).SetMetadata(map[string]any{"capabilities": []string{"outbound.transport.v1"}}).Save(ctx); err != nil {
		t.Fatal(err)
	}
	w := request("POST", endpoint, body, "", admin)
	if w.Code != 202 {
		t.Fatal("valid transport rejected", w.Code)
	}
	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	queued, err := s.Store.Task.Get(ctx, int(number(result["id"])))
	if err != nil {
		t.Fatal(err)
	}
	if queued.Payload["transport_options"] != nil || !strings.Contains(text(queued.Payload["settings_json"]), `"path":"/ws"`) {
		t.Fatal("transport not normalized for Agent")
	}
	r, err := s.Store.Resource.Create().SetKind("device-outbound").SetOwnerID(d.ID).SetKey(fmt.Sprintf("%d:88", d.ID)).SetData(map[string]any{"id": 88, "tag": "fixture", "protocol": "trojan", "address": "example.com", "port": 443, "password": "fixture-password-must-not-appear", "raw_link": "fixture-link-must-not-appear", "settings_json": `{"security":"tls","network":"grpc","service_name":"rpc","sni":"example.com"}`}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(r).Exec(ctx)
	w = request("GET", fmt.Sprintf("/api/v1/admin/resources/device-outbound?owner_id=%d", d.ID), nil, "", admin)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"service_name":"rpc"`) || strings.Contains(w.Body.String(), "must-not-appear") || strings.Contains(w.Body.String(), `"settings_json":`) {
		t.Fatal("public transport edit fields or secret redaction failed")
	}
	payload["id"] = 88
	payload["protocol"] = "trojan"
	payload["password"] = ""
	payload["transport_options"] = map[string]any{"network": "tcp", "security": "tls"}
	w = request("POST", endpoint, body, "", admin)
	if w.Code != 202 {
		t.Fatal("transport edit rejected")
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	queued, _ = s.Store.Task.Get(ctx, int(number(result["id"])))
	if queued.Payload["password"] != "fixture-password-must-not-appear" || strings.Contains(text(queued.Payload["settings_json"]), "service_name") {
		t.Fatal("edit lost password or retained removed transport")
	}

	payload["enabled"] = false
	if w = request("POST", endpoint, body, "", admin); w.Code != 400 {
		t.Fatal("enabled state sent to old Agent")
	}
	if _, err = s.Store.Device.UpdateOne(d).SetMetadata(map[string]any{"capabilities": []string{"outbound.transport.v1", "resource.enabled.v1"}}).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if w = request("POST", endpoint, body, "", admin); w.Code != 202 {
		t.Fatal("capable Agent state rejected")
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	queued, _ = s.Store.Task.Get(ctx, int(number(result["id"])))
	if queued.Payload["enabled"] != false {
		t.Fatal("disabled state lost")
	}
	payload["enabled"] = "false"
	if w = request("POST", endpoint, body, "", admin); w.Code != 400 {
		t.Fatal("invalid enabled type accepted")
	}
	delete(payload, "enabled")
	if _, err = s.Store.Resource.UpdateOne(r).SetData(map[string]any{"id": 88, "tag": "fixture", "protocol": "trojan", "password": "fixture-password-must-not-appear", "enabled": false}).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if w = request("POST", endpoint, body, "", admin); w.Code != 202 {
		t.Fatal("legacy disabled edit rejected")
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	queued, _ = s.Store.Task.Get(ctx, int(number(result["id"])))
	if queued.Payload["enabled"] != false {
		t.Fatal("legacy edit reenabled resource")
	}
	payload["transport_options"] = map[string]any{"security": "tls", "unknown_field": true}
	count, _ := s.Store.Task.Query().Where(task.DeviceIDEQ(d.ID)).Count(ctx)
	if w = request("POST", endpoint, body, "", admin); w.Code != 400 {
		t.Fatal("unknown transport field accepted")
	}
	if after, _ := s.Store.Task.Query().Where(task.DeviceIDEQ(d.ID)).Count(ctx); after != count {
		t.Fatal("rejected transport queued task")
	}
}
