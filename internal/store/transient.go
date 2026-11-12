package store

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Transient holds replaceable observations, never identity, accounting or
// business state. SQLite keeps it in memory; PostgreSQL centers share Redis.
type Transient interface {
	Get(context.Context, string) (string, bool, error)
	Set(context.Context, string, string, time.Duration) error
	Del(context.Context, ...string) error
}

type transientValue struct {
	value string
	until time.Time
}
type memoryTransient struct {
	mu     sync.Mutex
	values map[string]transientValue
}

func (m *memoryTransient) Get(ctx context.Context, key string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[key]
	if ok && !time.Now().Before(v.until) {
		delete(m.values, key)
		ok = false
	}
	if !ok {
		return "", false, nil
	}
	return v.value, true, nil
}
func (m *memoryTransient) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(key) == 0 || len(key) > 200 || len(value) > 256<<10 || ttl <= 0 || ttl > 24*time.Hour {
		return errors.New("invalid transient observation")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = map[string]transientValue{}
	}
	now := time.Now()
	if len(m.values) >= 10000 {
		for k, v := range m.values {
			if !now.Before(v.until) {
				delete(m.values, k)
			}
		}
	}
	if _, exists := m.values[key]; !exists && len(m.values) >= 10000 {
		return errors.New("transient observation capacity reached")
	}
	m.values[key] = transientValue{value, now.Add(ttl)}
	return nil
}
func (m *memoryTransient) Del(ctx context.Context, keys ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range keys {
		delete(m.values, key)
	}
	return nil
}
