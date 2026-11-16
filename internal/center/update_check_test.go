package center

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/selfupdate"
)

func completeUpdateRelease(version string) ghrelease.Release {
	r := ghrelease.Release{Tag: "v" + version, HTMLURL: "https://github.com/shusfun/weir/releases/tag/v" + version}
	for _, name := range []string{selfupdate.PrefetchName(version, "amd64"), selfupdate.PrefetchName(version, "arm64"), "checksums.txt"} {
		r.Assets = append(r.Assets, ghrelease.Asset{Name: name, State: "uploaded", Size: 100, Digest: "sha256:" + strings.Repeat("a", 64)})
	}
	return r
}

func TestUpdateNoticeUsesLatestValidSnapshot(t *testing.T) {
	s, _ := newInstalledServer(t)
	cacheUpdateNoticeRecommendations(t, s)
	old := buildinfo.Version
	buildinfo.Version = "0.1.16"
	t.Cleanup(func() { buildinfo.Version = old })
	s.updates.snapshot = updateSnapshot{SucceededAt: time.Now(), Releases: []weirRelease{
		{Version: "0.1.19", Unavailable: "missing assets"},
		{Version: "0.1.18"},
		{Version: "0.1.16"},
	}}
	found := false
	for _, n := range s.notices(t.Context()) {
		if strings.HasPrefix(n.ID, "weir-update:") {
			if n.ID != "weir-update:0.1.18" || n.Action == nil || n.Action.Kind != "update" {
				t.Fatalf("incorrect target notice: %+v", n)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing cross-version update notice")
	}
}

func cacheUpdateNoticeRecommendations(t *testing.T, s *Server) {
	t.Helper()
	// These tests exercise Weir notices without unrelated upstream kernel queries.
	for _, kernel := range []string{"xray", "sing-box", "mihomo"} {
		if err := s.Store.KV.Set(t.Context(), "kernel-recommend:"+kernel, `{}`, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
}

func waitUpdateCheck(t *testing.T, s *Server) {
	t.Helper()
	done, err := s.beginUpdateCheck("11223344556677889900aabbccddeeff", "test")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("check did not finish")
	}
}

func TestUpdateCheckTraceCrossVersionAndRecovery(t *testing.T) {
	s, admin := newInstalledServer(t)
	old := buildinfo.Version
	buildinfo.Version = "0.1.16"
	t.Cleanup(func() { buildinfo.Version = old })
	s.updates.ctx = t.Context()
	list := []ghrelease.Release{completeUpdateRelease("0.1.18"), completeUpdateRelease("0.1.16"), completeUpdateRelease("0.1.17")}
	list[2].Assets = nil
	var failure error
	var calls int
	s.GitHub = &ghrelease.Client{Query: func(ctx context.Context, repo, tag string, count, page int) ([]ghrelease.Release, error) {
		calls++
		if failure != nil {
			return nil, failure
		}
		if tag != "" {
			if tag != "v0.1.18" {
				t.Errorf("intermediate release requested: %s", tag)
			}
			for _, r := range list {
				if r.Tag == tag {
					return []ghrelease.Release{r}, nil
				}
			}
			return nil, ghrelease.ErrNotFound
		}
		if count == 0 {
			if len(list) == 0 {
				return nil, ghrelease.ErrNotFound
			}
			return list[:1], nil
		}
		return list, nil
	}}
	logs := captureLog(t)
	waitUpdateCheck(t, s)
	view := mustJSON(t, admin, "GET", "/api/v1/admin/system/update/status", nil, 200)
	if view["update_status"] != "available" || view["latest"].(map[string]any)["version"] != "0.1.18" || len(view["unavailable"].([]any)) != 1 {
		t.Fatalf("view=%v", view)
	}
	before := calls
	mustJSON(t, admin, "GET", "/api/v1/admin/system/update", nil, 200)
	if calls != before {
		t.Fatal("snapshot performed network query")
	}
	if ok, err := s.releaseKnown(t.Context(), "0.1.18"); !ok || err != nil {
		t.Fatalf("direct target: %v", err)
	}
	failure = errors.New("release_rate_limited")
	waitUpdateCheck(t, s)
	check := s.updateCheckSnapshot()
	if !check.Stale || check.Error == "" || len(check.Releases) != 3 || s.updateCheckView()["update_status"] != "failed" {
		t.Fatalf("failure lost evidence: %+v", check)
	}
	failure = nil
	list = []ghrelease.Release{completeUpdateRelease("0.1.18")}
	waitUpdateCheck(t, s)
	if got := s.updateCheckSnapshot(); got.Stale || got.Error != "" || len(got.Releases) != 1 {
		t.Fatalf("recovery=%+v", got)
	}
	// A target deleted after discovery must not be authorized from the cache.
	list = nil
	if ok, err := s.releaseKnown(t.Context(), "0.1.18"); ok || err == nil {
		t.Fatal("deleted target authorized")
	}
	waitUpdateCheck(t, s)
	if len(s.updateCheckSnapshot().Releases) != 0 || s.updateCheckView()["update_status"] != "unknown" {
		t.Fatal("deleted releases survived refresh")
	}
	for _, want := range []string{"update.check started", "update.check finished", "trace=11223344556677889900aabbccddeeff", "release_rate_limited"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing evidence %q", want)
		}
	}
	t.Log(logs.String())
}

func TestUpdateCheckDeduplicatesManualAndCancelsOnStop(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.updates.ctx = ctx
	entered := make(chan struct{})
	var calls atomic.Int32
	s.GitHub = &ghrelease.Client{Query: func(ctx context.Context, _, _ string, _, _ int) ([]ghrelease.Release, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	done, err := s.beginUpdateCheck("", "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	for i := 0; i < 5; i++ {
		mustJSON(t, admin, "POST", "/api/v1/admin/system/update/check", nil, 202)
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate network queries")
	}
	view := mustJSON(t, admin, "GET", "/api/v1/admin/system/update/status", nil, 200)
	if !view["check"].(map[string]any)["checking"].(bool) {
		t.Fatal("lost in-flight state")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel query")
	}
	if s.updateCheckSnapshot().Checking {
		t.Fatal("checking after stop")
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/system/update/check", nil, 503)
}

func TestUpdateCheckStartsSchedulesAndRejectsUser(t *testing.T) {
	if updateCheckInterval != 15*time.Minute {
		t.Fatal("changed cadence")
	}
	s, admin := newInstalledServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	queried := make(chan struct{}, 8)
	s.GitHub = &ghrelease.Client{Query: func(_ context.Context, _, _ string, count, _ int) ([]ghrelease.Release, error) {
		if count == 0 {
			return nil, ghrelease.ErrNotFound
		}
		queried <- struct{}{}
		return nil, nil
	}}
	s.startUpdateChecks(ctx, 50*time.Millisecond)
	for i := 0; i < 2; i++ {
		select {
		case <-queried:
		case <-time.After(2 * time.Second):
			t.Fatal("missing startup/periodic check")
		}
	}
	cancel()
	s.updates.Lock()
	done := s.updates.done
	s.updates.Unlock()
	if done != nil {
		<-done
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "update@test.invalid", "password": "UpdateTest2026"}, 200)
	u := login(t, s, "update@test.invalid", "UpdateTest2026")
	for _, path := range []string{"/api/v1/admin/system/update", "/api/v1/admin/system/update/status"} {
		mustJSON(t, u, "GET", path, nil, 403)
	}
	mustJSON(t, u, "POST", "/api/v1/admin/system/update/check", nil, 403)
	mustJSON(t, u, "POST", "/api/v1/admin/system/update", map[string]string{"action": "update", "version": "0.1.18"}, 403)
}

func TestUpdateCheckNoGateAndUnavailableLatest(t *testing.T) {
	s, _ := newInstalledServer(t)
	s.updates.ctx = t.Context()
	waitUpdateCheck(t, s)
	if got := s.updateCheckSnapshot(); !got.Stale || !strings.Contains(got.Error, "no_capable_gate") {
		t.Fatalf("no Gate=%+v", got)
	}
	old := buildinfo.Version
	buildinfo.Version = "0.1.16"
	defer func() { buildinfo.Version = old }()
	s.updates.snapshot = updateSnapshot{SucceededAt: time.Now(), Releases: []weirRelease{{Version: "0.1.18", Unavailable: "missing"}, {Version: "0.1.16"}}}
	if s.updateCheckView()["update_status"] != "unavailable" {
		t.Fatal("unavailable newer release called current")
	}
	s.updates.snapshot.SucceededAt = time.Now().Add(-time.Hour)
	if s.updateCheckView()["update_status"] != "stale" {
		t.Fatal("old result called current")
	}
}

func TestUpdateSubmissionDirectTargetAndNoIntermediatePackage(t *testing.T) {
	s, admin := newInstalledServer(t)
	updateDir(t)
	oldVersion, oldRole := buildinfo.Version, selfupdate.RoleFile
	buildinfo.Version = "0.1.16"
	selfupdate.RoleFile = filepath.Join(t.TempDir(), "role")
	t.Cleanup(func() { buildinfo.Version = oldVersion; selfupdate.RoleFile = oldRole })
	if err := os.WriteFile(selfupdate.RoleFile, []byte("center"), 0600); err != nil {
		t.Fatal(err)
	}
	s.updates.ctx = t.Context()
	s.GitHub = &ghrelease.Client{Query: func(_ context.Context, _, tag string, _, _ int) ([]ghrelease.Release, error) {
		if tag != "v0.1.18" {
			return nil, errors.New("unexpected intermediate release: " + tag)
		}
		return []ghrelease.Release{completeUpdateRelease("0.1.18")}, nil
	}}
	mustJSON(t, admin, "POST", "/api/v1/admin/system/update", map[string]string{"action": "update", "version": "0.1.18"}, 202)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	s.jobs.Wait(ctx, selfUpdateJob)
	raw, err := os.ReadFile(filepath.Join(selfupdate.Dir, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request selfupdate.Request
	if err = json.Unmarshal(raw, &request); err != nil || request.Version != "0.1.18" {
		t.Fatalf("request=%s err=%v", raw, err)
	}
}

func TestUpdateCheckRestoresLastSuccessAndStopsBeforeStoreClose(t *testing.T) {
	s, _ := newInstalledServer(t)
	s.updates.ctx = t.Context()
	s.GitHub = &ghrelease.Client{Query: func(context.Context, string, string, int, int) ([]ghrelease.Release, error) {
		return []ghrelease.Release{completeUpdateRelease("0.1.18")}, nil
	}}
	waitUpdateCheck(t, s)
	saved := s.updateCheckSnapshot()
	restored := &Server{Store: s.Store, GitHub: &ghrelease.Client{Query: func(context.Context, string, string, int, int) ([]ghrelease.Release, error) {
		return nil, errors.New("offline")
	}}}
	restored.updates.ctx = t.Context()
	waitUpdateCheck(t, restored)
	got := restored.updateCheckSnapshot()
	if got.Error != "offline" || !got.Stale || !got.SucceededAt.Equal(saved.SucceededAt) || len(got.Releases) != 1 {
		t.Fatalf("restored=%+v", got)
	}

	blocked := &Server{Store: s.Store}
	entered := make(chan struct{})
	blocked.GitHub = &ghrelease.Client{Query: func(ctx context.Context, _, _ string, _, _ int) ([]ghrelease.Release, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	blocked.startUpdateChecks(t.Context(), updateCheckInterval)
	<-entered
	blocked.stopUpdateChecks()
	if blocked.updateCheckSnapshot().Checking {
		t.Fatal("shutdown returned while check was running")
	}
}

func TestUpdateCheckTraceReconcilesLaggingReleaseList(t *testing.T) {
	s, admin := newInstalledServer(t)
	cacheUpdateNoticeRecommendations(t, s)
	old := buildinfo.Version
	buildinfo.Version = "0.1.16"
	t.Cleanup(func() { buildinfo.Version = old })
	s.updates.ctx = t.Context()
	list := []ghrelease.Release{completeUpdateRelease("0.1.17")}
	latest := completeUpdateRelease("0.1.18")
	var latestErr error
	s.GitHub = &ghrelease.Client{Query: func(_ context.Context, _, tag string, count, _ int) ([]ghrelease.Release, error) {
		if count > 0 {
			return list, nil
		}
		if tag != "" && tag != "v0.1.18" {
			t.Fatalf("unexpected intermediate target: %s", tag)
		}
		return []ghrelease.Release{latest}, latestErr
	}}
	logs := captureLog(t)
	waitUpdateCheck(t, s)
	view := mustJSON(t, admin, "GET", "/api/v1/admin/system/update/status", nil, 200)
	if view["latest"].(map[string]any)["version"] != "0.1.18" || view["update_status"] != "available" {
		t.Fatalf("lagging list hid update: %v", view)
	}
	found := false
	for _, notice := range s.notices(t.Context()) {
		if notice.ID == "weir-update:0.1.18" {
			found = true
		}
	}
	if !found {
		t.Fatal("notice did not use reconciled snapshot")
	}
	if ok, err := s.releaseKnown(t.Context(), "0.1.18"); !ok || err != nil {
		t.Fatalf("reconciled target rejected: %v", err)
	}
	for _, failure := range []error{errors.New("latest endpoint offline"), ghrelease.ErrNotFound} {
		latestErr = failure
		waitUpdateCheck(t, s)
		got := s.updateCheckSnapshot()
		if !got.Stale || got.Error == "" || got.Releases[0].Version != "0.1.18" || s.updateCheckView()["update_status"] != "failed" {
			t.Fatalf("partial query replaced last success: %+v", got)
		}
	}
	latestErr = nil
	// GitHub's designated latest may have a lower numeric version than the list.
	list = []ghrelease.Release{completeUpdateRelease("0.1.20"), completeUpdateRelease("0.1.18")}
	latest.Assets = nil
	waitUpdateCheck(t, s)
	got := s.updateCheckSnapshot()
	if got.Stale || got.Error != "" || len(got.Releases) != 2 || got.Releases[0].Version != "0.1.20" || got.Releases[1].Unavailable == "" {
		t.Fatalf("numeric ordering or fresh asset replacement failed: %+v", got)
	}
	for _, want := range []string{"update.releases reconciled", "tag=v0.1.18", "trace=11223344556677889900aabbccddeeff", "update.check finished"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing evidence %q", want)
		}
	}
	t.Log(logs.String())
}
