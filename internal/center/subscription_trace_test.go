package center

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/diagnostic"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/subgroup"
	"github.com/shusfun/weir/packages/contracts"
)

func TestSubscriptionTimelinePersistsNestedStagesAndDiagnostic(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	node := s.Store.Ent.SubNode.Create().SetName("leaf").SetURL("trojan://sensitive-password@203.0.113.1:443#leaf").SaveX(ctx)
	child := s.Store.Ent.SubProfile.Create().SetName("child").SetNodeIds([]int{node.ID}).SaveX(ctx)
	root := s.Store.Ent.SubProfile.Create().SetName("root").SetMembers([]contracts.SubscriptionMember{{Kind: "profile", ID: child.ID}}).SaveX(ctx)
	w := admin.do("POST", "/api/v1/admin/subscriptions/profiles/"+itoa(root.ID)+"/preview", map[string]any{"target": "base64"}, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	trace := w.Header().Get("X-Trace-Id")
	rows := mustJSON(t, admin, "GET", "/api/v1/admin/subscriptions/runs", nil, 200)["items"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["trace_id"] != trace {
		t.Fatal("HTTP operation lost its persisted trace", rows, trace)
	}
	id := rows[0].(map[string]any)["id"].(string)
	run, err := s.loadSubscriptionRun(ctx, id)
	if err != nil || len(run.Stages) != 4 || run.Stages[0].ProfileID != child.ID || run.Stages[1].ProfileID != root.ID || run.Stages[2].Operation != "subscription.render" || run.Stages[3].Operation != "subscription.response" {
		t.Fatal(run, err)
	}
	e, err := s.diagnosticEntry(ctx, diagnostic.Selection{Kind: "subscription-run", ID: id})
	if err != nil || len(e.Reproduction) == 0 || !strings.Contains(mustString(e.Evidence), "Combine") || strings.Contains(mustString(e), "sensitive-password") {
		t.Fatal("diagnostic lost source/test mapping or leaked node", e, err)
	}
	readTrace := "12345678123456781234567812345678"
	if _, _, _, _, err := s.groupNodes(withTrace(ctx, readTrace), root, "clash-meta/2.5.0", false); err != nil {
		t.Fatal(err)
	}
	rows = mustJSON(t, admin, "GET", "/api/v1/admin/subscriptions/runs", nil, 200)["items"].([]any)
	latest, _ := s.loadSubscriptionRun(ctx, rows[0].(map[string]any)["id"].(string))
	if latest.TraceID != readTrace || latest.Stages[0].Operation != "subscription.cache_hit" {
		t.Fatal("cache branch not persisted", latest)
	}
	child.Update().SetEnabled(false).ExecX(ctx)
	if _, _, _, _, err := s.groupNodes(ctx, root, "", false); err == nil {
		t.Fatal("inactive child accepted")
	}
	rows = mustJSON(t, admin, "GET", "/api/v1/admin/subscriptions/runs", nil, 200)["items"].([]any)
	latest, _ = s.loadSubscriptionRun(ctx, rows[0].(map[string]any)["id"].(string))
	if latest.Outcome != "failed" || latest.Stages[0].ErrorCode != "profile_member_inactive" {
		t.Fatal("member rejection omitted", latest)
	}
	viewer := &testClient{t: t, router: admin.router}
	if w := viewer.do("GET", "/api/v1/admin/subscriptions/runs/"+id, nil, nil); w.Code != 401 {
		t.Fatal("anonymous timeline access", w.Code)
	}
}

func TestSubscriptionTimelineRenderFailureIsNotSuccess(t *testing.T) {
	s, admin := newInstalledServer(t)
	p := s.Store.Ent.SubProfile.Create().SetName("render failure").SaveX(t.Context())
	w := admin.do("POST", "/api/v1/admin/subscriptions/profiles/"+itoa(p.ID)+"/preview", map[string]any{"target": "unsupported-format"}, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "output_error") {
		t.Fatal(w.Code, w.Body)
	}
	rows := mustJSON(t, admin, "GET", "/api/v1/admin/subscriptions/runs", nil, 200)["items"].([]any)
	if len(rows) != 1 {
		t.Fatal("nested recorder produced duplicate runs", rows)
	}
	run, err := s.loadSubscriptionRun(t.Context(), rows[0].(map[string]any)["id"].(string))
	if err != nil || run.Outcome != "failed" || len(run.Stages) != 3 || run.Stages[1].ErrorCode != "unsupported_target" || run.Stages[2].Operation != "subscription.response" {
		t.Fatal(run, err)
	}
}

func TestSubscriptionTimelineCancellationAndDeadline(t *testing.T) {
	for _, outcome := range []string{"cancelled", "timed_out"} {
		t.Run(outcome, func(t *testing.T) {
			s, _ := newInstalledServer(t)
			var ctx context.Context
			var cancel context.CancelFunc
			if outcome == "timed_out" {
				ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
			} else {
				ctx, cancel = context.WithCancel(t.Context())
			}
			ctx, finish := s.startSubscriptionRun(ctx, 0)
			cancel()
			finish(ctx.Err())
			finish(nil)
			rows := s.Store.Ent.Resource.Query().Where(resource.KindEQ(subscriptionRunKind)).AllX(t.Context())
			if len(rows) != 1 || rows[0].Data["outcome"] != outcome {
				t.Fatal("lost terminal status or duplicate finalization", rows)
			}
		})
	}
}

func TestSubscriptionTimelineBoundedRedactedAndPruned(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx, finish := s.startSubscriptionRun(t.Context(), 0)
	now := time.Now().UTC()
	for i := 0; i < 520; i++ {
		recordSubscriptionStage(ctx, 0, subgroup.Stage{Operation: "subscription.source", SourceID: 1, Started: now, Finished: now, Error: "Get https://example.com?token=do-not-store: request failed"}, "subgroup.fetchSource")
	}
	finish(nil)
	row := s.Store.Ent.Resource.Query().Where(resource.KindEQ(subscriptionRunKind)).OnlyX(t.Context())
	var run subscriptionRun
	raw, _ := json.Marshal(row.Data)
	if json.Unmarshal(raw, &run) != nil || run.Complete || len(run.Stages) != 512 || strings.Contains(string(raw), "do-not-store") || run.Outcome != "failed" {
		t.Fatal("unbounded or unredacted timeline", string(raw))
	}
	row.Update().SetCreatedAt(now.Add(-15 * 24 * time.Hour)).ExecX(t.Context())
	if err := s.pruneTextObservations(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if s.Store.Ent.Resource.Query().Where(resource.KindEQ(subscriptionRunKind)).CountX(t.Context()) != 0 {
		t.Fatal("expired subscription timeline retained")
	}
}
