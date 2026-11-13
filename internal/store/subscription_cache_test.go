package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscriptionCacheAdmissionBoundsConcurrentFills(t *testing.T) {
	for name, open := range backends(t) {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			ctx := t.Context()
			budget := SubscriptionCacheBudget{Bytes: 20, Entries: 2}
			var accepted atomic.Int32
			var workers sync.WaitGroup
			for i := 0; i < 30; i++ {
				workers.Go(func() {
					err := s.WriteSubscriptionOutputCache(ctx, fmt.Sprintf("admission-test:%d", i), "0123456789", time.Minute, budget)
					if err == nil {
						accepted.Add(1)
					} else if !errors.Is(err, ErrSubscriptionCacheCapacity) {
						t.Error(err)
					}
				})
			}
			workers.Wait()
			if n := accepted.Load(); n != 2 {
				t.Fatalf("accepted=%d; want exactly two", n)
			}
			if err := s.KV.Set(ctx, "session:admission-test", "preserve", time.Minute); err != nil {
				t.Fatal(err)
			}
			if err := s.WriteSubscriptionCacheBounded(ctx, "admission-test:overflow", "new", time.Minute, budget); !errors.Is(err, ErrSubscriptionCacheCapacity) {
				t.Fatalf("raw cache escaped shared budget: %v", err)
			}
			v, ok, err := s.KV.Get(ctx, "session:admission-test")
			if err != nil || !ok || v != "preserve" {
				t.Fatal("cache admission changed authentication state")
			}
		})
	}
}

type unavailableCache struct{ KV }

func (k unavailableCache) Set(context.Context, string, string, time.Duration) error {
	return errors.New("cache unavailable")
}

func TestSubscriptionCacheDurableFencing(t *testing.T) {
	for name, open := range backends(t) {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			ctx := t.Context()
			version := func() string {
				t.Helper()
				v, err := s.SubscriptionCacheVersion(ctx)
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			initial := version()
			tx, err := s.Ent.Tx(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.SubNode.Create().SetURL("trojan://example.invalid:443").Save(ctx); err != nil {
				t.Fatal(err)
			}
			if err = tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if version() != initial {
				t.Fatal("rolled back mutation changed version")
			}
			source := s.Ent.SubSource.Create().SetURL("https://example.invalid/sub").SaveX(ctx)
			created := version()
			if created == initial {
				t.Fatal("committed dependency did not invalidate")
			}
			source.Update().SetFetchedAt(time.Now()).SetNodeCount(3).ExecX(ctx)
			if version() != created {
				t.Fatal("telemetry invalidated configuration cache")
			}
			original := s.KV
			s.KV = unavailableCache{original}
			if err = s.WriteSubscriptionCache(ctx, "cache-fencing-test", "new", time.Minute); err == nil {
				t.Fatal("write failure was hidden")
			}
			// The next read repairs a pending publication even while Redis is down.
			if version() == created {
				t.Fatal("interrupted write was not fenced")
			}
			s.KV = original
			t.Cleanup(func() { _ = original.Del(context.Background(), "cache-fencing-test") })
			if err = s.WriteSubscriptionCache(ctx, "cache-fencing-test", "new", time.Minute); err != nil {
				t.Fatal(err)
			}
			written := version()
			if err = s.WriteSubscriptionCache(ctx, "cache-fencing-test", "new", time.Minute); err != nil {
				t.Fatal(err)
			}
			if version() != written {
				t.Fatal("identical content invalidated rendered cache")
			}
			if err = s.InvalidateSubscriptionCache(ctx); err != nil {
				t.Fatal(err)
			}
			if version() == written {
				t.Fatal("explicit invalidation depended on Redis")
			}
			if err = s.migrateSubscriptionCache(ctx); err != nil {
				t.Fatal(err)
			}
			before := version()
			if err = s.migrateSubscriptionCache(ctx); err != nil {
				t.Fatal(err)
			}
			if version() != before {
				t.Fatal("repeated migration reset durable version")
			}
		})
	}
}
