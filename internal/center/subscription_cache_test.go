package center

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/subgroup"
)

func TestSubscriptionRawCacheRoundTrip(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	cache := kvCache{s}
	if _, ok := cache.Load(ctx, 91); ok {
		t.Fatal("missing cache treated as known empty")
	}
	for _, nodes := range [][]string{{"trojan://p@203.0.113.1:443#one"}, {}} {
		cache.Store(ctx, 91, nodes)
		got, ok := cache.Load(ctx, 91)
		if !ok || strings.Join(got, "\n") != strings.Join(nodes, "\n") {
			t.Fatal("raw cache did not survive serialization", got, ok)
		}
	}
	if err := s.Store.KV.Set(ctx, sourceCacheKey(91), "invalid", 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Load(ctx, 91); ok {
		t.Fatal("corrupt cache treated as known empty")
	}
}

func TestSubscriptionRawIdentityAndEmptyBaseline(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	src := s.Store.Ent.SubSource.Create().SetURL("https://source.example/a").SetNodeCache(true).SetKnownNodes(12).SaveX(ctx)
	a, b := toSource(src), toSource(src)
	a.UserAgent, b.UserAgent = "format-a", "format-b"
	fail, empty := false, false
	f := subgroup.Fetcher{Fetch: func(_ context.Context, _, ua string) (subgroup.Response, error) {
		if fail {
			return subgroup.Response{}, fmt.Errorf("upstream_failed")
		}
		if empty {
			return subgroup.Response{}, nil
		}
		return subgroup.Response{Body: []byte("trojan://p@203.0.113.1:443#" + ua)}, nil
	}}
	read := func(source subgroup.Source) (string, subgroup.SourceResult) {
		t.Helper()
		body, results := subgroup.Combine(ctx, f, kvCache{s}, []subgroup.Source{source}, nil, subgroup.Options{})
		return body, results[0]
	}
	read(a)
	fail = true
	if body, r := read(b); body != "" || r.FromCache {
		t.Fatal("another user agent's raw cache leaked", body, r)
	}
	if body, r := read(a); body == "" || !r.FromCache {
		t.Fatal("matching raw cache was lost", body, r)
	}
	b = a
	b.URL = "https://source.example/b"
	if body, r := read(b); body != "" || r.FromCache {
		t.Fatal("old source URL raw cache leaked", body, r)
	}
	b = a
	b.NodeCache = false
	if body, r := read(b); body != "" || r.FromCache {
		t.Fatal("disabled cache used fallback", body, r)
	}
	fail, empty = false, true
	_, r := read(a)
	// This test changes the effective UA only in memory; the real result fence
	// accepts the source snapshot, while the raw cache stays UA-specific.
	s.recordResults(ctx, []subgroup.SourceResult{r})
	if got := s.Store.Ent.SubSource.GetX(ctx, src.ID); got.KnownNodes != 0 || got.NodeCount != 0 {
		t.Fatal("empty result retained the old baseline", got.KnownNodes)
	}
	empty = false
	if body, r := read(a); body == "" || r.Error != "" || r.FromCache {
		t.Fatal("recovery after legitimate empty was rejected", body, r)
	}
}

func TestSubscriptionSourceUpdateFencesInflightMetadataAndRawCache(t *testing.T) {
	s, admin := newInstalledServer(t)
	s.AllowPrivateSources = true
	ctx := t.Context()
	started, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			close(started)
			<-release
			w.Header().Set("Subscription-Userinfo", "total=123456")
			fmt.Fprint(w, "trojan://p@203.0.113.1:443#old")
			return
		}
		w.WriteHeader(502)
	}))
	defer upstream.Close()
	src := s.Store.Ent.SubSource.Create().SetURL(upstream.URL + "/old").SetNodeCache(true).SaveX(ctx)
	p := s.Store.Ent.SubProfile.Create().SetName("inflight").SetSourceIds([]int{src.ID}).SaveX(ctx)
	done := make(chan error, 1)
	go func() { _, _, _, _, err := s.groupNodes(ctx, p, "clash", true); done <- err }()
	<-started
	mustJSON(t, admin, "PATCH", "/api/v1/admin/subscriptions/sources/"+itoa(src.ID), map[string]any{"url": upstream.URL + "/new"}, 200)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	current := s.Store.Ent.SubSource.GetX(ctx, src.ID)
	if current.NodeCount != 0 || current.FetchedAt != nil || current.UserInfo != "" {
		t.Fatal("old response overwrote the new source", current.NodeCount, current.UserInfo)
	}
	body, results, _, _, err := s.groupNodes(ctx, p, "clash", false)
	if err != nil || body != "" || len(results) != 1 || results[0].FromCache || results[0].Error == "" {
		t.Fatal("old inflight cache contaminated the new URL", body, results, err)
	}
}

