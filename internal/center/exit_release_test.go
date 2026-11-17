package center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/exitbinding"
	"github.com/shusfun/weir/packages/contracts"
)

type previewWriteCounter struct {
	store.KV
	writes atomic.Int64
}

func (k *previewWriteCounter) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	k.writes.Add(1)
	return k.KV.Set(ctx, key, value, ttl)
}

func (k *previewWriteCounter) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	k.writes.Add(1)
	return k.KV.SetNX(ctx, key, value, ttl)
}

func (k *previewWriteCounter) Del(ctx context.Context, keys ...string) error {
	k.writes.Add(1)
	return k.KV.Del(ctx, keys...)
}

type previewLockCounter struct{ calls atomic.Int64 }

func (l *previewLockCounter) Do(context.Context, string, func(context.Context) error) error {
	l.calls.Add(1)
	return errors.New("preview attempted persistent lock")
}

func TestExitPreviewWrappersDoNotWrite(t *testing.T) {
	s, _, _ := setupGate(t)
	ctx := t.Context()
	actor, err := s.systemActor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.Store.Ent.User.UpdateOneID(actor.GetInt("user_id")).SetAllowChain(false).SaveX(ctx)
	p := pendingExit{ID: "preview-pending", Revision: 1, Status: "failed", Subject: actor.GetInt("user_id"), Actor: actor.GetInt("user_id"), Body: json.RawMessage(`{"kind":"blackhole"}`)}
	if err := s.savePendingExit(ctx, p); err != nil {
		t.Fatal(err)
	}
	kv := &previewWriteCounter{KV: s.Store.KV}
	lock := &previewLockCounter{}
	s.Store.KV, s.Store.Lock = kv, lock
	var writes atomic.Int64
	s.Store.Ent.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) {
			writes.Add(1)
			return nil, errors.New("preview attempted mutation")
		})
	})
	for _, tc := range []struct {
		name   string
		h      gin.HandlerFunc
		params gin.Params
		body   any
		status int
	}{
		{"save", s.asJob("save", "cancel", func(c *gin.Context) { s.saveExit(c, true) }), nil, map[string]any{"kind": "blackhole"}, 200},
		{"invalid", s.asJob("save", "cancel", func(c *gin.Context) { s.saveExit(c, true) }), nil, map[string]any{"kind": "socks5"}, 422},
		{"denied", func(c *gin.Context) { s.saveExit(c, false) }, nil, map[string]any{"kind": "blackhole"}, 403},
		{"retry", s.retryPendingExit, gin.Params{{Key: "candidate", Value: p.ID}}, map[string]any{"revision": 1}, 200},
		{"discard", s.discardPendingExit, gin.Params{{Key: "candidate", Value: p.ID}}, map[string]any{"revision": 1}, 200},
	} {
		code, result := call(actor, tc.h, "POST", tc.params, nil, tc.body, true)
		if code != tc.status {
			t.Fatalf("%s: %d %v", tc.name, code, result)
		}
	}
	if writes.Load() != 0 || kv.writes.Load() != 0 || lock.calls.Load() != 0 {
		t.Fatalf("preview mutations: DB=%d KV=%d locks=%d", writes.Load(), kv.writes.Load(), lock.calls.Load())
	}
	got, err := s.readPendingExit(ctx, p.ID)
	if err != nil || got.Revision != p.Revision || got.Status != p.Status || string(got.Body) != string(p.Body) {
		t.Fatalf("preview changed candidate: %+v %v", got, err)
	}
}

