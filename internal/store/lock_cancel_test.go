package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSQLiteLockWaitCanBeCancelled(t *testing.T) {
	locker := newLocalLocker()
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- locker.Do(t.Context(), "distribution", func(context.Context) error { close(held); <-release; return nil })
	}()
	<-held
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := locker.Do(ctx, "distribution", func(context.Context) error { t.Error("cancelled waiter acquired lock"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("waiter did not cancel", err)
	}
	if err := locker.Do(t.Context(), "independent", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
}
