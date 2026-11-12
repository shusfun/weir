package center

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscriptionTrafficRefreshMetadataAndTrace(t *testing.T) {
	s, admin := newInstalledServer(t)
	s.AllowPrivateSources = true
	var mode atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 0:
			w.Header().Set("Subscription-Userinfo", "upload=100; download=200; total=1000; expire=1999999999")
		case 2:
			http.Error(w, "upstream unavailable", 503)
			return
		case 3:
			w.Header().Set("Subscription-Userinfo", "total=1000; download=200; upload=-10; expire=1999999999")
		}
		fmt.Fprint(w, "trojan://demo@198.51.100.1:443#demo")
	}))
	defer upstream.Close()
	src := mustJSON(t, admin, "POST", "/api/v1/admin/subscriptions/sources", map[string]any{"name": "traffic fixture", "url": upstream.URL}, 200)
	id := int(src["id"].(float64))
	path := fmt.Sprintf("/api/v1/admin/subscriptions/sources/%d/refresh", id)
	refresh := func() {
		t.Helper()
		w := admin.do("POST", path, nil, nil)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		trace := w.Header().Get("X-Trace-Id")
		rows := mustJSON(t, admin, "GET", "/api/v1/admin/subscriptions/runs", nil, 200)["items"].([]any)
		run, err := s.loadSubscriptionRun(t.Context(), rows[0].(map[string]any)["id"].(string))
		if err != nil || trace == "" || run.TraceID != trace {
			t.Fatal("refresh trace lost", run, err)
		}
		saved := false
		for _, stage := range run.Stages {
			if stage.Operation == "source.persist" && stage.SourceID == id && stage.Outcome == "succeeded" {
				saved = true
			}
		}
		if !saved {
			t.Fatal("missing persistence stage", run)
		}
		if mode.Load() == 2 && run.Outcome != "failed" {
			t.Fatal("upstream failure reported as success", run)
		}
	}
	refresh()
	old := s.Store.Ent.SubSource.GetX(t.Context(), id)
	if used, total, ok := sourceTraffic(parseUserInfo(old.UserInfo)); !ok || used != 300 || total != 1000 {
		t.Fatal(old.UserInfo)
	}
	mode.Store(2)
	refresh()
	failed := s.Store.Ent.SubSource.GetX(t.Context(), id)
	if failed.UserInfo != old.UserInfo || failed.LastError == "" || !failed.FetchedAt.Equal(*old.FetchedAt) {
		t.Fatal("failed fetch replaced last successful metadata")
	}
	mode.Store(1)
	refresh()
	if got := s.Store.Ent.SubSource.GetX(t.Context(), id); got.UserInfo != "" || got.LastError != "" {
		t.Fatal("missing header retained stale traffic")
	}
	mode.Store(3)
	refresh()
	totals := mustJSON(t, admin, "GET", "/api/v1/admin/subscriptions/sources", nil, 200)["traffic"].(map[string]any)
	if totals["sources"].(float64) != 0 {
		t.Fatal("partial traffic counted", totals)
	}
	// Exercise the actual scheduled refresh entry after a successful partial response.
	settings := defaultSubscriptionSettings()
	settings.SourceRefresh = 1
	mustJSON(t, admin, "PUT", "/api/v1/admin/subscriptions/settings", settings, 200)
	mode.Store(1)
	if err := s.RefreshSources(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := s.Store.Ent.SubSource.GetX(t.Context(), id); got.UserInfo != "" {
		t.Fatal("background result retained stale traffic")
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "traffic-viewer@example.test", "password": "Passw0rd"}, 200)
	user := login(t, s, "traffic-viewer@example.test", "Passw0rd")
	if w := user.do("POST", path, nil, nil); w.Code != 403 {
		t.Fatal("ordinary user refresh allowed", w.Code)
	}
	viewer := &testClient{t: t, router: admin.router}
	if w := viewer.do("POST", path, nil, nil); w.Code != 401 {
		t.Fatal("anonymous refresh allowed", w.Code)
	}
}

func TestSubscriptionTrafficPartialInvalidAndZero(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		known bool
		used  int64
	}{
		{"upload=0;download=0;total=1000", true, 0},
		{"upload=1100;download=100;total=1000", true, 1200},
		{"download=100;total=1000", false, 0},
		{"upload=-1;download=100;total=1000", false, 0},
		{"upload=no;download=100;total=1000", false, 0},
		{"upload=0;download=0;total=0", false, 0},
		{"upload=9223372036854775807;download=1;total=1000", false, 0},
		{"expire=1999999999", false, 0},
	} {
		used, _, known := sourceTraffic(parseUserInfo(tc.raw))
		if known != tc.known || used != tc.used {
			t.Errorf("%s: used=%d known=%v", tc.raw, used, known)
		}
	}
	if parseUserInfo("upload=-1;download=oops;other=10") != nil {
		t.Fatal("invalid fields accepted")
	}
}