func TestRetainedExitBindingPreventsSourceDeletion(t *testing.T) {
	s, _, agent := setupGate(t)
	ctx := t.Context()
	source := s.Store.Ent.SubSource.Create().SetName("retained").SetURL("https://example.test/sub").SaveX(ctx)
	ex := s.Store.Ent.Exit.Create().SetKind(entexit.KindBlackhole).SetTag("changed-desired").SaveX(ctx)
	spec := ExitSpec{Kind: "source", SourceID: source.ID, Mode: "filter", Match: "."}
	snapshot, _ := json.Marshal(storedExit{Outbound: contracts.Outbound{Protocol: "socks", Address: "203.0.113.3", Port: 1080}})
	if err := s.bindApplied(ctx, s.Store.Ent, ex.ID, int(agent.id), spec, &exitProbe{resolved: snapshot}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("DELETE", "/internal", nil)
	if s.checkExitSourceDeletion(c, source.ID, 0) || w.Code != 409 {
		t.Fatalf("retained source allowed deletion: %d %s", w.Code, w.Body.String())
	}
}

func TestRoutePreviewWithFetchedProfileDoesNotWrite(t *testing.T) {
	s, _, agent := setupGate(t)
	ctx := t.Context()
	s.AllowPrivateSources = true
	var fetches atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		w.Write([]byte("socks5://203.0.113.9:1080#Preview"))
	}))
	defer upstream.Close()
	src := s.Store.Ent.SubSource.Create().SetName("preview source").SetURL(upstream.URL).SaveX(ctx)
	profile := s.Store.Ent.SubProfile.Create().SetName("preview profile").SetSourceIds([]int{src.ID}).SaveX(ctx)
	specs := []ExitSpec{
		{Kind: "socks5", Address: "203.0.113.9", Port: 1080},
		{Kind: "source", ProfileID: profile.ID, Mode: "filter", Match: "."},
	}
	var exits []*ent.Exit
	for i, spec := range specs {
		exits = append(exits, s.Store.Ent.Exit.Create().SetKind(entexit.Kind(spec.Kind)).SetTag("preview-"+itoa(i)).SetAddress(spec.Address).SetPort(spec.Port).SetSource(spec.source().toMap()).SaveX(ctx))
	}
	kv := &previewWriteCounter{KV: s.Store.KV}
	s.Store.KV = kv
	var dbWrites atomic.Int64
	s.Store.Ent.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			dbWrites.Add(1)
			return nil, errors.New("preview attempted mutation")
		})
	})
	actor := systemActor(ctx)
	for _, ex := range exits {
		code, result := call(actor, s.saveRoute, "POST", gin.Params{{Key: "id", Value: itoa(int(agent.id))}}, nil, map[string]any{"exit_id": ex.ID, "domain": "example.com"}, true)
		if code != 200 {
			t.Fatalf("preview %s: %d %v", ex.Kind, code, result)
		}
	}
	if fetches.Load() == 0 {
		t.Fatal("profile validation did not resolve the source")
	}
	if dbWrites.Load() != 0 || kv.writes.Load() != 0 {
		t.Fatalf("preview writes: database=%d KV=%d", dbWrites.Load(), kv.writes.Load())
	}
	if s.Store.Ent.ExitBinding.Query().CountX(ctx) != 0 || s.Store.Ent.Route.Query().CountX(ctx) != 0 {
		t.Fatal("preview published routing state")
	}
}

func TestRoutePreviewAndFailureDoNotPublishBinding(t *testing.T) {
	s, admin, agent := setupGate(t)
	ctx := t.Context()
	ex := s.Store.Ent.Exit.Create().SetKind(entexit.KindBlackhole).SetTag("preview-exit").SaveX(ctx)
	path := "/api/v1/admin/gates/" + itoa(int(agent.id)) + "/routes"
	mustJSON(t, admin, "POST", path, map[string]any{"exit_id": ex.ID}, 400)
	actor, _ := gin.CreateTestContext(httptest.NewRecorder())
	actor.Request = httptest.NewRequest("POST", path, nil)
	code, result := call(actor, s.saveRoute, "POST", gin.Params{{Key: "id", Value: itoa(int(agent.id))}}, nil, map[string]any{"exit_id": ex.ID, "domain": "example.com"}, true)
	if code != 200 {
		t.Fatalf("preview: %d %v", code, result)
	}
	if s.Store.Ent.ExitBinding.Query().CountX(ctx) != 0 || s.Store.Ent.Route.Query().CountX(ctx) != 0 {
		t.Fatal("preview or invalid request wrote business state")
	}
	s.Store.Ent.Gate.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			return nil, errors.New("revision persistence unavailable")
		})
	})
	mustJSON(t, admin, "POST", path, map[string]any{"exit_id": ex.ID, "domain": "example.com"}, 500)
	if s.Store.Ent.ExitBinding.Query().CountX(ctx) != 0 || s.Store.Ent.Route.Query().CountX(ctx) != 0 {
		t.Fatal("failed revision committed route or binding")
	}
}

