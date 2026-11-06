package center

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/sample"
	"github.com/shusfun/weir/packages/contracts"
)

func TestResourceHistoryAndTrace(t *testing.T) {
	s, admin, g := setupGate(t)
	ctx := t.Context()
	now := time.Now().UTC()
	cpu, mem := 12.5, uint64(1024)
	h := &contracts.HostSample{ObservedAt: now, ObservationID: "sample-run", CPUPercent: &cpu, Processes: map[string]contracts.ProcessSample{"weir-center": {ObservedAt: now, State: "running", CPUPercent: &cpu, MemoryRSS: &mem}}}
	trace := "0123456789abcdef0123456789abcdef"
	if err := s.recordResourceHost(context.WithValue(ctx, httpTraceKey{}, httpTrace{ID: trace}), sample.ScopeCenter, 1, h, now); err != nil {
		t.Fatal(err)
	}
	row, err := s.Store.Ent.Sample.Query().Where(sample.ScopeEQ(sample.ScopeCenter)).Only(ctx)
	if err != nil || row.TraceID != trace || row.ObservationID != "sample-run" {
		t.Fatalf("row=%+v err=%v", row, err)
	}
	s.resources.host = h
	logs := captureLog(t)
	w := admin.do("GET", "/api/v1/admin/center/resources", nil, map[string]string{"traceparent": "00-" + trace + "-1111111111111111-01"})
	if w.Code != 200 || w.Header().Get("X-Trace-Id") != trace || !strings.Contains(logs.String(), "observation=sample-run") {
		t.Fatalf("response=%d logs=%s", w.Code, logs)
	}
	points := mustJSON(t, admin, "GET", "/api/v1/admin/center/resources/history?hours=1", nil, 200)["points"].([]any)
	p := points[len(points)-1].(map[string]any)
	if p["traffic"] != nil || p["cpu"] != cpu || p["processes"] == nil {
		t.Fatalf("point=%v", p)
	}
	for _, hours := range []int{6, 24} {
		ps, err := s.history(ctx, sample.ScopeCenter, 1, hours)
		if err != nil || len(ps) == 0 || ps[len(ps)-1].Processes["weir-center"].CPUPercent == nil {
			t.Fatalf("hours=%d err=%v", hours, err)
		}
	}
	// Missing fields replace old values in both the snapshot and minute history.
	heartbeat(t, g, contracts.Heartbeat{Host: h})
	heartbeat(t, g, contracts.Heartbeat{Host: &contracts.HostSample{ObservedAt: now}})
	view := mustJSON(t, admin, "GET", "/api/v1/admin/gates/"+itoa(int(g.id)), nil, 200)
	if view["host"].(map[string]any)["cpu_percent"] != nil {
		t.Fatal("old CPU survived missing observation")
	}
	h.ObservedAt = now.Add(-2 * time.Minute)
	if err := s.recordResourceHost(ctx, sample.ScopeCenter, 2, h, now); err != nil {
		t.Fatal(err)
	}
	if count, _ := s.Store.Ent.Sample.Query().Where(sample.ScopeEQ(sample.ScopeCenter), sample.SubjectIDEQ(2)).Count(ctx); count != 0 {
		t.Fatal("stale sample persisted")
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "resource-reader@test.invalid", "password": "ResourceTest2026"}, 200)
	u := &testClient{t: t, router: admin.router}
	// Existing test helpers establish an independent ordinary-user session.
	mustJSON(t, u, "POST", "/api/v1/auth/login", map[string]string{"email": "resource-reader@test.invalid", "password": "ResourceTest2026"}, 200)
	for _, path := range []string{"/api/v1/admin/center/resources", "/api/v1/admin/center/resources/history"} {
		if w := u.do("GET", path, nil, nil); w.Code != 403 {
			t.Fatalf("unauthorized %s=%d", path, w.Code)
		}
	}
}

func TestProcessHistoryAggregationExpiryAndRetention(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	now := time.Now().UTC()
	start := minuteOf(now).Add(time.Minute).Add(-6 * time.Hour)
	at := start.Add(14 * 5 * time.Minute)
	for i, cpu := range []float64{10, 30} {
		tm, rss := at.Add(time.Duration(i)*time.Minute), uint64((i+1)*100)
		h := &contracts.HostSample{ObservedAt: tm, Processes: map[string]contracts.ProcessSample{"xray": {ObservedAt: tm, State: "running", CPUPercent: &cpu, MemoryRSS: &rss}}}
		if err := s.recordResourceHost(ctx, sample.ScopeCenter, 1, h, tm); err != nil {
			t.Fatal(err)
		}
	}
	points, err := s.history(ctx, sample.ScopeCenter, 1, 6)
	if err != nil {
		t.Fatal(err)
	}
	p := points[14].Processes["xray"]
	if p.CPUPercent == nil || *p.CPUPercent != 20 || p.MemoryRSS == nil || *p.MemoryRSS != 200 || len(points[13].Processes) != 0 {
		t.Fatalf("aggregation=%+v", p)
	}
	cpu, rss := 80.0, uint64(1000)
	stale := contracts.ProcessSample{ObservedAt: now.Add(-2 * time.Minute), State: "running", CPUPercent: &cpu, MemoryRSS: &rss}
	if err := s.recordResourceHost(ctx, sample.ScopeCenter, 1, &contracts.HostSample{ObservedAt: now, Processes: map[string]contracts.ProcessSample{"xray": stale}}, now); err != nil {
		t.Fatal(err)
	}
	ps, err := s.history(ctx, sample.ScopeCenter, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	p = ps[len(ps)-1].Processes["xray"]
	if p.CPUPercent != nil || p.MemoryRSS != nil || p.State != "unknown" {
		t.Fatalf("stale=%+v", p)
	}
	old, err := s.Store.Ent.Sample.Create().SetScope(sample.ScopeCenter).SetSubjectID(1).SetAt(now.Add(-26 * time.Hour)).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PruneSamples(ctx, now); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Store.Ent.Sample.Query().Where(sample.IDEQ(old.ID)).Exist(ctx); ok {
		t.Fatal("expired center observation survived cleanup")
	}
	if count, _ := s.Store.Ent.Sample.Query().Where(sample.ScopeEQ(sample.ScopeCenter)).Count(ctx); count != 3 {
		t.Fatalf("recent observations=%d", count)
	}
}
