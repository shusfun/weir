package center

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/packages/contracts"
)

func TestWorkerCommunicationBatchesActualBodyBytes(t *testing.T) {
	s, _ := newInstalledServer(t)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("gate", &ent.Gate{ID: 3}); c.Next() })
	r.Use(s.agentCommunication)
	r.POST("/agent/test", func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		c.Data(422, "text/plain", append([]byte("拒绝:"), b...))
	})
	data := []byte("秘密不写日志")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/agent/test", bytes.NewReader(data)))
	got, err := s.Store.Tasks.Communication(context.Background(), time.Now().Add(-time.Hour))
	if err != nil || len(got) != 0 {
		t.Fatalf("request synchronously wrote counters: %+v %v", got, err)
	}
	if err = s.flushCommunication(context.Background(), time.Now(), true); err != nil {
		t.Fatal(err)
	}
	got, err = s.Store.Tasks.Communication(context.Background(), time.Now().Add(-time.Hour))
	if err != nil || len(got) != 1 || got[0].GateID != 3 || got[0].Requests != 1 || got[0].Errors != 1 || got[0].Received != int64(len(data)) || got[0].Sent != int64(w.Body.Len()) {
		t.Fatalf("body counters: %+v %v", got, err)
	}
	if err = s.flushCommunication(context.Background(), time.Now(), true); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Store.Tasks.Communication(context.Background(), time.Now().Add(-time.Hour))
	if got[0].Requests != 1 {
		t.Fatal("flush duplicated counters")
	}
}

