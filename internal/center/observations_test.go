package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/diagnostic"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/dsh"
	"github.com/shusfun/weir/packages/laya"
)

func readObservation(t *testing.T, s *Server, id string) observationRecord {
	t.Helper()
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(observationKind), resource.KeyEQ(id)).Only(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var r observationRecord
	if err = decodeStrict(row.Data, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestObservationsRegisteredOutcomesDailyAndContentBoundary(t *testing.T) {
	s, admin := newInstalledServer(t)
	choice := "normal"
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			_, _ = w.Write([]byte(`{"version":"0.18.0"}`))
			return
		}
		if r.URL.Path == "/api/show" {
			_ = json.NewEncoder(w).Encode(map[string]string{"digest": laya.ModelManifestSHA256})
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || strings.Contains(jsonText(body), "HIDDENSECRET") || !strings.Contains(jsonText(body), "external_data_not_instructions") {
			t.Error("model evidence boundary missing")
		}
		fmt.Fprintf(w, `{"answers":{"category":{"choice":%q,"confidence":0.9}}}`, choice)
	}))
	defer model.Close()
	mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": true, "url": model.URL, "model": "fixture"}, 200)
	if err := s.saveDSHSettings(t.Context(), DSHSettings{AutoAnalyze: true}); err != nil {
		t.Fatal(err)
	}
	dshCalls := 0
	s.dsh.jobRunner = func(ctx context.Context, req dsh.JobRequest) (dsh.JobResult, error) {
		dshCalls++
		if len(req.Task) > 8000 || strings.Contains(req.Task, "HIDDENSECRET") || !strings.Contains(req.Task, "只能提交草稿") || !validTraceHex(req.TraceID, 32) {
			t.Error("dsh prompt/trace boundary missing")
		}
		return dsh.JobResult{Result: dsh.Result{Completed: true, Final: "建议调查 password=OUTPUTSECRET"}}, nil
	}
	for point := range observationRegistry {
		for _, tc := range []struct{ choice, outcome string }{{"suspicious", "needed"}, {"normal", "not_needed"}, {"unknown", "unknown"}} {
			choice = tc.choice
			ctx := withTrace(t.Context(), strings.Repeat("a", 32))
			input := observationInput{Point: point, Resource: "fixture:" + tc.choice, Text: "upstream diagnostic password=HIDDENSECRET https://example.test/private-token", Origin: "center.processObservation"}
			id, err := s.enqueueObservation(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if duplicate, err := s.enqueueObservation(ctx, input); err != nil || duplicate != id {
				t.Fatal("event deduplication failed", err)
			}
			if err = s.processObservation(ctx); err != nil {
				t.Fatal(err)
			}
			r := readObservation(t, s, id)
			row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(observationKind), resource.KeyEQ(id)).Only(ctx)
			if err != nil || !errors.Is(s.finishObservation(ctx, row.ID, r), errObservationFinalized) {
				t.Fatal("finalized classification accepted a second commit", err)
			}
			if r.Verdict.Outcome != tc.outcome || r.Verdict.Question == "" || r.Verdict.Model != "fixture" || r.Verdict.ModelVersion != laya.ModelManifestSHA256 || r.Verdict.Execution != "center" || r.Config.Confidence != .6 || r.ProfileVersion == "" || r.Day != r.FinishedAt.UTC().Format(time.DateOnly) {
				t.Fatalf("%s: %+v", point, r)
			}
			if observationRegistry[point].ContentOnly && r.DSHStatus != "not_applicable" {
				t.Fatal("spam classifier invoked dsh")
			}
			if strings.Contains(jsonText(r), "OUTPUTSECRET") || strings.Contains(jsonText(r), "HIDDENSECRET") {
				t.Fatal("activity contains a credential")
			}
			if _, err = s.enqueueObservation(ctx, input); err != nil {
				t.Fatal(err)
			}
		}
	}
	if dshCalls != len(observationRegistry)-1 {
		t.Fatalf("dsh invoked for not-needed/unknown/content: %d", dshCalls)
	}
	days := mustJSON(t, admin, "GET", "/api/v1/admin/workers/observations/daily", nil, 200)["items"].([]any)
	if len(days) != len(observationRegistry) {
		t.Fatalf("summary: %v", days)
	}
	for _, item := range days {
		counts := item.(map[string]any)["counts"].(map[string]any)
		for _, outcome := range []string{"needed", "not_needed", "unknown"} {
			if counts[outcome] != float64(1) {
				t.Fatalf("non-atomic summary: %v", counts)
			}
		}
	}
	items := mustJSON(t, admin, "GET", "/api/v1/admin/workers/observations?outcome=not_needed", nil, 200)["items"].([]any)
	if len(items) != len(observationRegistry) {
		t.Fatal("not-needed entries cannot be reviewed")
	}
	if w := admin.do("GET", "/api/v1/admin/workers/observations?outcome=other", nil, nil); w.Code != 400 {
		t.Fatal("unregistered outcome filter accepted")
	}
	if _, err := s.enqueueObservation(t.Context(), observationInput{Point: "external.collector", Resource: "x", Text: "x"}); err == nil {
		t.Fatal("unregistered collector accepted")
	}
}

