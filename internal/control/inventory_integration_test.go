package control

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/imzyb/MiGate/internal/subscription"
	"net/http/httptest"
	"testing"

	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
)

func testInventoryIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	content := "proxies:\n - {name: HK, type: http, server: example.com, port: 8080, username: user, password: synthetic}\n - {name: US, type: socks5, server: example.org, port: 1080}"
	if request("POST", "/api/v1/admin/subscriptions/import", map[string]any{"content": content}, "", alice).Code != 403 {
		t.Fatal("ordinary user can import inventory")
	}
	before, err := s.Store.Resource.Query().Where(resource.KindEQ("nodes")).Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	preview := request("POST", "/api/v1/admin/subscriptions/import", map[string]any{"content": content, "preview": true}, "", admin)
	if preview.Code != 200 {
		t.Fatal("inventory preview failed")
	}
	var view map[string]any
	json.Unmarshal(preview.Body.Bytes(), &view)
	if view["node_count"].(float64) != 2 {
		t.Fatal("preview count wrong")
	}
	for _, raw := range view["nodes"].([]any) {
		for key := range raw.(map[string]any) {
			if key == "password" || key == "url" || key == "username" || key == "credential" {
				t.Fatal("preview leaks credentials")
			}
		}
	}
	previewCount, _ := s.Store.Resource.Query().Where(resource.KindEQ("nodes")).Count(ctx)
	if previewCount != before {
		t.Fatal("preview created resources")
	}
	duplicate := request("POST", "/api/v1/admin/subscriptions/import", map[string]any{"content": "socks5://u:p@example.com:1080#A\nsocks5://u:p@example.com:1080#B", "preview": true, "deduplicate": true}, "", admin)
	json.Unmarshal(duplicate.Body.Bytes(), &view)
	if duplicate.Code != 200 || view["node_count"].(float64) != 1 || view["duplicate_count"].(float64) != 1 {
		t.Fatal("dedup preview mismatch")
	}
	active := true
	calls := 0
	s.Store.Resource.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if active && m.Op() == ent.OpCreate {
				calls++
				if calls == 2 {
					return nil, errors.New("injected batch failure")
				}
			}
			return next.Mutate(ctx, m)
		})
	})
	response := request("POST", "/api/v1/admin/subscriptions/import", map[string]any{"content": content}, "", admin)
	active = false
	if response.Code != 500 {
		t.Fatal("batch injection not triggered")
	}
	after, _ := s.Store.Resource.Query().Where(resource.KindEQ("nodes")).Count(ctx)
	if after != before {
		t.Fatal("partial import committed")
	}
	response = request("POST", "/api/v1/admin/subscriptions/import", map[string]any{"content": content}, "", admin)
	if response.Code != 200 {
		t.Fatal("batch import failed", response.Code)
	}
	var saved struct {
		IDs   []int `json:"ids"`
		Count int   `json:"node_count"`
	}
	json.Unmarshal(response.Body.Bytes(), &saved)
	if saved.Count != 2 || len(saved.IDs) != 2 {
		t.Fatal("batch count wrong")
	}
	for _, id := range saved.IDs {
		s.Store.Resource.DeleteOneID(id).Exec(ctx)
	}
	source, err := s.Store.Resource.Create().SetKind("sources").SetKey("structured-source-fixture").SetData(map[string]any{"url": "https://example.com/source"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(source).Exec(ctx)
	refreshed, err := s.refreshSourceCache(ctx, source.ID, func(context.Context, string, string) (subscription.FetchedInventory, error) {
		return subscription.FetchedInventory{Body: []byte(content)}, nil
	})
	if err != nil || number(refreshed.Data["node_count"]) != 2 {
		t.Fatal("structured source refresh did not import nodes", err)
	}
	refreshed, err = s.refreshSourceCache(ctx, source.ID, func(context.Context, string, string) (subscription.FetchedInventory, error) {
		return subscription.FetchedInventory{Body: []byte(content), Usage: subscription.ParseUsageMetadata([]string{"download=9007199254740993;total=9223372036854775807;expire=0"})}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := s.Store.Resource.Get(ctx, source.ID)
	b, _ := json.Marshal(persisted.Data["upstream_usage"])
	var usage subscription.UsageMetadata
	if err != nil || json.Unmarshal(b, &usage) != nil || usage.Status != "available" || usage.Download == nil || *usage.Download != "9007199254740993" || usage.Upload != nil {
		t.Fatal("persisted upstream observations lost precision or fabricated counters")
	}
	response = request("POST", "/api/v1/admin/resources/sources", map[string]any{"id": source.ID, "data": map[string]any{"url": "https://example.com/source", "upstream_usage": map[string]any{"status": "available", "download": "0"}}}, "", admin)
	if response.Code != 200 {
		t.Fatal("source update failed")
	}
	persisted, _ = s.Store.Resource.Get(ctx, source.ID)
	b, _ = json.Marshal(persisted.Data["upstream_usage"])
	if json.Unmarshal(b, &usage) != nil || *usage.Download != "9007199254740993" {
		t.Fatal("administrator form replaced observed metadata with fabricated values")
	}
}
