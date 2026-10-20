package control

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"
)

const mailWorkerLock = 69442903

// Return a pooled session only after confirming its advisory lock is gone.
// An interrupted unlock must discard the physical connection, not return a
// session that can silently retain the worker lock indefinitely.
func releaseMailWorker(conn *sql.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var unlocked bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_advisory_unlock($1)`, mailWorkerLock).Scan(&unlocked); err != nil || !unlocked {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	_ = conn.Close()
}

func acquireMailWorker(ctx context.Context, db *sql.DB) (*sql.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var acquired bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, mailWorkerLock).Scan(&acquired); err != nil {
		// The result can be lost after the server acquired the lock.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
		return nil, err
	}
	if !acquired {
		_ = conn.Close()
		return nil, errors.New("mail_worker_busy")
	}
	return conn, nil
}