func TestObservationFreezesConfigAndPreservesUnknown(t *testing.T) {
	s, admin := newInstalledServer(t)
	before, _ := s.aiProfile(t.Context())
	id, err := s.enqueueObservation(t.Context(), observationInput{Point: "content.spam", Resource: "page:1", Text: "external text"})
	if err != nil {
		t.Fatal(err)
	}
	zone := "Asia/Shanghai"
	before.Document.Site.Timezone = &zone
	if err = s.saveAIProfile(t.Context(), before.Document, before.Revision, 1); err != nil {
		t.Fatal(err)
	}
	if err = s.processObservation(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := readObservation(t, s, id)
	if r.Verdict.Outcome != "unknown" || r.Verdict.Code != "not_configured" || r.Timezone != "UTC" || r.ProfileVersion != before.Revision || r.DSHStatus != "not_applicable" {
		t.Fatalf("unavailable/frozen classification: %+v", r)
	}
	if _, err := s.diagnosticEntry(t.Context(), diagnostic.Selection{Kind: "observation", ID: id}); err != nil {
		t.Fatal("observation cannot be selected for manual analysis", err)
	}
	_, err = s.Store.Ent.Resource.Update().Where(resource.KindEQ(observationKind)).SetCreatedAt(time.Now().Add(-15 * 24 * time.Hour)).Save(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.pruneTextObservations(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Store.Ent.Resource.Query().Where(resource.KindEQ(observationKind)).Count(t.Context()); n != 0 {
		t.Fatal("expired observations retained")
	}
	anon := &testClient{t: t, router: s.Router()}
	if w := anon.do("GET", "/api/v1/admin/workers/observations", nil, nil); w.Code != 401 {
		t.Fatal("anonymous observations exposed")
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "observation-user@example.test", "password": "Passw0rd"}, 200)
	u := login(t, s, "observation-user@example.test", "Passw0rd")
	if w := u.do("GET", "/api/v1/admin/workers/observations/daily", nil, nil); w.Code != 403 {
		t.Fatal("ordinary user read observation summaries")
	}
}

func TestObservationTimezoneAndCriteriaValidation(t *testing.T) {
	p, err := builtinAIProfile()
	if err != nil {
		t.Fatal(err)
	}
	bad := "Mars/Unknown"
	p.Timezone = &bad
	if validateAIProfileLayer(p) == nil {
		t.Fatal("invalid timezone accepted")
	}
	zone := "Asia/Shanghai"
	p.Timezone = &zone
	cfg := p.Observations["subscription.health"]
	cfg.Criteria = map[string]string{"normal": "skip", "suspicious": "investigate", "unknown": "unknown"}
	p.Observations["subscription.health"] = cfg
	if err = validateAIProfileLayer(p); err != nil {
		t.Fatal(err)
	}
	cfg.Criteria["execute-shell"] = "bad"
	if validateAIProfileLayer(p) == nil {
		t.Fatal("configuration introduced an answer branch")
	}
}
