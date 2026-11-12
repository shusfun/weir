package center

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/diagnostic"
	"github.com/shusfun/weir/internal/layahost"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

func approvedCenterLaya() layahost.Deployment {
	digest := "sha256:" + strings.Repeat("a", 64)
	return layahost.Deployment{Schema: 1, Version: "v0.18.0", Model: laya.DefaultModel,
		Spec:   laya.DockerSpec{Name: "center-laya", Repository: "ghcr.io/ollaya-dev/ollaya", Digest: digest, ModelVolume: "center-laya-models", UID: 1000, GID: 1000, HostPort: 11435, MemoryBytes: 2 << 30, CPUMillis: 1000},
		Policy: laya.DockerPolicy{Repositories: []string{"ghcr.io/ollaya-dev/ollaya"}, Digests: []string{digest}, MaxMemoryBytes: 2 << 30, MaxCPUMillis: 1000}}
}

func hostingPreview(t *testing.T, admin *testClient, operation string) layaHostPreview {
	t.Helper()
	w := admin.do("POST", "/api/v1/admin/defense/laya/hosting/preview", map[string]string{"operation": operation}, nil)
	var preview layaHostPreview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	return preview
}

func TestLayaHostingPreviewCannotMutateAndApprovalCannotDrift(t *testing.T) {
	s, admin := newInstalledServer(t)
	doc, mutations := approvedCenterLaya(), 0
	s.layaHosting.load = func() (layahost.Deployment, error) { return doc, nil }
	s.layaHosting.operate = func(_ context.Context, req layahost.Request) (layahost.Response, error) {
		if req.Operation != "status" {
			mutations++
		}
		return layahost.Response{Version: doc.Version, Model: doc.Model, Status: laya.DockerStatus{State: laya.StateRunning}}, nil
	}
	p := hostingPreview(t, admin, "stop")
	if mutations != 0 || p.Before != laya.StateRunning {
		t.Fatal("preview changed the runtime")
	}
	doc.Spec.CPUMillis = 500
	w := admin.do("POST", "/api/v1/admin/defense/laya/hosting/"+p.ID+"/confirm", nil, nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "laya_deployment_changed") || mutations != 0 {
		t.Fatalf("approval drift accepted: %d %s", w.Code, w.Body.String())
	}
	if w = admin.do("POST", "/api/v1/admin/defense/laya/hosting/preview", map[string]any{"operation": "ensure", "args": []string{"--privileged"}}, nil); w.Code != 400 {
		t.Fatal("arbitrary container arguments accepted")
	}
	if w = admin.do("POST", "/api/v1/admin/defense/laya/hosting/preview", map[string]string{"operation": "ensure"}, nil); w.Code != 409 || !strings.Contains(w.Body.String(), "laya_resources_unknown") {
		t.Fatalf("missing resources became available: %d %s", w.Code, w.Body.String())
	}
}

func TestLayaHostingConfirmationIsIdempotentAndActivitiesSurviveJobs(t *testing.T) {
	s, admin := newInstalledServer(t)
	doc := approvedCenterLaya()
	var mu sync.Mutex
	mutations := 0
	s.layaHosting.load = func() (layahost.Deployment, error) { return doc, nil }
	s.layaHosting.operate = func(_ context.Context, req layahost.Request) (layahost.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if req.Operation != "status" {
			mutations++
		}
		return layahost.Response{Version: doc.Version, Model: doc.Model, Status: laya.DockerStatus{State: laya.StateStopped}}, nil
	}
	p := hostingPreview(t, admin, "stop")
	const trace = "0123456789abcdef0123456789abcdef"
	w := admin.do("POST", "/api/v1/admin/defense/laya/hosting/"+p.ID+"/confirm", nil, map[string]string{"traceparent": "00-" + trace + "-1111111111111111-01"})
	if w.Code != 202 {
		t.Fatalf("confirm: %d %s", w.Code, w.Body.String())
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	job, ok := s.jobs.Wait(ctx, "laya:"+p.ID)
	if !ok || job.Status != jobDone {
		t.Fatalf("job=%+v", job)
	}
	w = admin.do("POST", "/api/v1/admin/defense/laya/hosting/"+p.ID+"/confirm", nil, nil)
	mu.Lock()
	count := mutations
	mu.Unlock()
	if w.Code != 200 || count != 1 {
		t.Fatalf("replayed mutation: %d %d", w.Code, count)
	}
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(layaOperationKind), resource.KeyEQ(s.layaHostKey(p.ID))).Only(ctx)
	var op layaHostOperation
	if err != nil || decodeStrict(row.Data, &op) != nil || op.Status != "succeeded" || op.TraceID != trace || len(op.Stages) != 1 || op.Stages[0].Finished.Before(op.Stages[0].Started) {
		t.Fatalf("activity missing: %+v %v", op, err)
	}
	if n, err := s.Store.Ent.Audit.Query().Where(audit.ActionIn("laya.host.confirm", "laya.host.result"), audit.TargetEQ(p.ID)).Count(ctx); err != nil || n != 2 {
		t.Fatalf("audit: %d %v", n, err)
	}
	e, err := s.diagnosticEntry(ctx, diagnostic.Selection{Kind: "laya-operation", ID: p.ID})
	if err != nil || e.Evidence["stage_sources"] == nil || e.ExternalData["approved_version"] == "" {
		t.Fatalf("diagnostic: %+v %v", e, err)
	}
}

