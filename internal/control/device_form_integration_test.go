package control

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imzyb/MiGate/ent/task"
)

func testDeviceFormIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin string) {
	t.Helper()
	ctx := context.Background()
	d, err := s.Store.Device.Create().SetName("device-form-fixture").SetPublicKey("device-form-fixture").SetPairExpires(time.Now()).SetStatus("approved").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Device.DeleteOneID(d.ID).Exec(ctx)
	defer s.Store.Task.Delete().Where(task.DeviceIDEQ(d.ID)).Exec(ctx)
	unsupported := request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID), map[string]any{"operation": "core.stop", "payload": map[string]any{"core": "xray"}, "confirm": true}, "", admin)
	if unsupported.Code != 400 {
		t.Fatal("core control sent to old Agent")
	}
	inspectPath := fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID)
	if w := request("POST", inspectPath, map[string]any{"operation": "core.inspect", "payload": map[string]any{"core": "xray"}, "confirm": true}, "", admin); w.Code != 400 {
		t.Fatal("diagnostics sent to old Agent")
	}
	for _, payload := range []map[string]any{{"core": "xray", "version": "26.10.17"}, {"core": "xray", "url": "https://example.org/core"}} {
		w := request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID), map[string]any{"operation": "core.install", "payload": payload, "confirm": true}, "", admin)
		if w.Code != 400 {
			t.Fatal("version installation sent to old Agent")
		}
	}
	if _, err := s.Store.Device.UpdateOne(d).SetMetadata(map[string]any{"capabilities": []any{"core.control.v1", "core.versions.v1", "core.diagnostics.v1"}}).Save(ctx); err != nil {
		t.Fatal(err)
	}
	post := func(op string, payload map[string]any) map[string]any {
		t.Helper()
		w := request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID), map[string]any{"operation": op, "payload": payload, "confirm": true}, "", admin)
		if w.Code != 202 {
			t.Fatalf("form task %s: %d", op, w.Code)
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	a := post("inbound.upsert", map[string]any{"protocol": "vless", "security": "reality", "port": 24446})
	post("core.inspect", map[string]any{"core": "xray"})
	for _, payload := range []map[string]any{{"core": "x-ui"}, {"core": "xray", "path": "/etc/passwd"}, {"core": "xray", "command": "restart"}} {
		if w := request("POST", inspectPath, map[string]any{"operation": "core.inspect", "payload": payload, "confirm": true}, "", admin); w.Code != 400 {
			t.Fatal("unsafe diagnostics payload accepted")
		}
	}
	post("core.install", map[string]any{"core": "xray", "version": "26.10.17"})
	for _, payload := range []map[string]any{{"core": "xray", "version": "../escape"}, {"core": "xray", "version": "26.10.17", "url": "https://example.org/core"}, {"core": "x-ui", "version": "26.10.17"}} {
		w := request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID), map[string]any{"operation": "core.install", "payload": payload, "confirm": true}, "", admin)
		if w.Code != 400 {
			t.Fatal("unsafe core version payload accepted")
		}
	}
	post("core.stop", map[string]any{"core": "xray"})
	b := post("inbound.upsert", map[string]any{"protocol": "vless", "security": "reality", "port": 24447})
	if a["resource_id"].(float64) <= 0 || a["resource_id"] == b["resource_id"] {
		t.Fatal("auto resource ID collision")
	}
	queued, err := s.Store.Task.Get(ctx, int(a["id"].(float64)))
	if err != nil {
		t.Fatal(err)
	}
	private, e := base64.RawURLEncoding.DecodeString(text(queued.Payload["reality_private_key"]))
	if e != nil {
		t.Fatal(e)
	}
	key, e := ecdh.X25519().NewPrivateKey(private)
	if e != nil || base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()) != queued.Payload["reality_public_key"] {
		t.Fatal("Reality generated key mismatch")
	}
	if len(text(queued.Payload["reality_short_id"])) != 8 {
		t.Fatal("invalid generated short ID")
	}
	r, err := s.Store.Resource.Create().SetKind("device-inbound").SetOwnerID(d.ID).SetKey(fmt.Sprintf("%d:77", d.ID)).SetData(map[string]any{"id": 77, "reality_private_key": "fixture-never-to-browser", "password": "fixture-password", "protocol": "vless", "reality_public_key": "public-fixture", "reality_short_id": "12345678"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOneID(r.ID).Exec(ctx)
	w := request("GET", fmt.Sprintf("/api/v1/admin/resources/device-inbound?owner_id=%d", d.ID), nil, "", admin)
	if w.Code != 200 || strings.Contains(w.Body.String(), "fixture-never-to-browser") || strings.Contains(w.Body.String(), "fixture-password") || !strings.Contains(w.Body.String(), `"has_reality_private_key":true`) {
		t.Fatal("device resource secret exposure")
	}
	update := post("inbound.upsert", map[string]any{"id": 77, "reality_private_key": "", "password": "", "protocol": "vless"})
	retained, _ := s.Store.Task.Get(ctx, int(update["id"].(float64)))
	if retained.Payload["reality_private_key"] != "fixture-never-to-browser" || retained.Payload["password"] != "fixture-password" || retained.Payload["reality_public_key"] != "public-fixture" || retained.Payload["reality_short_id"] != "12345678" {
		t.Fatal("empty edit did not retain secret")
	}
	stateBody := map[string]any{"operation": "inbound.upsert", "confirm": true, "payload": map[string]any{"id": 77, "protocol": "vless", "enabled": false}}
	if w := request("POST", inspectPath, stateBody, "", admin); w.Code != 400 {
		t.Fatal("inbound state sent to old Agent")
	}
	s.Store.Device.UpdateOne(d).SetMetadata(map[string]any{"capabilities": []string{"inbound.enabled.v1"}}).SaveX(ctx)
	disabled := post("inbound.upsert", stateBody["payload"].(map[string]any))
	saved := s.Store.Task.GetX(ctx, int(disabled["id"].(float64)))
	if saved.Payload["enabled"] != false {
		t.Fatal("inbound disabled state lost")
	}
	r.Data["enabled"] = false
	s.Store.Resource.UpdateOne(r).SetData(r.Data).SaveX(ctx)
	omitted := post("inbound.upsert", map[string]any{"id": 77, "protocol": "vless"})
	if s.Store.Task.GetX(ctx, int(omitted["id"].(float64))).Payload["enabled"] != false {
		t.Fatal("legacy edit reenabled disabled inbound")
	}
	stateBody["payload"].(map[string]any)["enabled"] = "false"
	if w := request("POST", inspectPath, stateBody, "", admin); w.Code != 400 {
		t.Fatal("inbound nonboolean state accepted")
	}
	cert := post("certificate.upsert", map[string]any{"certificate_pem": "public fixture", "key_pem": "private fixture"})
	sealed, _ := s.Store.Task.Get(ctx, int(cert["id"].(float64)))
	plain, e := s.Crypto.Decrypt(text(sealed.Payload["sealed_key"]))
	if e != nil || plain != fmt.Sprintf("certificate:%d:%d:private fixture", d.ID, int(cert["resource_id"].(float64))) || sealed.Payload["key_pem"] != nil {
		t.Fatal("auto certificate ID sealing mismatch")
	}
}