func TestExitPublishFailureRetainsSnapshot(t *testing.T) {
	s, _, agent := setupGate(t)
	ctx := t.Context()
	g := s.Store.Ent.Gate.GetX(ctx, int(agent.id))
	ex := s.Store.Ent.Exit.Create().SetKind(entexit.KindSocks5).SetTag("retained-exit").SetAddress("203.0.113.3").SetPort(1080).SaveX(ctx)
	old := ExitSpec{Kind: "socks5", Address: "203.0.113.3", Port: 1080, Password: "keep-secret"}
	if err := s.bindApplied(ctx, s.Store.Ent, ex.ID, g.ID, old, nil); err != nil {
		t.Fatal(err)
	}
	before := s.Store.Ent.ExitBinding.Query().Where(exitbinding.ExitIDEQ(ex.ID)).OnlyX(ctx)
	s.Store.Ent.Gate.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			return nil, errors.New("revision persistence unavailable")
		})
	})
	result := s.applyExitOnGate(ctx, ex, g, ExitSpec{Kind: "blackhole"}, true)
	if result.Status != "kept" || !strings.Contains(result.Reason, "revision persistence unavailable") {
		t.Fatalf("result: %+v", result)
	}
	after := s.Store.Ent.ExitBinding.Query().Where(exitbinding.ExitIDEQ(ex.ID)).OnlyX(ctx)
	if after.Address != before.Address || after.Password != before.Password || after.Kind != before.Kind || !reflect.DeepEqual(after.Resolved, before.Resolved) {
		t.Fatal("failed publish changed applied snapshot")
	}
	if s.Store.Ent.Gate.GetX(ctx, g.ID).ConfigRevision != g.ConfigRevision {
		t.Fatal("failed publish advanced revision")
	}
	if !s.exitBound(ctx, ex.ID, g.ID) {
		t.Fatal("retained snapshot lost its binding")
	}
}

func TestExitSnapshotsAndProbesAreIsolatedByGate(t *testing.T) {
	s, _, agent := setupGate(t)
	ctx := t.Context()
	gid := int(agent.id)
	ex := s.Store.Ent.Exit.Create().SetKind(entexit.KindSource).SetTag("isolated-exit").SaveX(ctx)
	for i, gateID := range []int{gid, gid + 100} {
		address := []string{"203.0.113.1", "203.0.113.2"}[i]
		raw, _ := json.Marshal(storedExit{Outbound: contracts.Outbound{Protocol: "socks", Address: address, Port: 1080}})
		if err := s.bindApplied(ctx, s.Store.Ent, ex.ID, gateID, ExitSpec{Kind: "source", Mode: "group"}, &exitProbe{resolved: raw}); err != nil {
			t.Fatal(err)
		}
		if err := s.saveLocatedProbe(ctx, exitOwner(ex.ID), "gate", gateID, &exitProbe{OK: true, ExitIP: address}); err != nil {
			t.Fatal(err)
		}
		// 共享 KV 内容不参与任何一台 VPS 的配置编译。
		if err := s.Store.KV.Set(ctx, sourceExitKey(exitOwner(ex.ID)), `{"outbound":{"protocol":"blackhole"}}`, 0); err != nil {
			t.Fatal(err)
		}
		out, err := s.compileExitForGate(ctx, ex, gateID)
		if err != nil || out.Address != address {
			t.Fatalf("snapshot for gate %d: %+v %v", gateID, out, err)
		}
		p, ok := s.loadLocatedProbe(ctx, exitOwner(ex.ID), "gate", gateID)
		if !ok || p.ExitIP != address {
			t.Fatalf("probe for gate %d: %+v", gateID, p)
		}
	}
	if _, err := s.compileExitForGate(ctx, ex, gid+200); err == nil {
		t.Fatal("unbound gate borrowed shared snapshot")
	}
	if _, ok, err := s.Store.KV.Get(ctx, "exit-probe:"+exitOwner(ex.ID)+":gate:"+itoa(gid)); err != nil || !ok {
		t.Fatal("probe key differs from migration format", err)
	}
	if _, ok := s.loadLocatedProbe(ctx, exitOwner(ex.ID), "center", 0); ok {
		t.Fatal("center borrowed VPS observation")
	}
}

