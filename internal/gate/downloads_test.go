package gate

import (
	"context"
	"testing"
	"time"
)

func TestDownloadCoordinatorCancellationDoesNotLeakTheSlot(t *testing.T) {
	var d downloadCoordinator
	release, err := d.acquire(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := d.acquire(ctx, true); err == nil {
		t.Fatal("parallel download bypassed slot")
	}
	release()
	release()
	unlock, err := d.acquire(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}
