package center

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

func relayFixture(body []byte) contracts.AssetFetch {
	sum := sha256.Sum256(body)
	return contracts.AssetFetch{URLs: []string{"https://github.com/example/project/releases/download/v1/component"}, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body)), MaxSize: 2 << 20}
}

func TestRelayChunksSurviveCenterReplacementAndFencePreviousConsumer(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	body := bytes.Repeat([]byte("x"), contracts.UploadChunk+13)
	req := relayFixture(body)
	session, err := s.openRelay(t.Context(), int(g.id), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.openRelay(t.Context(), int(g.id), req); err == nil || err.Error() != "relay_transfer_busy" {
		t.Fatal("second consumer bypassed global transfer slot", err)
	}
	if _, err = s.openRelay(t.Context(), int(g.id), relayFixture([]byte("different"))); err == nil {
		t.Fatal("concurrent distinct transfer accepted")
	}
	session, err = s.writeRelayChunk(t.Context(), session.ID, int(g.id), 0, body[:contracts.UploadChunk], 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	// A replacement process has no callbacks, upload map or original path.
	replacement := &Server{Store: s.Store}
	loaded, err := replacement.loadRelay(t.Context(), session.ID, int(g.id))
	if err != nil || loaded.Received != contracts.UploadChunk {
		t.Fatalf("durable offset: %+v %v", loaded, err)
	}
	if _, err = replacement.loadRelay(t.Context(), session.ID, int(g.id)+1); err == nil {
		t.Fatal("cross-Gate upload ownership bypassed")
	}
	if _, err = replacement.writeRelayChunk(t.Context(), session.ID, int(g.id), 0, body[:contracts.UploadChunk], 128<<20); err == nil || err.Error() != "offset_mismatch" {
		t.Fatal("duplicate bytes appended", err)
	}
	loaded.ConsumerUntil = time.Now().Add(-time.Minute)
	if err = s.Store.Ent.Resource.Update().Where(resource.KindEQ(relaySessionKind), resource.KeyEQ(session.ID)).SetData(toMap(loaded)).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.openRelay(t.Context(), int(g.id), req)
	if err != nil || resumed.ID != session.ID || resumed.ActionID != session.ActionID || resumed.ConsumerToken == session.ConsumerToken || resumed.Received != session.Received {
		t.Fatalf("restart resume: %+v %v", resumed, err)
	}
	if err = s.closeRelay(t.Context(), session.ID, int(g.id), session.ConsumerToken); err == nil || err.Error() != "relay_consumer_lost" {
		t.Fatal("stale consumer closed resumed transfer", err)
	}
	resumed, err = replacement.writeRelayChunk(t.Context(), session.ID, int(g.id), contracts.UploadChunk, body[contracts.UploadChunk:], 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "verified-component")
	if err = replacement.materializeRelay(t.Context(), resumed, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("materialized content differs")
	}
	if err = s.closeRelay(t.Context(), resumed.ID, int(g.id), resumed.ConsumerToken); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(relayChunkKind), resource.KeyHasPrefix(session.ID+":")).Count(t.Context()); err != nil || n != 0 {
		t.Fatal("closed transfer retained binary chunks")
	}
}

func TestRelayRejectsCorruptionWithoutReplacingDestinationAndPrunesExpiredChunks(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	req := relayFixture([]byte("expected"))
	session, err := s.openRelay(t.Context(), int(g.id), req)
	if err != nil {
		t.Fatal(err)
	}
	session, err = s.writeRelayChunk(t.Context(), session.ID, int(g.id), 0, []byte("bad-data"), 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "component")
	if err = os.WriteFile(destination, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.materializeRelay(t.Context(), session, destination); err == nil || err.Error() != "relay_digest_mismatch" {
		t.Fatal("bad digest accepted", err)
	}
	if got, _ := os.ReadFile(destination); string(got) != "previous" {
		t.Fatal("unverified bytes replaced destination")
	}
	old := time.Now().Add(-3 * time.Hour)
	if err = s.Store.Ent.Resource.Update().Where(resource.KindEQ(relaySessionKind), resource.KeyEQ(session.ID)).SetCreatedAt(old).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = s.pruneRelaySessions(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(relayChunkKind)).Count(t.Context()); err != nil || n != 0 {
		t.Fatal("expired binary chunks retained")
	}
}

func TestRelayUploadRequiresTheCurrentWorkerLease(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	session, err := s.openRelay(t.Context(), int(g.id), relayFixture([]byte("payload")))
	if err != nil {
		t.Fatal(err)
	}
	caps := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.ActionAssetFetch: contracts.TaskVersion}}
	if err = s.Store.Tasks.SetCapabilities(t.Context(), int(g.id), caps); err != nil {
		t.Fatal(err)
	}
	if err = s.migratePendingActions(t.Context(), int(g.id)); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Store.Tasks.Claim(t.Context(), int(g.id), caps, contracts.WorkerResources{Concurrency: 1}, time.Now())
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	task := claimed[0].Task
	path := "/api/v1/agent/uploads/" + session.ID
	header := map[string]string{"Authorization": "Bearer " + g.bearer, "X-Weir-Task-ID": task.ID, "X-Weir-Task-Attempt": strconv.Itoa(task.Attempt), "X-Weir-Task-Lease": task.LeaseToken}
	if w := g.c.do("GET", path, nil, header); w.Code != 200 {
		t.Fatalf("valid lease: %d %s", w.Code, w.Body.String())
	}
	header["X-Weir-Task-Lease"] = strings.Repeat("0", 48)
	if w := g.c.do("GET", path, nil, header); w.Code != 409 {
		t.Fatalf("stale lease: %d", w.Code)
	}
	if code, _ := g.call("GET", path, nil); code != 409 {
		t.Fatalf("new worker bypassed lease with legacy upload: %d", code)
	}
	update := contracts.TaskUpdate{Attempt: task.Attempt, LeaseToken: task.LeaseToken, Result: json.RawMessage(`{"size":7}`)}
	if code, _ := g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", update); code != 400 {
		t.Fatalf("accepted completion before upload: %d", code)
	}
	current, err := s.writeRelayChunk(t.Context(), session.ID, int(g.id), 0, []byte("payload"), 128<<20)
	if err != nil || current.Received != 7 {
		t.Fatalf("upload: %+v %v", current, err)
	}
	if code, body := g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", update); code != 204 {
		t.Fatalf("valid completed upload: %d %s", code, body)
	}
}

