package control

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imzyb/MiGate/internal/subscription"
)

func testSourceOptionsIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin string) {
	t.Helper()
	testSourceProtectionIntegration(t, s)
	testSourceUsageIntegration(t, s, request, admin)
	ctx := context.Background()
	data := map[string]any{"name": "source-options-fixture", "url": "https://example.com/sub", "user_agent": "Inventory/69", "website": "https://example.com", "notes": "owned fixture", "enabled": true}
	r, err := s.Store.Resource.Create().SetKind("sources").SetKey(randomToken(16)).SetData(data).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(r).Exec(ctx)
	r, err = s.refreshSourceCache(ctx, r.ID, func(_ context.Context, url, ua string) (subscription.FetchedInventory, error) {
		if url != "https://example.com/sub" || ua != "Inventory/69" {
			t.Error("source request options not forwarded")
		}
		return subscription.FetchedInventory{Body: []byte("trojan://synthetic@example.com:443#n")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data = r.Data
	data["notes"] = "updated notes"
	if w := request("POST", "/api/v1/admin/resources/sources", map[string]any{"id": r.ID, "data": data}, "", admin); w.Code != 200 {
		t.Fatal("metadata update rejected", w.Code)
	}
	r, _ = s.Store.Resource.Get(ctx, r.ID)
	if r.Data["last_update"] == nil || r.Data["links"] == nil {
		t.Fatal("metadata update discarded cache")
	}
	data = r.Data
	data["user_agent"] = "Inventory/70"
	if w := request("POST", "/api/v1/admin/resources/sources", map[string]any{"id": r.ID, "data": data}, "", admin); w.Code != 200 {
		t.Fatal("UA update rejected", w.Code)
	}
	r, _ = s.Store.Resource.Get(ctx, r.ID)
	for _, field := range sourceCacheFields {
		if _, exists := r.Data[field]; exists {
			t.Fatal("UA change retained old cache", field)
		}
	}
	r, err = s.refreshSourceCache(ctx, r.ID, func(context.Context, string, string) (subscription.FetchedInventory, error) {
		return subscription.FetchedInventory{Body: []byte("trojan://p%2Bq@example.com:443#HK+A%2BB")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data = r.Data
	data["plus_as_space"] = true
	if w := request("POST", "/api/v1/admin/resources/sources", map[string]any{"id": r.ID, "data": data}, "", admin); w.Code != 200 {
		t.Fatal("name option update rejected", w.Code)
	}
	r, _ = s.Store.Resource.Get(ctx, r.ID)
	for _, field := range sourceCacheFields {
		if _, exists := r.Data[field]; exists {
			t.Fatal("name option change retained old cache", field)
		}
	}
	r, err = s.refreshSourceCache(ctx, r.ID, func(context.Context, string, string) (subscription.FetchedInventory, error) {
		return subscription.FetchedInventory{Body: []byte("trojan://p%2Bq@example.com:443#HK+A%2BB")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	links, _, err := sourceLinks(r.Data, time.Now())
	if err != nil || len(links) != 1 {
		t.Fatal("new name cache unavailable", err)
	}
	node, err := subscription.ParseLink(links[0])
	if err != nil || node.Remark != "HK A+B" || node.Password != "p+q" {
		t.Fatal("name setting lost node fields", err)
	}
	previousUpdate := r.Data["last_update"]
	data = r.Data
	data["filter_exclude"] = "HK"
	if w := request("POST", "/api/v1/admin/resources/sources", map[string]any{"id": r.ID, "data": data}, "", admin); w.Code != 200 {
		t.Fatal("source filter update rejected")
	}
	r, _ = s.Store.Resource.Get(ctx, r.ID)
	links, _, err = sourceLinks(r.Data, time.Now())
	if err != nil || len(links) != 0 || r.Data["last_update"] != previousUpdate {
		t.Fatal("filter failed to apply to existing cache without clearing it", err)
	}
	for key, value := range map[string]any{"user_agent": "x\r\nAuthorization: secret", "website": "javascript:alert(1)", "notes": 42, "plus_as_space": "true", "enabled": 1, "allow_stale": "false", "node_cache": "true", "exclude_traffic": "true", "filter_include": "[", "filter_exclude": 123} {
		bad := map[string]any{}
		for k, v := range r.Data {
			bad[k] = v
		}
		bad[key] = value
		if w := request("POST", "/api/v1/admin/resources/sources", map[string]any{"id": r.ID, "data": bad}, "", admin); w.Code != 400 {
			t.Fatal(fmt.Sprintf("invalid source %s accepted", key))
		}
	}
	r, _ = s.Store.Resource.Get(ctx, r.ID)
	if r.Data["filter_exclude"] != "HK" || r.Data["last_update"] != previousUpdate {
		t.Fatal("rejected filter changed saved data")
	}
}