func TestPendingExitPartialResultIsNotApplied(t *testing.T) {
	for _, tc := range []struct{ body, status string }{
		{`{"applications":[{"gate_id":1,"status":"applied"}]}`, "applied"},
		{`{"applications":[{"gate_id":1,"status":"applied"},{"gate_id":2,"status":"kept","reason":"unreachable"}]}`, "failed"},
		{`{"applications":[{"gate_id":1,"status":"waiting"}]}`, "waiting"},
		{`{"applications":[{"gate_id":1,"status":"waiting"},{"gate_id":2,"status":"failed"}]}`, "failed"},
		{`{"applications":[{"gate_id":1,"status":"unexpected"}]}`, "failed"},
		{`{}`, "failed"}, {`not-json`, "failed"},
	} {
		status, reason := pendingApplyOutcome([]byte(tc.body), true)
		if status != tc.status || reason == "" {
			t.Fatalf("%s => %s %q", tc.body, status, reason)
		}
	}
}

func TestSourceRefreshPublishFailureKeepsGateSnapshot(t *testing.T) {
	s, _, agent := setupGate(t)
	ctx := t.Context()
	gid := int(agent.id)
	source := s.Store.Ent.SubSource.Create().SetName("refresh").SetURL("https://example.test/sub").SaveX(ctx)
	spec := ExitSpec{Kind: "source", SourceID: source.ID, Mode: "filter", Match: "."}
	ex := s.Store.Ent.Exit.Create().SetKind(entexit.KindSource).SetTag("refresh-retained").SetSource(spec.source().toMap()).SaveX(ctx)
	snapshot, _ := json.Marshal(storedExit{Outbound: contracts.Outbound{Protocol: "group", Members: []contracts.Outbound{{Protocol: "trojan", Address: "203.0.113.1", Port: 443, Password: "original"}}}})
	if err := s.bindApplied(ctx, s.Store.Ent, ex.ID, gid, spec, &exitProbe{resolved: snapshot}); err != nil {
		t.Fatal(err)
	}
	before := s.Store.Ent.Gate.GetX(ctx, gid)
	kvCache{s}.Store(ctx, source.ID, []string{"trojan://changed@203.0.113.2:443#next"})
	s.Store.Ent.Gate.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			return nil, errors.New("revision unavailable")
		})
	})
	if err := s.refreshSourceExit(ctx, exitOwner(ex.ID), gid, spec.source(), false, false); err == nil || !strings.Contains(err.Error(), "revision unavailable") {
		t.Fatal("missing publish failure", err)
	}
	got, err := s.compileExitForGate(ctx, ex, gid)
	if err != nil || len(got.Members) != 1 || got.Members[0].Password != "original" || got.Members[0].Address != "203.0.113.1" {
		t.Fatal("lost previous snapshot", err)
	}
	if s.Store.Ent.Gate.GetX(ctx, gid).ConfigRevision != before.ConfigRevision {
		t.Fatal("failed refresh advanced revision")
	}
	if _, ok := s.loadExit(ctx, sourceExitGateOwner(exitOwner(ex.ID), gid)); ok {
		t.Fatal("failed refresh published cache")
	}
}

func TestExitApplyChecksEveryGateKernel(t *testing.T) {
	s, _, agent := setupGate(t)
	ctx := t.Context()
	gid := int(agent.id)
	gate := s.Store.Ent.Gate.GetX(ctx, gid)
	direct, err := s.directExit(ctx, s.Store.Ent, gid)
	if err != nil {
		t.Fatal(err)
	}
	for i, kernel := range []string{"xray", "sing-box"} {
		s.Store.Ent.Node.Create().SetGateID(gid).SetName(kernel).SetExitID(direct.ID).SetKernel(kernel).SetProtocol("shadowsocks").SetPort(31000 + i).SaveX(ctx)
	}
	// A missing pinned artifact on the second kernel must prevent publication.
	gate = gate.Update().SetKernelPins(map[string]string{"sing-box": "0.0.0-missing"}).SaveX(ctx)
	ex := s.Store.Ent.Exit.Create().SetKind(entexit.KindSocks5).SetTag("multi-kernel").SetAddress("203.0.113.9").SetPort(1080).SaveX(ctx)
	result := s.applyExitOnGate(ctx, ex, gate, ExitSpec{Kind: "socks5", Address: ex.Address, Port: ex.Port}, true)
	if result.Status != "failed" || !strings.Contains(result.Reason, "sing-box") {
		t.Fatalf("second kernel not checked: %+v", result)
	}
	if s.Store.Ent.ExitBinding.Query().CountX(ctx) != 0 {
		t.Fatal("incompatible kernel published binding")
	}
}
