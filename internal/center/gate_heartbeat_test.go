package center

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/packages/contracts"
)

func TestHeartbeatV2AvoidsUnchangedBusinessWritesAndKeepsPresence(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	var writes atomic.Int64
	s.Store.Ent.Gate.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			writes.Add(1)
			return next.Mutate(ctx, m)
		})
	})
	full := contracts.Heartbeat{Protocol: 2, Full: true, Version: "test", Arch: "amd64", Hostname: "host", Panel: &contracts.PanelStatus{Detected: true, Kind: "1panel"}}
	full.Digest, _ = contracts.HeartbeatDigest(full)
	call := func(h contracts.Heartbeat) contracts.HeartbeatReply {
		t.Helper()
		code, body := g.call("POST", "/api/v1/agent/heartbeat", h)
		var reply contracts.HeartbeatReply
		if code != 200 || json.Unmarshal(body, &reply) != nil {
			t.Fatalf("heartbeat: %d %s", code, body)
		}
		return reply
	}
	first := call(full)
	if first.Digest != full.Digest || first.HeartbeatProtocol != 2 {
		t.Fatal(first)
	}
	writes.Store(0)
	call(full)
	if writes.Load() != 0 {
		t.Fatalf("unchanged full snapshot wrote Gate: %d", writes.Load())
	}
	light := contracts.Heartbeat{Protocol: 2, Digest: full.Digest}
	call(light)
	if writes.Load() != 0 {
		t.Fatalf("light heartbeat wrote Gate: %d", writes.Load())
	}
	stored, err := s.Store.Ent.Gate.Get(t.Context(), int(g.id))
	if err != nil || !gateOnline(stored) {
		t.Fatalf("presence lost: %+v %v", stored, err)
	}
	// A stale durable checkpoint must not hide a live worker from scheduling.
	if err = s.Store.Ent.Gate.UpdateOneID(int(g.id)).SetLastSeenAt(time.Now().Add(-time.Hour)).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Tasks.SetCapabilities(t.Context(), int(g.id), contracts.CapabilityDocument{Protocol: 2, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskSubscriptionFetch: 1}}); err != nil {
		t.Fatal(err)
	}
	if ids, err := s.workerCandidates(t.Context(), contracts.TaskSubscriptionFetch, 0); err != nil || len(ids) != 1 {
		t.Fatalf("live worker omitted: %v %v", ids, err)
	}
	writes.Store(0)
	sample := light
	cpu := 12.0
	sample.Host = &contracts.HostSample{ObservedAt: time.Now(), ObservationID: "0123456789abcdef0123456789abcdef", CPUPercent: &cpu}
	call(sample)
	if writes.Load() != 0 {
		t.Fatal("host history sample rewrote Gate row")
	}
	stored, err = s.Store.Ent.Gate.Get(t.Context(), int(g.id))
	if err != nil || stored.Host["cpu_percent"] != cpu {
		t.Fatalf("latest host observation unavailable: %v %v", stored.Host, err)
	}
	if s.Store.Driver == store.DriverSQLite {
		var count int
		if err = s.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM kv WHERE key LIKE 'gate-observation:%'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("presence persisted per heartbeat: %d %v", count, err)
		}
	}
	// A lost transient snapshot is explicitly reconciled, never acknowledged.
	if err = s.Store.Transient.Del(t.Context(), "gate-observation:"+itoa(int(g.id))); err != nil {
		t.Fatal(err)
	}
	if missing := call(light); !missing.NeedFull || missing.Digest != "" {
		t.Fatal(missing)
	}
	if recovered := call(full); recovered.NeedFull || recovered.Digest != full.Digest {
		t.Fatal(recovered)
	}
	bad := full
	bad.Hostname = "forged"
	if code, _ := g.call("POST", "/api/v1/agent/heartbeat", bad); code != 400 {
		t.Fatalf("forged digest accepted: %d", code)
	}
	bad = light
	bad.Version = "hidden-change"
	if code, _ := g.call("POST", "/api/v1/agent/heartbeat", bad); code != 400 {
		t.Fatalf("partial snapshot mutation accepted: %d", code)
	}
}

func TestHeartbeatV2OfflineCheckpointIsConditional(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	seen := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Microsecond)
	if err := s.Store.Ent.Gate.UpdateOneID(int(g.id)).SetLastSeenAt(seen.Add(-time.Hour)).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetGateObservation(t.Context(), int(g.id), store.GateObservation{SeenAt: seen}); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckpointOfflineGates(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Transient.Del(t.Context(), "gate-observation:"+itoa(int(g.id))); err != nil {
		t.Fatal(err)
	}
	persisted, err := s.Store.Ent.Gate.Get(t.Context(), int(g.id))
	if err != nil || persisted.LastSeenAt == nil || !persisted.LastSeenAt.Equal(seen) {
		t.Fatalf("last offline observation lost: %+v %v", persisted, err)
	}
}
