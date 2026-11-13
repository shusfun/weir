package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"hash/fnv"
	"sync"
	"time"
)

// Locker 让「读出 → 修改 → 写回」这类操作在同一名字下串行执行。
// PostgreSQL 用会话级咨询锁（多实例安全）；SQLite 单进程，用进程内互斥。
type Locker interface {
	Do(ctx context.Context, name string, fn func(ctx context.Context) error) error
}

type pgLocker struct{ db *sql.DB }

func lockKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("weir:" + name))
	return int64(h.Sum64())
}

func (l pgLocker) Do(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	started := time.Now()
	key := lockKey(name)
	var conn *sql.Conn
	for {
		var err error
		conn, err = l.db.Conn(ctx)
		if err != nil {
			return err
		}
		var acquired bool
		err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&acquired)
		if err != nil {
			discardLockConn(conn)
			return err
		}
		if acquired {
			break
		}
		conn.Close()
		// Waiters must release pool slots: the owner needs a second connection
		// for business queries and transactions while holding its session lock.
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	recordLock(ctx, started)
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		if err := conn.QueryRowContext(release, `SELECT pg_advisory_unlock($1)`, key).Scan(&unlocked); err != nil || !unlocked {
			discardLockConn(conn)
			return
		}
		conn.Close()
	}()
	return fn(ctx)
}

func discardLockConn(conn *sql.Conn) {
	// A canceled query may have acquired the lock before losing its reply.
	// Discard that physical session rather than returning a possible lock to
	// the pool, where a different request could accidentally re-enter it.
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
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
