package center

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/packages/contracts"
)

func TestWorkerCandidateFilteringPrecedesPreference(t *testing.T) {
	s, admin := newInstalledServer(t)
	first, second := pairFakeGate(t, admin), pairFakeGate(t, admin)
	memory, disk := uint64(8<<30), uint64(16<<30)
	now := time.Now().UTC()
	for _, g := range []*fakeGate{first, second} {
		heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
		c := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskLayaInstall: contracts.TaskVersion}, Region: "jp", Sources: map[string]bool{"github": true}}
		if err := s.Store.Tasks.SetCapabilities(t.Context(), int(g.id), c); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.recordWorkerResources(t.Context(), int(second.id), contracts.WorkerResources{Concurrency: 1, AvailableMemory: &memory, AvailableDisk: &disk, SourcesObservedAt: &now}); err != nil {
		t.Fatal(err)
	}
	requirements := contracts.TaskRequirements{PreferredGateID: int(first.id), Source: "github", Region: "jp", MemoryBytes: memory, DiskBytes: disk}
	ids, err := s.workerCandidatesFor(t.Context(), contracts.TaskLayaInstall, requirements)
	if err != nil || len(ids) != 1 || ids[0] != int(second.id) {
		t.Fatalf("unknown resources preferred: %v %v", ids, err)
	}
	requirements.TargetGateID = int(first.id)
	if _, err = s.workerCandidatesFor(t.Context(), contracts.TaskLayaInstall, requirements); !errors.Is(err, errNoWorker) {
		t.Fatalf("bound task reassigned: %v", err)
	}
}

func TestWorkerCandidatesPreferRequestedThenIdleLowLoad(t *testing.T) {
	s, admin := newInstalledServer(t)
	first, second := pairFakeGate(t, admin), pairFakeGate(t, admin)
	for _, g := range []*fakeGate{first, second} {
		heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
		if err := s.Store.Tasks.SetCapabilities(t.Context(), int(g.id), contracts.CapabilityDocument{Protocol: 2, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskSubscriptionFetch: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	high, low := .9, .1
	for id, load := range map[int]*float64{int(first.id): &high, int(second.id): &low} {
		if err := s.recordWorkerResources(t.Context(), id, contracts.WorkerResources{Concurrency: 1, Load: load}); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := s.workerCandidates(t.Context(), contracts.TaskSubscriptionFetch, 0)
	if err != nil || len(ids) != 2 || ids[0] != int(second.id) {
		t.Fatalf("load order: %v %v", ids, err)
	}
	ids, err = s.workerCandidates(t.Context(), contracts.TaskSubscriptionFetch, int(first.id))
	if err != nil || ids[0] != int(first.id) {
		t.Fatalf("preference: %v %v", ids, err)
	}
	if s.Store.Driver == store.DriverSQLite {
		var count int
		if err = s.Store.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM kv WHERE key LIKE 'worker-resources:%'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("dynamic observations wrote SQLite: %d %v", count, err)
		}
	}
	nan := math.NaN()
	if err = s.recordWorkerResources(t.Context(), int(first.id), contracts.WorkerResources{Concurrency: 1, Load: &nan}); err == nil {
		t.Fatal("NaN load accepted")
	}
	stale := workerResourceObservation{Resources: contracts.WorkerResources{Concurrency: 1}, ObservedAt: time.Now().Add(-2 * time.Minute)}
	if err = s.Store.Transient.Set(t.Context(), "worker-resources:"+itoa(int(first.id)), jsonText(stale), time.Minute); err != nil {
		t.Fatal(err)
	}
	if observed, err := s.workerResources(t.Context(), int(first.id)); err != nil || observed != nil {
		t.Fatal("stale resources treated as current", err)
	}
}
