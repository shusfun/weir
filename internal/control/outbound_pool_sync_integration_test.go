package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
)

func testOutboundPoolSync(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	d, e := s.Store.Device.Create().SetName("sync-pool-device").SetPublicKey(randomToken(32)).SetPairExpires(time.Now()).SetStatus("approved").SetMetadata(map[string]any{"capabilities": []string{"outbound.pool.v1", "outbound.tools.v1", "outbound.transport.v1", "resource.enabled.v1"}}).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Store.Device.DeleteOne(d).Exec(ctx)
	src, e := s.Store.Resource.Create().SetKind("sources").SetKey(randomToken(16)).SetData(map[string]any{"name": "sync-source", "url": "https://example.com/sync", "enabled": true, "last_update": time.Now().UTC().Format(time.RFC3339), "links": []string{"http://u:secret@example.com:8080#one", "http://u:secret@example.com:8080#duplicate", "trojan://synthetic@example.org:443#two", "snell://synthetic@example.net:443?version=4#unsupported"}}).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Store.Resource.DeleteOne(src).Exec(ctx)
	r, e := s.Store.Resource.Create().SetKind("outbound-pool").SetKey(randomToken(16)).SetOwnerID(d.ID).SetData(map[string]any{"name": "sync", "source_id": src.ID, "version": 1, "status": "applied", "enabled": true, "auto_refresh": true, "auto_armed": true}).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Store.Resource.Delete().Where(resource.OwnerIDEQ(d.ID), resource.KindIn("outbound-pool", "pool-node-id", "device-outbound", "device-routing")).Exec(ctx)
	if e = s.syncPool(ctx, r.ID, true); e != nil {
		t.Fatal(e)
	}
	r, _ = s.Store.Resource.Get(ctx, r.ID)
	if r.Data["status"] != "pending" || number(r.Data["sync_unsupported"]) != 1 {
		t.Fatal("sync status not explicit")
	}
	taskID := int(number(r.Data["task_id"]))
	task, _ := s.Store.Task.Get(ctx, taskID)
	if len(task.Payload["next"].([]any)) != 2 {
		t.Fatal("dedup/support selection incorrect")
	}
	commit := func(task *ent.Task, status string) {
		t.Helper()
		tx, e := s.Store.Tx(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		if e = savePoolResult(ctx, tx, task, status); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Task.UpdateOneID(task.ID).SetStatus(status).Save(ctx); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
	}
	commit(task, "applied")
	rows, _ := s.Store.Resource.Query().Where(resource.KindEQ("device-outbound"), resource.OwnerIDEQ(d.ID)).All(ctx)
	if len(rows) != 2 {
		t.Fatal("materialization missing")
	}
	if e = s.syncPool(ctx, r.ID, true); e != nil {
		t.Fatal(e)
	}
	r, _ = s.Store.Resource.Get(ctx, r.ID)
	if int(number(r.Data["task_id"])) != taskID || r.Data["status"] != "applied" {
		t.Fatal("unchanged source duplicated task")
	}
	// Remove a referenced node: even a disabled rule must keep its identity.
	var removed, retained *ent.Resource
	for _, row := range rows {
		if row.Data["protocol"] == "http" {
			removed = row
		} else {
			retained = row
		}
	}
	_, e = s.Store.Resource.Create().SetKind("device-routing").SetKey(fmt.Sprintf("%d:901", d.ID)).SetOwnerID(d.ID).SetData(map[string]any{"id": 901, "outbound_id": removed.Data["id"], "enabled": false}).Save(ctx)
	if e != nil {
		t.Fatal(e)
	}
	src.Data["links"] = []string{"trojan://synthetic@example.org:443#renamed"}
	s.Store.Resource.UpdateOne(src).SetData(src.Data).Save(ctx)
	if e = s.syncPool(ctx, r.ID, true); e != nil {
		t.Fatal(e)
	}
	r, _ = s.Store.Resource.Get(ctx, r.ID)
	if r.Data["sync_error"] != "pool_member_has_routes" || r.Data["status"] != "applied" {
		t.Fatal("route removal did not preserve applied state")
	}
	s.Store.Resource.Delete().Where(resource.KindEQ("device-routing"), resource.OwnerIDEQ(d.ID)).Exec(ctx)
	if e = s.syncPool(ctx, r.ID, true); e != nil {
		t.Fatal(e)
	}
	r, _ = s.Store.Resource.Get(ctx, r.ID)
	task, _ = s.Store.Task.Get(ctx, int(number(r.Data["task_id"])))
	next := task.Payload["next"].([]any)[0].(map[string]any)
	if next["id"] != retained.Data["id"] || next["tag"] != retained.Data["tag"] {
		t.Fatal("sync rotated connection identity")
	}
	commit(task, "failed")
	count, _ := s.Store.Resource.Query().Where(resource.KindEQ("device-outbound"), resource.OwnerIDEQ(d.ID)).Count(ctx)
	if count != 2 {
		t.Fatal("failed sync discarded inventory")
	}
	path := fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID)
	for _, payload := range []map[string]any{{"ids": []int{int(number(removed.Data["id"]))}, "address": "127.0.0.1"}, {"ids": []int{999999}}, {"ids": []int{int(number(removed.Data["id"])), int(number(removed.Data["id"]))}}} {
		w := request("POST", path, map[string]any{"operation": "outbound.probe", "confirm": true, "payload": payload}, "", admin)
		if w.Code != 400 && w.Code != 409 {
			t.Fatal("unbounded probe accepted", w.Code)
		}
	}
	body := map[string]any{"operation": "outbound.probe", "confirm": true, "payload": map[string]any{"ids": []int{int(number(removed.Data["id"]))}}}
	if request("POST", path, body, "", alice).Code != 403 {
		t.Fatal("ordinary probe allowed")
	}
	if w := request("POST", path, body, "", admin); w.Code != 202 {
		t.Fatal("valid probe refused", w.Code, w.Body.String())
	}
	if request("POST", path, body, "", admin).Code != 409 {
		t.Fatal("parallel device probe allowed")
	}
	// Only the fixed-source resolver may build a public catalog dial target.
	if request("POST", path, map[string]any{"operation": "outbound.catalog.probe", "confirm": true, "payload": map[string]any{"address": "8.8.8.8", "port": 443, "key": randomToken(32)}}, "", admin).Code != 400 {
		t.Fatal("browser supplied probe address accepted")
	}
	catalog := proxyCatalog{ObservedAt: time.Now().UTC(), Items: []proxyCandidate{{Key: randomToken(32), Protocol: "http", Address: "8.8.8.8", Port: 8080, Country: "Synthetic country", CountryCode: "ZZ", Username: "catalog-user", Password: "catalog-secret"}}}
	b, _ := json.Marshal(catalog)
	sealed, _ := s.Crypto.Encrypt("proxy-catalog:http:" + string(b))
	if e = s.Redis.Set(ctx, "migate:proxy-catalog:http", sealed, time.Minute).Err(); e != nil {
		t.Fatal(e)
	}
	defer s.Redis.Del(ctx, "migate:proxy-catalog:http")
	w := request("GET", "/api/v1/admin/proxy-catalog/http?page=1", nil, "", admin)
	if w.Code != 200 || strings.Contains(w.Body.String(), "catalog-secret") || strings.Contains(w.Body.String(), "catalog-user") {
		t.Fatal("catalog exposed credentials", w.Code)
	}
	if request("GET", "/api/v1/admin/proxy-catalog/http", nil, "", alice).Code != 403 {
		t.Fatal("ordinary catalog access")
	}
	if request("GET", "/api/v1/admin/proxy-catalog/http?url=https://internal", nil, "", admin).Code != 400 {
		t.Fatal("browser catalog URL accepted")
	}
	w = request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/proxy-catalog/import", d.ID), map[string]any{"protocol": "http", "key": catalog.Items[0].Key, "confirm": true}, "", admin)
	if w.Code != 202 || strings.Contains(w.Body.String(), "catalog-secret") {
		t.Fatal("catalog import failed", w.Code, w.Body.String())
	}
	t.Log("Auto sync dedup/unsupported, unchanged-source idempotency, disabled-route protection, stable identities, failure preservation and bounded owned probe passed")
}
