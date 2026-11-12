package dsh

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJobRequestRemovalCancelsRunningTask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	done := make(chan JobResult, 1)
	go func() {
		done <- runRequestedJob(t.Context(), path, func(ctx context.Context) JobResult {
			close(entered)
			<-ctx.Done()
			return JobResult{Result: Result{State: "cancelled"}}
		})
	}()
	<-entered
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.State != "cancelled" {
			t.Fatalf("result: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("removed request did not cancel execution")
	}
	called := false
	result := runRequestedJob(t.Context(), path, func(ctx context.Context) JobResult { called = true; return JobResult{} })
	if called || result.State != "cancelled" {
		t.Fatal("absent request executed")
	}
}

func TestBackgroundProfileMustBeDeclaredByHost(t *testing.T) {
	a := &agent{}
	d := Desired{Version: "0.2.0", Namespace: "consumer", AnalystPatch: "default", BackgroundProfiles: map[string]string{"review": "host profile"}}
	if result := a.runJob(t.Context(), d, JobRequest{Task: "inspect", Profile: "../web"}); result.State != "failed" {
		t.Fatal("profile path injection accepted")
	}
	if result := a.runJob(t.Context(), d, JobRequest{Task: "inspect", Profile: "missing"}); result.Error != "dsh 未启用" {
		t.Fatal("unregistered profile used the default profile", result)
	}
}