func TestWorkerTaskTimelineReadsOnlyFromOriginalGate(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	caps := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.ActionCoreRestart: 1, contracts.TaskTraceRead: 1}}
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: contracts.AgentProtocol, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1}})
	var first contracts.AgentPollReply
	if code != 200 || json.Unmarshal(body, &first) != nil {
		t.Fatalf("initial poll: %d %s", code, body)
	}
	created, err := s.enqueueWorkerTask(t.Context(), int(g.id), contracts.ActionCoreRestart, map[string]any{"kernel": "xray"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.Tasks.List(t.Context(), 20)
	if err != nil {
		t.Fatal(err)
	}
	var sourceID string
	for _, item := range items {
		if item.LegacyID == created.ID {
			sourceID = item.Task.ID
		}
	}
	if sourceID == "" {
		t.Fatal("source task was not persisted")
	}
	code, body = g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: contracts.AgentProtocol, ConfigRevision: first.Config.Revision, Resources: contracts.WorkerResources{Concurrency: 1}})
	var sourcePoll contracts.AgentPollReply
	if code != 200 || json.Unmarshal(body, &sourcePoll) != nil || len(sourcePoll.Tasks) != 1 || sourcePoll.Tasks[0].ID != sourceID {
		t.Fatalf("source task poll: %d %s", code, body)
	}
	if code, body = g.call("POST", "/api/v1/agent/tasks/"+sourceID+"/result", contracts.TaskUpdate{Attempt: sourcePoll.Tasks[0].Attempt, LeaseToken: sourcePoll.Tasks[0].LeaseToken, Result: json.RawMessage(`{}`)}); code != 204 {
		t.Fatalf("source task result: %d %s", code, body)
	}
	type response struct {
		code int
		body string
	}
	done := make(chan response, 1)
	go func() {
		w := admin.do("POST", "/api/v1/admin/workers/tasks/"+sourceID+"/timeline", nil, nil)
		done <- response{w.Code, w.Body.String()}
	}()
	code, body = g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: contracts.AgentProtocol, ConfigRevision: first.Config.Revision, Resources: contracts.WorkerResources{Concurrency: 1}})
	var poll contracts.AgentPollReply
	if code != 200 || json.Unmarshal(body, &poll) != nil || len(poll.Tasks) != 1 || poll.Tasks[0].Kind != contracts.TaskTraceRead || poll.Tasks[0].Requirements.TargetGateID != int(g.id) {
		t.Fatalf("timeline task not bound to source Gate: %d %s", code, body)
	}
	traceTask := poll.Tasks[0]
	var request contracts.TraceReadTask
	if err = json.Unmarshal(traceTask.Payload, &request); err != nil {
		t.Fatal(err)
	}
	source, err := s.Store.Tasks.Get(t.Context(), sourceID)
	if err != nil || request.TraceID != source.Task.TraceID || request.Limit != 200 {
		t.Fatalf("wrong timeline query: %+v %v", request, err)
	}
	stage := contracts.TaskStageRecord{TaskID: sourceID, TraceID: request.TraceID, Kind: source.Task.Kind, Attempt: 1, Stage: "execute", Started: time.Now().Add(-time.Second), Finished: time.Now(), Outcome: "failed", ErrorCode: "core_restart_failed", Origin: "gate.restartCore"}
	result, _ := json.Marshal(contracts.TraceReadResult{Records: []contracts.TaskStageRecord{stage}, CollectedAt: time.Now(), Complete: true})
	badResult := contracts.TraceReadResult{Records: []contracts.TaskStageRecord{stage}, CollectedAt: time.Now(), Complete: true}
	badResult.Records[0].TraceID = strings.Repeat("a", 32)
	badRaw, _ := json.Marshal(badResult)
	if code, _ = g.call("POST", "/api/v1/agent/tasks/"+traceTask.ID+"/result", contracts.TaskUpdate{Attempt: traceTask.Attempt, LeaseToken: traceTask.LeaseToken, Result: badRaw}); code != 400 {
		t.Fatalf("unrelated timeline record accepted: %d", code)
	}
	if code, body = g.call("POST", "/api/v1/agent/tasks/"+traceTask.ID+"/result", contracts.TaskUpdate{Attempt: traceTask.Attempt, LeaseToken: traceTask.LeaseToken, Result: result}); code != 204 {
		t.Fatalf("timeline result rejected: %d %s", code, body)
	}
	select {
	case got := <-done:
		if got.code != 200 || !strings.Contains(got.body, "core_restart_failed") || !strings.Contains(got.body, request.TraceID) {
			t.Fatalf("timeline response: %d %s", got.code, got.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeline request did not complete")
	}
	if w := admin.do("POST", "/api/v1/admin/workers/tasks/"+traceTask.ID+"/timeline", nil, nil); w.Code != 409 {
		t.Fatalf("trace task recursively queried: %d %s", w.Code, w.Body.String())
	}
}

func TestWorkerObservationRequiresAdministrator(t *testing.T) {
	s, admin := newInstalledServer(t)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "worker-user@example.test", "password": "Passw0rd"}, 200)
	u := login(t, s, "worker-user@example.test", "Passw0rd")
	anonymous := &testClient{t: t, router: s.Router()}
	for _, path := range []string{"/api/v1/admin/workers/tasks", "/api/v1/admin/workers/tasks/unknown", "/api/v1/admin/workers/tasks/unknown/cancel", "/api/v1/admin/workers/tasks/unknown/timeline", "/api/v1/admin/workers/communication", "/api/v1/admin/ai/profile", "/api/v1/admin/ai/profile/history", "/api/v1/admin/ai/profile/draft"} {
		method := "GET"
		if strings.HasSuffix(path, "/cancel") || strings.HasSuffix(path, "/draft") || strings.HasSuffix(path, "/timeline") {
			method = "POST"
		}
		if w := u.do(method, path, nil, nil); w.Code != 403 {
			t.Fatalf("ordinary user: %s %d %s", path, w.Code, w.Body.String())
		}
		if w := anonymous.do(method, path, nil, nil); w.Code != 401 {
			t.Fatalf("anonymous: %s %d", path, w.Code)
		}
	}
	if w := admin.do("GET", "/api/v1/admin/workers/communication?range=1h", nil, nil); w.Code != 200 {
		t.Fatalf("admin: %d %s", w.Code, w.Body.String())
	}
}
