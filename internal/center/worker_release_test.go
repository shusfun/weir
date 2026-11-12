package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/packages/contracts"
)

func TestReleaseMetadataUsesWorkerAndRejectsForgedOwnership(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	now := time.Now().UTC()
	caps := contracts.CapabilityDocument{Sources: map[string]bool{"github": true}, Protocol: 2, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskReleaseQuery: 1}}
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1, SourcesObservedAt: &now}})
	var first contracts.AgentPollReply
	if err := json.Unmarshal(body, &first); err != nil || code != 200 || first.Config == nil {
		t.Fatalf("initial poll: %d %s %v", code, body, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	const trace = "11111111111111112222222222222222"
	type response struct {
		releases []ghrelease.Release
		err      error
	}
	done := make(chan response, 1)
	go func() {
		rs, err := s.weirGitHub().List(withTrace(ctx, trace), "owner/repo", 2)
		done <- response{rs, err}
	}()
	code, body = g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, ConfigRevision: first.Config.Revision, Resources: contracts.WorkerResources{Concurrency: 1, SourcesObservedAt: &now}})
	var next contracts.AgentPollReply
	if err := json.Unmarshal(body, &next); err != nil || code != 200 || len(next.Tasks) != 1 {
		t.Fatalf("poll: %d %s %v", code, body, err)
	}
	task := next.Tasks[0]
	if task.TraceID != trace || task.Kind != contracts.TaskReleaseQuery {
		t.Fatalf("task=%+v", task)
	}
	result := contracts.ReleaseQueryResult{Repository: "other/repo", Releases: []contracts.ReleaseRecord{{Tag: "v1", HTMLURL: "https://github.com/owner/repo/releases/tag/v1", Assets: []contracts.ReleaseAsset{}}}}
	post := func() int {
		raw, _ := json.Marshal(result)
		sum := sha256.Sum256(raw)
		envelope, _ := json.Marshal(contracts.SubscriptionFetchResult{Body: raw, SHA256: hex.EncodeToString(sum[:])})
		status, _ := g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", contracts.TaskUpdate{Attempt: task.Attempt, LeaseToken: task.LeaseToken, Result: envelope})
		return status
	}
	if post() != 400 {
		t.Fatal("forged ownership accepted")
	}
	result.Repository = "owner/repo"
	if post() != 204 {
		t.Fatal("valid release rejected")
	}
	select {
	case got := <-done:
		if got.err != nil || len(got.releases) != 1 || got.releases[0].Tag != "v1" {
			t.Fatalf("query=%+v", got)
		}
	case <-ctx.Done():
		t.Fatal("query did not finish")
	}
}
