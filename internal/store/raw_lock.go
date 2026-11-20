package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"hash/fnv"
	"time"
)

// pgLocker 用 PostgreSQL 会话级咨询锁让多个中心实例互斥。咨询锁是会话函数
// （pg_try_advisory_lock / pg_advisory_unlock），Ent 没有对应的查询构造，必须在独占连接上直接调用。
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
