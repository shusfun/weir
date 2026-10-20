package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func testSourceUsageIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin string) {
	t.Helper()
	ctx := context.Background()
	if request("GET", "/api/v1/admin/sources/usage", nil, "", "").Code != 401 {
		t.Fatal("anonymous usage allowed")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	d := map[string]any{"name": "usage-owned", "url": "https://example.com/sub", "enabled": true, "last_update": now, "upstream_usage": map[string]any{"status": "available", "upload": "1", "download": "2", "total": "100", "expire": "500"}}
	r := s.Store.Resource.Create().SetKind("sources").SetKey(randomToken(16)).SetData(d).SaveX(ctx)
	defer s.Store.Resource.DeleteOne(r).Exec(ctx)
	w := request("GET", "/api/v1/admin/sources/usage", nil, "", admin)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("usage endpoint unavailable")
	}
	// Fixed PG projection through preview; unrelated source inventories do not participate.
	p := s.Store.Resource.Create().SetKind("profiles").SetKey(randomToken(16)).SetData(map[string]any{"source_ids": []int{r.ID}}).SaveX(ctx)
	defer s.Store.Resource.DeleteOne(p).Exec(ctx)
	r.Data["links"] = []string{"trojan://synthetic@example.com:443#fixture"}
	s.Store.Resource.UpdateOne(r).SetData(r.Data).SaveX(ctx)
	w = request("POST", "/api/v1/admin/profiles/preview", map[string]any{"id": p.ID, "target": "links"}, "", admin)
	var view struct {
		Usage sourceUsageSummary `json:"upstream_usage"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Usage.Total == nil || *view.Usage.Total != "100" {
		t.Fatal("profile usage missing", w.Code)
	}
	r.Data["exclude_traffic"] = true
	s.Store.Resource.UpdateOne(r).SetData(r.Data).SaveX(ctx)
	w = request("POST", "/api/v1/admin/profiles/preview", map[string]any{"id": p.ID, "target": "links"}, "", admin)
	json.Unmarshal(w.Body.Bytes(), &view)
	if w.Code != 200 || view.Usage.Excluded != 1 || view.Usage.Included != 0 || view.Usage.Total != nil || view.Usage.Expire == nil || *view.Usage.Expire != "500" {
		t.Fatal(fmt.Sprintf("excluded source contract: %+v", view.Usage))
	}
}
