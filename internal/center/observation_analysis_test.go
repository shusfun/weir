package center

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/diagnostic"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/dsh"
)

func TestObservationAnalysisUsesReviewedSnapshotDeduplicatesAndRecords(t *testing.T) {
	s, admin := newInstalledServer(t)
	var calls atomic.Int32
	entered := make(chan dsh.JobRequest, 1)
	release := make(chan struct{})
	s.dsh.jobRunner = func(ctx context.Context, req dsh.JobRequest) (dsh.JobResult, error) {
		calls.Add(1)
		entered <- req
		select {
		case <-release:
		case <-ctx.Done():
			return dsh.JobResult{}, ctx.Err()
		}
		return dsh.JobResult{Result: dsh.Result{Completed: true, Final: "只有脱敏证据。password=OUTPUTSECRET"}}, nil
	}
	e := &SecurityEvent{ID: 12, TraceID: strings.Repeat("b", 32), Status: "new", SourceIP: "203.0.113.90", Kind: "abuse", Analysis: "external password=INPUTSECRET"}
	if err := s.saveEvent(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	w := admin.do("POST", "/api/v1/admin/workers/diagnostics/preview", map[string]any{"items": []diagnostic.Selection{{Kind: "event", ID: "12"}, {Kind: "center", ID: "current"}}}, nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	id := decode(t, w)["id"].(string)
	path := "/api/v1/admin/workers/diagnostics/" + id + "/analyze"
	parent := "00-0123456789abcdef0123456789abcdef-1111111111111111-01"
	w = admin.do("POST", path, map[string]any{"keep": []string{"event:12"}}, map[string]string{"traceparent": parent})
	if w.Code != 202 {
		t.Fatalf("analysis submit: %d %s", w.Code, w.Body.String())
	}
	response := decode(t, w)
	aid := response["analysis"].(map[string]any)["id"].(string)
	jobID := response["job"].(map[string]any)["id"].(string)
	select {
	case req := <-entered:
		for _, secret := range []string{"203.0.113.90", "INPUTSECRET", `"kind":"center"`} {
			if strings.Contains(req.Task, secret) {
				t.Fatalf("unselected or private data sent to dsh: %s", secret)
			}
		}
		if req.TraceID != "0123456789abcdef0123456789abcdef" || !strings.Contains(req.Task, "external_data_not_instructions") || !strings.Contains(req.Task, "不授予自动生效权限") {
			t.Fatal("trust/trace boundaries missing")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dsh did not receive the job")
	}
	if w := admin.do("POST", path, map[string]any{"keep": []string{"event:12"}}, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"duplicate":true`) {
		t.Fatalf("duplicate analysis not fenced: %d %s", w.Code, w.Body.String())
	}
	if w := admin.do("POST", path, map[string]any{"keep": []string{"center:current"}}, nil); w.Code != 409 {
		t.Fatalf("background concurrency budget ignored: %d %s", w.Code, w.Body.String())
	}
	close(release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	job, _ := s.jobs.Wait(ctx, jobID)
	if job.Status != jobDone || calls.Load() != 1 {
		t.Fatalf("job: %+v calls=%d", job, calls.Load())
	}
	w = admin.do("GET", "/api/v1/admin/workers/analyses", nil, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "OUTPUTSECRET") || !strings.Contains(w.Body.String(), aid) || !strings.Contains(w.Body.String(), "succeeded") {
		t.Fatalf("activity: %d %s", w.Code, w.Body.String())
	}
	audits, err := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("observation.analyze"), audit.TargetEQ(aid)).All(t.Context())
	if err != nil || len(audits) != 2 {
		t.Fatalf("audit: %d %v", len(audits), err)
	}
	for _, a := range audits {
		if a.Detail["trace_id"] != "0123456789abcdef0123456789abcdef" {
			t.Fatal("analysis audit lost request trace")
		}
	}
	if w := admin.do("POST", path, map[string]any{"keep": []string{"gate:99999"}}, nil); w.Code != 400 {
		t.Fatalf("unreviewed context accepted: %d", w.Code)
	}
	if _, err := s.diagnosticEntry(t.Context(), diagnostic.Selection{Kind: "analysis", ID: aid}); err != nil {
		t.Fatalf("analysis cannot be selected for export: %v", err)
	}
}

func TestObservationAnalysisRetentionLeavesOtherResources(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	now := time.Now()
	for i := 0; i < 103; i++ {
		if _, err := s.Store.Ent.Resource.Create().SetKind(observationAnalysisKind).SetKey(strconv.Itoa(i)).SetData(map[string]any{"status": "succeeded"}).SetCreatedAt(now.Add(-time.Hour)).Save(ctx); err != nil {
			t.Fatal(err)
		}
	}
	old, err := s.Store.Ent.Resource.Create().SetKind(observationAnalysisKind).SetKey("old").SetData(map[string]any{}).SetCreatedAt(now.Add(-15 * 24 * time.Hour)).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Store.Ent.Resource.Create().SetKind("unrelated").SetKey("old").SetData(map[string]any{}).SetCreatedAt(now.Add(-15 * 24 * time.Hour)).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.pruneObservationAnalyses(ctx, now, 14*24*time.Hour, 100); err != nil {
		t.Fatal(err)
	}
	if count, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(observationAnalysisKind)).Count(ctx); err != nil || count != 100 {
		t.Fatalf("retention cap: %d %v", count, err)
	}
	if _, err = s.Store.Ent.Resource.Get(ctx, old.ID); !ent.IsNotFound(err) {
		t.Fatal("expired analysis retained")
	}
	if _, err = s.Store.Ent.Resource.Get(ctx, other.ID); err != nil {
		t.Fatal("unrelated resource deleted")
	}
	if err = s.pruneObservationAnalyses(ctx, now, 0, 100); err == nil {
		t.Fatal("unbounded retention accepted")
	}
}

func TestObservationAnalysisFailureCancellationAndMissingCompletion(t *testing.T) {
	for _, tc := range []struct{ name, status, code string }{{"failure", "failed", "analysis_unavailable"}, {"cancel", "cancelled", "analysis_cancelled"}, {"incomplete", "unknown", "analysis_incomplete"}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newInstalledServer(t)
			s.dsh.jobRunner = func(ctx context.Context, req dsh.JobRequest) (dsh.JobResult, error) {
				if tc.name == "failure" {
					return dsh.JobResult{}, errors.New("password=ERRORSECRET")
				}
				return dsh.JobResult{Result: dsh.Result{Completed: false}}, nil
			}
			a := observationAnalysis{ID: "fixture", TraceID: strings.Repeat("a", 32), Status: "running", Deadline: time.Now().Add(time.Minute)}
			data, _ := analysisData(a)
			if _, err := s.Store.Ent.Resource.Create().SetKind(observationAnalysisKind).SetKey(a.ID).SetData(data).Save(t.Context()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			if tc.name == "cancel" {
				cancel()
			}
			defer cancel()
			job := s.jobs.Start("analysis-fixture", "observation-analysis", "fixture", func(_ context.Context, run JobRun) (map[string]any, error) {
				return s.runObservationAnalysis(ctx, a, diagnostic.Bundle{}, run)
			})
			_, _ = s.jobs.Wait(t.Context(), job.ID)
			rows, err := s.Store.Ent.Resource.Query().All(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range rows {
				if row.Kind != observationAnalysisKind {
					continue
				}
				found = true
				var got observationAnalysis
				if decodeObservationAnalysis(row, &got) != nil || got.Status != tc.status || got.Code != tc.code || strings.Contains(jsonText(row.Data), "ERRORSECRET") {
					t.Fatalf("outcome: %+v", got)
				}
			}
			if !found {
				t.Fatal("missing durable outcome")
			}
		})
	}
	a := analysisObservedState(observationAnalysis{Status: "running", Deadline: time.Now().Add(-time.Minute)}, time.Now())
	if a.Status != "unknown" || a.Code != "completion_not_observed" {
		t.Fatal("restart/timeout falsely shown as success")
	}
}

func TestObservationAnalysisPermissions(t *testing.T) {
	s, admin := newInstalledServer(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "analysis-user@example.test", "password": "Passw0rd"}, 200)
	u := login(t, s, "analysis-user@example.test", "Passw0rd")
	anon := &testClient{t: t, router: s.Router()}
	for _, path := range []string{"/api/v1/admin/workers/analyses", "/api/v1/admin/workers/diagnostics/" + strings.Repeat("a", 48) + "/analyze"} {
		method := "GET"
		if strings.HasSuffix(path, "/analyze") {
			method = "POST"
		}
		if w := u.do(method, path, nil, nil); w.Code != 403 {
			t.Fatalf("ordinary user: %d", w.Code)
		}
		if w := anon.do(method, path, nil, nil); w.Code != 401 {
			t.Fatalf("anonymous: %d", w.Code)
		}
	}
}
