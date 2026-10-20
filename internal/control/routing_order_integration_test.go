package control

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/imzyb/MiGate/ent"
	"net/http/httptest"
	"testing"
	"time"
)

func testRoutingOrderIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	d, err := s.Store.Device.Create().SetName("routing-order-test").SetPublicKey(randomToken(32)).SetPairExpires(time.Now()).SetStatus("approved").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Device.DeleteOne(d).Exec(ctx)
	rows := []*ent.Resource{}
	for _, id := range []int{71, 72} {
		r, err := s.Store.Resource.Create().SetKind("device-routing").SetKey(fmt.Sprintf("%d:%d", d.ID, id)).SetOwnerID(d.ID).SetData(map[string]any{"id": id, "sort": 0, "domain": "order.example", "outbound_id": 9}).Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, r)
		defer s.Store.Resource.DeleteOne(r).Exec(ctx)
	}
	path := fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID)
	payload := map[string]any{"rules": []any{map[string]any{"id": 72, "sort": 0}, map[string]any{"id": 71, "sort": 0}}}
	body := map[string]any{"operation": "routing.reorder", "confirm": true, "payload": payload}
	if request("POST", path, body, "", alice).Code != 403 {
		t.Fatal("ordinary user reordered device routes")
	}
	if request("POST", path, body, "", admin).Code != 400 {
		t.Fatal("unsupported Agent received reorder")
	}
	if _, err = s.Store.Device.UpdateOne(d).SetMetadata(map[string]any{"capabilities": []string{"routing.order.v1"}}).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if request("POST", path, map[string]any{"operation": "routing.reorder", "confirm": true, "payload": map[string]any{"rules": []any{map[string]any{"id": 71, "sort": 0}}}}, "", admin).Code != 409 {
		t.Fatal("incomplete order accepted")
	}
	w := request("POST", path, body, "", admin)
	if w.Code != 202 {
		t.Fatal("reorder enqueue failed", w.Code)
	}
	var created struct{ ID int }
	if json.Unmarshal(w.Body.Bytes(), &created) != nil {
		t.Fatal("invalid task response")
	}
	defer s.Store.Task.DeleteOneID(created.ID).Exec(ctx)
	for _, r := range rows {
		saved, _ := s.Store.Resource.Get(ctx, r.ID)
		if number(saved.Data["sort"]) != 0 {
			t.Fatal("queued order reported applied")
		}
	}
	// Invoke the same receipt persistence helper inside a transaction. Task receipt
	// target/revision/terminal idempotency is already covered by integration flow.
	tx, err := s.Store.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = saveRoutingOrder(ctx, tx, d.ID, payload); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for i, r := range rows {
		saved, _ := s.Store.Resource.Get(ctx, r.ID)
		if number(saved.Data["sort"]) != float64(20-i*10) || number(saved.Data["outbound_id"]) != 9 {
			t.Fatal("receipt order/attributes incorrect")
		}
	}
	if request("POST", path, body, "", admin).Code != 409 {
		t.Fatal("stale editor accepted")
	}
	t.Log("routing order admin/capability boundary, complete-set and stale guards, queued != applied, transactional receipt attributes passed")
}