func TestLayaHostingPreviewBoundToActorHostAndExpiry(t *testing.T) {
	s, admin := newInstalledServer(t)
	doc := approvedCenterLaya()
	s.layaHosting.load = func() (layahost.Deployment, error) { return doc, nil }
	s.layaHosting.operate = func(context.Context, layahost.Request) (layahost.Response, error) {
		return layahost.Response{}, &contracts.TaskError{Code: "laya_container_missing"}
	}
	p := hostingPreview(t, admin, "remove")
	for _, change := range []func(*layaHostPreview){
		func(p *layaHostPreview) { p.Actor++ },
		func(p *layaHostPreview) { p.HostID = strings.Repeat("b", 64) },
		func(p *layaHostPreview) { p.ExpiresAt = time.Now().Add(-time.Minute) },
	} {
		bad := p
		change(&bad)
		sealed, _ := s.Crypto.Encrypt(jsonText(bad))
		if err := s.Store.KV.Set(t.Context(), "laya-host-preview:"+p.ID, sealed, time.Minute); err != nil {
			t.Fatal(err)
		}
		w := admin.do("POST", "/api/v1/admin/defense/laya/hosting/"+p.ID+"/confirm", nil, nil)
		if w.Code != 404 {
			t.Fatalf("foreign or expired approval: %d %s", w.Code, w.Body.String())
		}
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "laya-user@example.test", "password": "Passw0rd"}, 200)
	u := login(t, s, "laya-user@example.test", "Passw0rd")
	anon := &testClient{t: t, router: s.Router()}
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/admin/workers/system"}, {"GET", "/api/v1/admin/defense/laya/hosting"}, {"POST", "/api/v1/admin/defense/laya/hosting/preview"}, {"POST", "/api/v1/admin/defense/laya/hosting/" + p.ID + "/confirm"}} {
		if w := u.do(route.method, route.path, nil, nil); w.Code != 403 {
			t.Fatalf("ordinary user: %s %d", route.path, w.Code)
		}
		if w := anon.do(route.method, route.path, nil, nil); w.Code != 401 {
			t.Fatalf("anonymous: %s %d", route.path, w.Code)
		}
	}
}

func TestLayaHostingInterruptedResultsRemainUnknownWithoutReplaying(t *testing.T) {
	s, _ := newInstalledServer(t)
	now := time.Now().UTC()
	op := layaHostOperation{ID: strings.Repeat("a", 48), Status: "running", Stage: "container.ensure", TraceID: strings.Repeat("b", 32), CreatedAt: now.Add(-time.Hour), Deadline: now.Add(-30 * time.Minute)}
	if _, err := s.Store.Ent.Resource.Create().SetKind(layaOperationKind).SetKey(s.layaHostKey(op.ID)).SetCreatedAt(op.CreatedAt).SetData(toMap(op)).Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.layaHosting.operate = func(context.Context, layahost.Request) (layahost.Response, error) {
		t.Error("recovery must not blindly replay a mutation")
		return layahost.Response{}, errors.New("unexpected")
	}
	if err := s.maintainLayaHosting(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(layaOperationKind), resource.KeyEQ(s.layaHostKey(op.ID))).Only(t.Context())
	if err != nil || decodeStrict(row.Data, &op) != nil || op.Status != "unknown" || op.Code != "laya_operation_interrupted" || op.FinishedAt != nil {
		t.Fatalf("recovery outcome: %+v %v", op, err)
	}
}
