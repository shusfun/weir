package control

import (
	"context"
	"testing"
)

func testMailWorkerLockRecovery(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()
	first, err := acquireMailWorker(ctx, s.DB)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	if err = first.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		releaseMailWorker(first)
		t.Fatal(err)
	}
	if second, e := acquireMailWorker(ctx, s.DB); e == nil {
		releaseMailWorker(second)
		releaseMailWorker(first)
		t.Fatal("concurrent mail worker acquired lock")
	}
	var terminated bool
	// Terminate only this test's own acquired connection in disposable PG.
	if err = s.DB.QueryRowContext(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&terminated); err != nil || !terminated {
		releaseMailWorker(first)
		t.Fatal("could not terminate owned mail-lock connection", err)
	}
	releaseMailWorker(first)
	second, err := acquireMailWorker(ctx, s.DB)
	if err != nil {
		t.Fatal("mail worker could not recover after connection loss", err)
	}
	var newPID int
	if err = second.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&newPID); err != nil || newPID == pid {
		releaseMailWorker(second)
		t.Fatal("terminated mail connection reused")
	}
	releaseMailWorker(second)
	third, err := acquireMailWorker(ctx, s.DB)
	if err != nil {
		t.Fatal("normal release retained worker lock", err)
	}
	releaseMailWorker(third)
}
