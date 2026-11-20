package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/subscriptioncacheentry"
)

const subscriptionCacheLock = "subscription-cache-state"

// WriteSubscriptionCache bridges the database and disposable KV without a
// distributed transaction. An interrupted write leaves a durable dirty marker;
// the next version read fences it before allowing a rendered-cache lookup.
func (s *Store) WriteSubscriptionCache(ctx context.Context, key, value string, ttl time.Duration) error {
	return s.WriteSubscriptionCacheBounded(ctx, key, value, ttl, SubscriptionCacheBudget{Bytes: 256 << 20, Entries: 2048})
}

type SubscriptionCacheBudget struct{ Bytes, Entries int }

var ErrSubscriptionCacheCapacity = errors.New("subscription cache budget exhausted")

// Admission metadata bounds this product's disposable cache payloads without
// changing Redis's global policy or evicting sessions and one-time credentials.
// It reserves before the KV boundary and retains uncertain reservations on error.
func (s *Store) reserveSubscriptionCache(ctx context.Context, key, value string, ttl time.Duration, budget SubscriptionCacheBudget) error {
	if budget.Bytes < 1 || budget.Entries < 1 || ttl <= 0 || len(value) > budget.Bytes {
		return ErrSubscriptionCacheCapacity
	}
	var usage []struct {
		Count int64            `json:"count"`
		Bytes entsql.NullInt64 `json:"bytes"`
	}
	err := s.Ent.SubscriptionCacheEntry.Query().
		Where(subscriptioncacheentry.ExpiresAtGT(time.Now().UnixMilli()), subscriptioncacheentry.IDNEQ(key)).
		Aggregate(ent.As(ent.Count(), "count"), ent.As(ent.Sum(subscriptioncacheentry.FieldBytes), "bytes")).
		Scan(ctx, &usage)
	if err != nil {
		return err
	}
	if len(usage) != 1 {
		return fmt.Errorf("subscription cache usage: %d rows", len(usage))
	}
	if usage[0].Count >= int64(budget.Entries) || usage[0].Bytes.Int64+int64(len(value)) > int64(budget.Bytes) {
		return ErrSubscriptionCacheCapacity
	}
	return s.Ent.SubscriptionCacheEntry.Create().SetID(key).SetBytes(int64(len(value))).SetExpiresAt(time.Now().Add(ttl).UnixMilli()).
		OnConflictColumns(subscriptioncacheentry.FieldID).UpdateNewValues().Exec(ctx)
}

func (s *Store) WriteSubscriptionCacheBounded(ctx context.Context, key, value string, ttl time.Duration, budget SubscriptionCacheBudget) error {
	return s.Lock.Do(ctx, subscriptionCacheLock, func(ctx context.Context) error {
		if err := s.reserveSubscriptionCache(ctx, key, value, ttl, budget); err != nil {
			return err
		}
		old, exists, err := s.KV.Get(ctx, key)
		if err != nil {
			return err
		}
		if exists && old == value {
			return s.KV.Set(ctx, key, value, ttl)
		}
		if err = s.markSubscriptionCacheDirty(ctx); err != nil {
			return err
		}
		if err = s.KV.Set(ctx, key, value, ttl); err != nil {
			return err
		}
		return s.publishSubscriptionCache(ctx)
	})
}

func (s *Store) WriteSubscriptionOutputCache(ctx context.Context, key, value string, ttl time.Duration, budget SubscriptionCacheBudget) error {
	return s.Lock.Do(ctx, subscriptionCacheLock, func(ctx context.Context) error {
		if err := s.reserveSubscriptionCache(ctx, key, value, ttl, budget); err != nil {
			return err
		}
		return s.KV.Set(ctx, key, value, ttl)
	})
}

func (s *Store) PruneSubscriptionCacheReservations(ctx context.Context, batch int) error {
	if batch < 1 || batch > 10000 {
		return errors.New("invalid cache maintenance batch")
	}
	now := time.Now().UnixMilli()
	_, err := s.Ent.SubscriptionCacheEntry.Delete().Where(func(sel *entsql.Selector) {
		d := entsql.Dialect(sel.Dialect())
		r := d.Table(subscriptioncacheentry.Table).As("r")
		expired := d.Select(r.C(subscriptioncacheentry.FieldID)).From(r).
			Where(entsql.LTE(r.C(subscriptioncacheentry.FieldExpiresAt), now)).
			OrderBy(r.C(subscriptioncacheentry.FieldExpiresAt)).Limit(batch)
		sel.Where(entsql.In(sel.C(subscriptioncacheentry.FieldID), expired))
	}).Exec(ctx)
	return err
}
