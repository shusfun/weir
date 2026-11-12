package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/packages/contracts"
)

func TestSubscriptionFetchUsesWorkerAndChecksDigest(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	caps := contracts.CapabilityDocument{Protocol: 2, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskSubscriptionFetch: 1}}
	code, body := g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, Capabilities: &caps, Resources: contracts.WorkerResources{Concurrency: 1}})
	if code != 200 {
		t.Fatalf("capabilities: %d %s", code, body)
	}
	var first contracts.AgentPollReply
	_ = json.Unmarshal(body, &first)
	type answer struct {
		body     []byte
		location string
		userInfo string
		err      error
	}
	done := make(chan answer, 1)
	const traceID = "0123456789abcdef0123456789abcdef"
	router := gin.New()
	router.Use(requestLog)
	router.GET("/fetch", func(c *gin.Context) {
		r, e := s.fetchSubscription(c.Request.Context(), "https://subscription.example/private-token", "test-UA")
		done <- answer{r.Body, r.Location, r.UserInfo, e}
		c.Status(200)
	})
	request := httptest.NewRequest("GET", "/fetch", nil).WithContext(ctx)
	request.Header.Set("traceparent", "00-"+traceID+"-1111111111111111-01")
	response := httptest.NewRecorder()
	httpDone := make(chan struct{})
	go func() { defer close(httpDone); router.ServeHTTP(response, request) }()
	code, body = g.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: 2, ConfigRevision: first.Config.Revision, Resources: contracts.WorkerResources{Concurrency: 1}})
	var poll contracts.AgentPollReply
	_ = json.Unmarshal(body, &poll)
	if code != 200 || len(poll.Tasks) != 1 {
		t.Fatalf("dispatch: %d %s", code, body)
	}
	task := poll.Tasks[0]
	if task.TraceID != traceID {
		t.Fatalf("HTTP trace lost at dispatch: %q", task.TraceID)
	}
	var req contracts.SubscriptionFetchTask
	_ = json.Unmarshal(task.Payload, &req)
	if req.Credentials.URL != "https://subscription.example/private-token" {
		t.Fatal("credential not dispatched")
	}
	nodes := []byte("ss://example-node")
	sum := sha256.Sum256(nodes)
	res := contracts.SubscriptionFetchResult{Body: nodes, SHA256: strings.Repeat("0", 64)}
	raw, _ := json.Marshal(res)
	u := contracts.TaskUpdate{Attempt: task.Attempt, LeaseToken: task.LeaseToken, Result: raw}
	code, _ = g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", u)
	if code != 400 {
		t.Fatalf("wrong digest accepted: %d", code)
	}
	res.SHA256 = hex.EncodeToString(sum[:])
	res.UserInfo = "upload=10; download=20; total=1000"
	u.Result, _ = json.Marshal(res)
	code, body = g.call("POST", "/api/v1/agent/tasks/"+task.ID+"/result", u)
	if code != 204 {
		t.Fatalf("result: %d %s", code, body)
	}
	select {
	case got := <-done:
		if got.err != nil || string(got.body) != string(nodes) || got.location != "gate:"+itoa(int(g.id)) || got.userInfo != res.UserInfo {
			t.Fatalf("fetch: %+v", got)
		}
	case <-ctx.Done():
		t.Fatal("fetch did not finish")
	}
	<-httpDone
	if response.Header().Get("X-Trace-Id") != task.TraceID {
		t.Fatalf("response/worker trace mismatch: %v", response.Header())
	}
	r, e := s.Store.Tasks.Get(ctx, task.ID)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(r.Ciphertext, "private-token") || strings.Contains(string(r.Result), "example-node") {
		t.Fatal("plaintext retained in task storage")
	}
}

func TestWorkerNotificationCrossInstance(t *testing.T) {
	s, _ := newInstalledServer(t)
	if s.Store.Driver != store.DriverPostgres {
		t.Skip("requires PostgreSQL + Redis")
	}
	other, e := store.Open(t.Context(), s.Config.Store)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	wake, unsubscribe := other.Notifications.Subscribe("gate:123")
	defer unsubscribe()
	if e = s.Store.Notifications.Publish(t.Context(), "gate:123"); e != nil {
		t.Fatal(e)
	}
	select {
	case <-wake:
	case <-time.After(3 * time.Second):
		t.Fatal("Redis did not wake another center instance")
	}
}
