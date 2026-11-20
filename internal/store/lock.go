package store

import (
	"context"
	"sync"
	"time"
)

// Locker 让「读出 → 修改 → 写回」这类操作在同一名字下串行执行。
// PostgreSQL 用会话级咨询锁（多实例安全）；SQLite 单进程，用进程内互斥。
type Locker interface {
	Do(ctx context.Context, name string, fn func(ctx context.Context) error) error
}

type localLocker struct {
	mu    sync.Mutex
	locks map[string]*localLock
}

type localLock struct {
	slot  chan struct{}
	users int
}

func newLocalLocker() *localLocker { return &localLocker{locks: map[string]*localLock{}} }

func (l *localLocker) Do(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	l.mu.Lock()
	m, ok := l.locks[name]
	if !ok {
		m = &localLock{slot: make(chan struct{}, 1)}
		l.locks[name] = m
	}
	m.users++
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		m.users--
		if m.users == 0 {
			delete(l.locks, name)
		}
		l.mu.Unlock()
	}()
	started := time.Now()
	select {
	case m.slot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	recordLock(ctx, started)
	defer func() { <-m.slot }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(ctx)
}