func TestSubscriptionGroupCacheIdentityEmptyAndRecovery(t *testing.T) {
	s, _ := newInstalledServer(t)
	s.AllowPrivateSources = true
	trace := "12345678123456781234567812345678"
	ctx := withTrace(t.Context(), trace)
	logs := captureLog(t)
	var calls atomic.Int32
	var fail atomic.Bool
	var empty atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(502)
			return
		}
		if !empty.Load() {
			fmt.Fprint(w, "trojan://p@203.0.113.1:443#HK-node")
		}
	}))
	defer upstream.Close()
	source := s.Store.Ent.SubSource.Create().SetName("source").SetURL(upstream.URL).SetNodeCache(true).SaveX(ctx)
	profile := s.Store.Ent.SubProfile.Create().SetName("cache").SetSourceIds([]int{source.ID}).SaveX(ctx)
	read := func(ua string, refresh bool) (string, bool) {
		t.Helper()
		body, result, _, _, err := s.groupNodes(ctx, profile, ua, refresh)
		if err != nil {
			t.Fatal(err)
		}
		return body, len(result) != 0 && result[0].Error != ""
	}
	first, _ := read("client-a", false)
	read("client-a", false)
	read("client-a", false)
	// The first raw publication changes the durable dependency version. Only
	// a subsequent stable computation may populate the rendered cache.
	if first == "" || calls.Load() != 2 {
		t.Fatal("normal cache miss", first, calls.Load())
	}
	if !strings.Contains(logs.String(), "trace="+trace) || !strings.Contains(logs.String(), "stage=subscription.source") || !strings.Contains(logs.String(), "stage=subscription.combine") || !strings.Contains(logs.String(), "outcome=hit") {
		t.Fatal("source, processing and cache stages lost correlation", logs.String())
	}
	read("client-b", false)
	if calls.Load() != 3 {
		t.Fatal("different user agents shared a group cache")
	}
	profile.Settings = map[string]any{"filter_include": "keep:does-not-match"}
	filtered, _ := read("client-a", false)
	if filtered != "" || calls.Load() != 4 {
		t.Fatal("effective settings absent from cache identity", filtered, calls.Load())
	}
	read("client-a", false)
	if calls.Load() != 4 {
		t.Fatal("valid empty filtered result was not cached")
	}
	profile.Settings = nil
	source.Update().SetName("new-name").ExecX(ctx)
	read("client-a", false)
	if calls.Load() != 5 {
		t.Fatal("dependency changes reused old group cache")
	}
	fail.Store(true)
	s.invalidateGroups(ctx)
	body, failed := read("client-a", false)
	if !failed || body == "" {
		t.Fatal("protected raw cache not returned on failure", body, failed)
	}
	fail.Store(false)
	read("client-a", false)
	if calls.Load() != 7 {
		t.Fatal("failed fallback cached as a successful group")
	}
	empty.Store(true)
	s.invalidateGroups(ctx)
	read("client-a", false)
	read("client-a", false)
	read("client-a", false)
	if calls.Load() != 9 {
		t.Fatal("valid upstream empty not cached")
	}
}

func TestSubscriptionGroupInvalidationFencesInflightFetch(t *testing.T) {
	s, _ := newInstalledServer(t)
	s.AllowPrivateSources = true
	ctx := t.Context()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		fmt.Fprint(w, "trojan://p@203.0.113.1:443#node")
	}))
	defer upstream.Close()
	source := s.Store.Ent.SubSource.Create().SetURL(upstream.URL).SaveX(ctx)
	p := s.Store.Ent.SubProfile.Create().SetName("inflight").SetSourceIds([]int{source.ID}).SaveX(ctx)
	done := make(chan error, 1)
	read := func(ctx context.Context, profile *ent.SubProfile) error {
		_, _, _, _, err := s.groupNodes(ctx, profile, "client", false)
		return err
	}
	go func() { done <- read(ctx, p) }()
	<-started
	s.invalidateGroups(ctx)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := read(ctx, p); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("in-flight fetch repopulated an invalidated cache")
	}
}