func TestRelayChecksLeaseAfterRateWaitAndCleansExpiredSession(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	session, err := s.openRelay(t.Context(), int(g.id), relayFixture([]byte("data")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.writeRelayChunk(t.Context(), session.ID, int(g.id), 0, []byte("data"), 128<<20, func(context.Context) error { return errors.New("task_lease_lost") })
	if err == nil || err.Error() != "task_lease_lost" {
		t.Fatal("lost lease wrote bytes", err)
	}
	current, err := s.loadRelay(t.Context(), session.ID, int(g.id))
	if err != nil || current.Received != 0 || current.Chunks != 0 {
		t.Fatal("failed lease changed durable progress", err)
	}
	current, err = s.writeRelayChunk(t.Context(), session.ID, int(g.id), 0, []byte("data"), 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	current.CreatedAt = time.Now().Add(-3 * time.Hour)
	current.ExpiresAt = current.CreatedAt.Add(relayTransferTimeout)
	if err = s.Store.Ent.Resource.Update().Where(resource.KindEQ(relaySessionKind), resource.KeyEQ(session.ID)).SetData(toMap(current)).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = s.closeRelay(t.Context(), current.ID, int(g.id), current.ConsumerToken); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(relayChunkKind)).Count(t.Context()); err != nil || n != 0 {
		t.Fatal("expired close retained chunks", err)
	}
}
