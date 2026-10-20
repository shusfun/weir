package control

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/internal/subscription"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testOutboundPools(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	d, err := s.Store.Device.Create().SetName("isolated-pool-device").SetPublicKey(randomToken(32)).SetStatus("approved").SetPairExpires(time.Now().Add(time.Minute)).SetMetadata(map[string]any{"capabilities": []any{"outbound.pool.v1"}}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Device.DeleteOne(d).Exec(ctx)
	d, err = s.Store.Device.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.Store.Resource.Create().SetKind("sources").SetKey(randomToken(16)).SetData(map[string]any{"name": "pool-source", "url": "https://example.com/pool", "enabled": true, "last_update": time.Now().UTC().Format(time.RFC3339), "links": []string{"http://u:pool-secret@example.com:8080#A", "http://u:pool-secret@example.com:8080#B", "trojan://synthetic@example.org:443?sni=example.org#C"}}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(src).Exec(ctx)
	base := fmt.Sprintf("/api/v1/admin/devices/%d/pools", d.ID)
	must := func(w *httptest.ResponseRecorder, status int) map[string]any {
		t.Helper()
		if w.Code != status {
			t.Fatalf("pool HTTP %d expected %d: %s", w.Code, status, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	must(request("GET", base, nil, "", alice), 403)
	pool := must(request("POST", base, map[string]any{"name": "Pool", "source_id": src.ID}, "", admin), 200)
	poolID := int(pool["id"].(float64))
	path := fmt.Sprintf("%s/%d", base, poolID)
	defer func() {
		s.Store.Resource.Delete().Where(resource.KindIn("outbound-pool", "pool-node-id", "device-outbound", "device-routing"), resource.OwnerIDEQ(d.ID)).Exec(ctx)
	}()
	preview := must(request("POST", path+"/preview", map[string]any{"version": 1}, "", admin), 200)
	if preview["duplicates"].(float64) != 1 || len(preview["items"].([]any)) != 2 {
		t.Fatal("pool did not deduplicate full connection identity")
	}
	b, _ := json.Marshal(preview)
	if strings.Contains(string(b), "pool-secret") {
		t.Fatal("pool preview leaked credentials")
	}
	selected := []string{preview["items"].([]any)[0].(map[string]any)["identity"].(string)}
	apply := map[string]any{"version": 1, "preview_token": preview["preview_token"], "selected": selected, "enabled": true, "confirm": true}
	manual, err := s.enqueue(ctx, d.ID, "routing.delete", randomToken(16), 1, map[string]any{"id": 123})
	if err != nil {
		t.Fatal(err)
	}
	must(request("POST", path+"/apply", apply, "", admin), 409)
	s.Store.Task.UpdateOne(manual).SetStatus("failed").Save(ctx)
	queued := must(request("POST", path+"/apply", apply, "", admin), 202)
	must(request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID), map[string]any{"operation": "routing.delete", "payload": map[string]any{"id": 123}, "confirm": true}, "", admin), 409)
	must(request("POST", path+"/apply", apply, "", admin), 409)
	must(request("POST", path+"/preview", map[string]any{"version": 2}, "", admin), 409)
	taskID := int(queued["id"].(float64))
	task, err := s.Store.Task.Get(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	// Apply an actual authenticated receipt through the same router as a Gate.
	agent := "pool-device-session"
	session, _ := json.Marshal(deviceSession{ID: d.ID, Epoch: d.PairExpires.UTC().Format(time.RFC3339Nano)})
	s.Redis.Set(ctx, "migate:device-token:"+digest(agent), session, time.Minute)
	s.Store.Task.UpdateOne(task).SetStatus("running").Save(ctx)
	receipt := map[string]any{"task_id": task.ID, "revision": task.Revision, "status": "applied", "data": map[string]any{"applied": true}}
	must(request("POST", "/api/v1/agent/results", receipt, agent, ""), 200)
	must(request("POST", "/api/v1/agent/results", receipt, agent, ""), 200)
	rows, err := s.Store.Resource.Query().Where(resource.KindEQ("device-outbound"), resource.OwnerIDEQ(d.ID)).All(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatal("pool receipt not atomic/idempotent", err)
	}
	memberID := int(number(rows[0].Data["id"]))
	listed := must(request("GET", fmt.Sprintf("/api/v1/admin/resources/device-outbound?owner_id=%d", d.ID), nil, "", admin), 200)
	view := listed["items"].([]any)[0].(map[string]any)["data"].(map[string]any)
	if view["username"] != nil || view["password"] != nil || view["settings_json"] != nil || view["raw_link"] != nil || view["has_username"] != true {
		t.Fatal("managed pool list exposed credentials")
	}
	must(request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID), map[string]any{"operation": "outbound.delete", "payload": map[string]any{"id": memberID}, "confirm": true}, "", admin), 409)
	// Change display names/order. The existing endpoint must retain its ID/tag.
	src, err = s.refreshSourceCache(ctx, src.ID, func(context.Context, string, string) (subscription.FetchedInventory, error) {
		return subscription.FetchedInventory{Body: []byte("trojan://synthetic@example.org:443?sni=example.org#C\nhttp://u:pool-secret@example.com:8080#Renamed")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	preview = must(request("POST", path+"/preview", map[string]any{"version": 2}, "", admin), 200)
	apply["version"], apply["preview_token"] = 2, preview["preview_token"]
	queued = must(request("POST", path+"/apply", apply, "", admin), 202)
	task, err = s.Store.Task.Get(ctx, int(queued["id"].(float64)))
	if err != nil {
		t.Fatal(err)
	}
	next := task.Payload["next"].([]any)[0].(map[string]any)
	if int(number(next["id"])) != memberID {
		t.Fatal("upstream reorder replaced endpoint identity")
	}
	s.Store.Task.UpdateOne(task).SetStatus("running").Save(ctx)
	receipt["task_id"], receipt["revision"], receipt["status"] = task.ID, task.Revision, "failed"
	must(request("POST", "/api/v1/agent/results", receipt, agent, ""), 200)
	rows, _ = s.Store.Resource.Query().Where(resource.KindEQ("device-outbound"), resource.OwnerIDEQ(d.ID)).All(ctx)
	if len(rows) != 1 || int(number(rows[0].Data["id"])) != memberID {
		t.Fatal("failed apply discarded existing members")
	}
	// Both active and dormant routing references prevent implicit deletion.
	route, err := s.Store.Resource.Create().SetKind("device-routing").SetOwnerID(d.ID).SetKey(fmt.Sprintf("%d:77", d.ID)).SetData(map[string]any{"id": 77, "outbound_id": memberID, "enabled": false}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	must(request("POST", path+"/apply", map[string]any{"version": 3, "enabled": false, "confirm": true}, "", admin), 409)
	s.Store.Resource.DeleteOne(route).Exec(ctx)
	queued = must(request("POST", path+"/apply", map[string]any{"version": 3, "enabled": false, "delete": true, "confirm": true}, "", admin), 202)
	task, _ = s.Store.Task.Get(ctx, int(queued["id"].(float64)))
	s.Store.Task.UpdateOne(task).SetStatus("running").Save(ctx)
	receipt["task_id"], receipt["revision"], receipt["status"] = task.ID, task.Revision, "applied"
	must(request("POST", "/api/v1/agent/results", receipt, agent, ""), 200)
	if exists, _ := s.Store.Resource.Query().Where(resource.IDEQ(poolID)).Exist(ctx); exists {
		t.Fatal("pool delete did not complete after receipt")
	}
}
