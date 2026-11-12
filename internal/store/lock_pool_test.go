package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPostgresLockWaitersDoNotExhaustPool(t *testing.T) {
	s := openPostgresForTest(t)
	s.DB.SetMaxOpenConns(2)
	s.DB.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	owner := make(chan struct{})
	proceed := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- s.Lock.Do(ctx, "pool-regression", func(ctx context.Context) error {
			close(owner)
			<-proceed
			var n int
			return s.DB.QueryRowContext(ctx, "SELECT 1").Scan(&n)
		})
	}()
	<-owner
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Lock.Do(ctx, "pool-regression", func(context.Context) error { return nil })
		}()
	}
	// Let the waiters contend while the owner still needs a business connection.
	time.Sleep(100 * time.Millisecond)
	close(proceed)
	err := <-result
	cancel()
	wg.Wait()
	if err != nil {
		t.Fatal("lock waiters blocked the owner's database work", err)
	}
}

func TestPostgresIndependentLockOwnersCanQuery(t *testing.T) {
	s := openPostgresForTest(t)
	s.DB.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	ready := make(chan struct{}, 2)
	proceed := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			results <- s.Lock.Do(ctx, fmt.Sprintf("independent-%d", i), func(ctx context.Context) error {
				ready <- struct{}{}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-proceed:
				}
				var n int
				return s.DB.QueryRowContext(ctx, "SELECT 1").Scan(&n)
			})
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(proceed)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal("independent lock owners exhausted business pool", err)
		}
	}
}
