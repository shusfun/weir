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

func testEventDirectoryIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	now := time.Now()
	for i, fixture := range []struct{ kind, status, severity, observed string }{
		{"security-event", "resolved", "info", now.Format(time.RFC3339)},
		{"security-event", "resolved", "warning", now.Add(-2 * time.Minute).Format(time.RFC3339)},
		{"security-event", "unknown", "critical", "not-a-time"},
		{"model-event", "classified", "info", now.Add(-time.Hour).Format(time.RFC3339)},
		{"model-event", "classified", "info", now.Add(time.Hour).Format(time.RFC3339)},
	} {
		r, err := s.Store.Resource.Create().SetKind(fixture.kind).SetKey(fmt.Sprintf("event-directory-%d", i)).SetData(map[string]any{"category": "directory_100%", "reason": "fixture", "device_id": 19, "status": fixture.status, "severity": fixture.severity, "observed_at": fixture.observed, "private_key": "must-not-leak", "prompt": "untrusted-hidden-field"}).Save(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer s.Store.Resource.DeleteOne(r).Exec(context.Background())
	}
	get := func(q, cookie string, status int) map[string]any {
		t.Helper()
		w := request("GET", "/api/v1/admin/events"+q, nil, "", cookie)
		if w.Code != status {
			t.Fatalf("events HTTP %d want %d", w.Code, status)
		}
		if strings.Contains(w.Body.String(), "must-not-leak") || strings.Contains(w.Body.String(), "untrusted-hidden-field") {
			t.Fatal("event allowlist leak")
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	a := get("?q=DIRECTORY_100%25&page=1&page_size=1", admin, 200)
	b := get("?q=directory_100%25&page=2&page_size=1", admin, 200)
	if a["total"].(float64) != 5 || a["items"].([]any)[0].(map[string]any)["id"] == b["items"].([]any)[0].(map[string]any)["id"] {
		t.Fatal("event paging or literal matching mismatch")
	}
	rows := get("?q=directory_100%25", admin, 200)["items"].([]any)
	expected := []string{"unknown", "snapshot", "unknown", "stale", "fresh"}
	for i, v := range rows {
		r := v.(map[string]any)
		if r["validity"] != expected[i] || r["source"] == nil {
			t.Fatal("unknown, stale, future or historical classification validity incorrect")
		}
	}
	filtered := get("?q=directory_100%25&kind=security-event&status=resolved&severity=warning&device_id=19", admin, 200)
	if filtered["total"].(float64) != 1 {
		t.Fatal("combined observation filters mismatch")
	}
	for _, q := range []string{"?page=0", "?page_size=201", "?kind=nodes", "?device_id=-1", "?device_id=oops"} {
		get(q, admin, 400)
	}
	get("?q=directory", alice, 403)
	get("?q=directory", "", 401)
}
