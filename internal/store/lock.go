package store

import (
	"context"
	"database/sql"
	"hash/fnv"
	"sync"
)

// Locker 让「读出 → 修改 → 写回」这类操作在同一名字下串行执行。
// PostgreSQL 用会话级咨询锁（多实例安全）；SQLite 单进程，用进程内互斥。
type Locker interface {
	Do(ctx context.Context, name string, fn func(ctx context.Context) error) error
}

type pgLocker struct{ db *sql.DB }

func lockKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("migate:" + name))
	return int64(h.Sum64())
}

func (l pgLocker) Do(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	key := lockKey(name)
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, key); err != nil {
		return err
	}
	// 解锁使用独立上下文，调用方取消时也必须释放。
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, key)
	return fn(ctx)
}

type localLocker struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newLocalLocker() *localLocker { return &localLocker{locks: map[string]*sync.Mutex{}} }

func (l *localLocker) Do(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	l.mu.Lock()
	m, ok := l.locks[name]
	if !ok {
		m = &sync.Mutex{}
		l.locks[name] = m
	}
	l.mu.Unlock()
	m.Lock()
	defer m.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(ctx)
}
