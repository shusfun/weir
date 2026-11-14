package center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
	"github.com/shusfun/weir/packages/laya"
)

func TestLayaAsyncCoalescesAndRejectsStaleOrCancelledResults(t *testing.T) {
	for _, outcome := range []string{"success", "changed", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			s, admin := newInstalledServer(t)
			entered, release := make(chan struct{}, 1), make(chan struct{})
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/version":
					w.Write([]byte(`{"version":"0.18.0"}`))
				case "/api/show":
					json.NewEncoder(w).Encode(map[string]string{"digest": laya.ModelManifestSHA256})
				default:
					calls.Add(1)
					entered <- struct{}{}
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					w.Write([]byte(`{"answers":{"category":{"choice":"normal","confidence":0.99}}}`))
				}
			}))
			defer upstream.Close()
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": false, "url": upstream.URL, "model": "observer", "warmup_seconds": 30}, 200)
			start := func() Job {
				w := admin.do("POST", "/api/v1/admin/defense/laya/test-async", nil, nil)
				var doc struct {
					Job Job `json:"job"`
				}
				if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &doc) != nil || doc.Job.Trace == "" {
					t.Fatalf("async: %d %s", w.Code, w.Body.String())
				}
				return doc.Job
			}
			job := start()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("inference did not start")
			}
			if duplicate := start(); duplicate.ID != job.ID || calls.Load() != 1 {
				t.Fatal("duplicate inference")
			}
			if outcome == "changed" {
				mustJSON(t, admin, "PUT", "/api/v1/admin/defense/laya", map[string]any{"enabled": false, "url": upstream.URL, "model": "changed"}, 200)
			}
			if outcome == "cancelled" {
				w := admin.do("POST", "/api/v1/admin/jobs/"+job.ID+"/cancel", nil, nil)
				if w.Code >= 400 {
					t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
				}
			}
			unblock()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result, ok := s.jobs.Wait(ctx, job.ID)
			if !ok || result.Status == jobRunning {
				t.Fatal("job did not finish")
			}
			view := mustJSON(t, admin, "GET", "/api/v1/admin/defense/laya", nil, 200)
			if (view["verified"] == true) != (outcome == "success") || view["enabled"] != false {
				t.Fatalf("incorrect verification: %v", view)
			}
			if outcome == "success" && result.Status != jobDone {
				t.Fatalf("failed: %+v", result)
			}
		})
	}
}

func TestLayaQueueBudgetExpiresBeforeInference(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	caps := contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskLayaClassify: contracts.TaskVersion}}
	if err := s.Store.Tasks.SetCapabilities(t.Context(), int(g.id), caps); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	payload := contracts.LayaClassifyTask{Model: laya.DefaultModel, State: json.RawMessage(`"synthetic event"`), ExpectedRuntimeVersion: "0.18.0", Questions: map[string]contracts.DecisionQuestion{"category": {Type: "choice", Criteria: json.RawMessage(`{"normal":"normal","suspicious":"suspicious"}`)}}}
	_, _, err := s.dispatchWorkerTask(ctx, contracts.TaskLayaClassify, payload, contracts.TaskRequirements{TargetGateID: int(g.id)}, []int{int(g.id)})
	var failure *contracts.TaskError
	if !errors.As(err, &failure) || failure.Code != "queue_timeout" {
		t.Fatalf("expected queue timeout, got %v", err)
	}
}

func TestLayaManualBudgetAllowsSlowInference(t *testing.T) {
	s, _ := newInstalledServer(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0.18.0"}`))
		case "/api/show":
			json.NewEncoder(w).Encode(map[string]string{"digest": laya.ModelManifestSHA256})
		default:
			calls.Add(1)
			select {
			case <-time.After(9 * time.Second):
			case <-r.Context().Done():
				return
			}
			w.Write([]byte(`{"answers":{"category":{"choice":"normal","confidence":0.99}}}`))
		}
	}))
	defer srv.Close()
	ctx := context.WithValue(t.Context(), layaTestRevisionKey{}, "manual")
	verdict, err := s.classify(ctx, layaSettings{URL: srv.URL, Model: "observer", WarmupSeconds: 30}, "Synthetic test")
	if err != nil || verdict.Category != "normal" || calls.Load() != 1 {
		t.Fatalf("slow inference: %+v %v calls=%d", verdict, err, calls.Load())
	}
}
