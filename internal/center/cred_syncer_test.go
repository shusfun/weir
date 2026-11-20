package center

import (
	"context"
	"testing"
	"time"
)

// 没有后台协程时登记即同步执行；启动后多次登记合并，done 追上 requested，停止后协程退出。
func TestCredSyncerCoalesces(t *testing.T) {
	s, _, _ := setupGate(t)
	ctx := t.Context()
	seq, err := s.RequestCredSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st := s.credSyncStatus(); st.Done != seq || st.Requested != seq {
		t.Fatalf("inline sync status=%+v", st)
	}
	runCtx, stop := context.WithCancel(ctx)
	exited := make(chan struct{})
	go func() { s.runCredSync(runCtx); close(exited) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.creds.mu.Lock()
		started := s.creds.started
		s.creds.mu.Unlock()
		if started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("syncer did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var last uint64
	for range 5 {
		if last, err = s.RequestCredSync(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for {
		st := s.credSyncStatus()
		if st.Done == last {
			if st.Requested != last || st.LastError != "" {
				t.Fatalf("status=%+v", st)
			}
			break
		}
		if st.Done > last || time.Now().After(deadline) {
			t.Fatalf("sync did not catch up: %+v want %d", st, last)
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("syncer goroutine leaked after stop")
	}
}
