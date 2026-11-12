package dsh

import (
	"context"
	"errors"
	"testing"
)

func TestJobChecksCancellationAndCallerCompatibilityBeforeExecution(t *testing.T) {
	job := Job{Namespace: "example", Patch: []byte("restricted"), Compatibility: Compatibility{InstalledVersion: "0.3.0", VerifiedRange: ">=0.2.0 <0.3.0", OutOfRange: DegradeBaseOnly}}
	result, err := job.Run(t.Context(), "read status")
	if err == nil || result.State != "unknown" {
		t.Fatalf("unverified task ran: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = job.Run(ctx, "read status"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
