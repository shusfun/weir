package center

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func TestPendingNodeProbeRestoresDeduplicatesAndInvalidates(t *testing.T) {
	s, admin, gate := setupGate(t)
	ctx := t.Context()
	created := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gate.id, "preset": "ss2022", "skip_probe": true}, 200)
	n := s.Store.Ent.Node.GetX(ctx, int(created["id"].(float64)))
	g := s.Store.Ent.Gate.GetX(ctx, n.GateID)
	s.Store.Ent.Gate.UpdateOne(g).SetCenterURL("http://127.0.0.1:19810").SetAppliedRevision(g.ConfigRevision).ExecX(ctx)
	var calls atomic.Int32
	entered, release := make(chan struct{}, 2), make(chan struct{})
	gate.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionNodeTest: func(contracts.Action) contracts.ActionResult {
			calls.Add(1)
			entered <- struct{}{}
			<-release
			return contracts.ActionResult{OK: true, Result: map[string]any{"stage": "connect", "latency_ms": 3}}
		},
	})
	if err := s.queueNodeProbe(ctx, n, 1); err != nil {
		t.Fatal(err)
	}
	key := pendingNodeProbePrefix + itoa(n.ID)
	if _, ok, err := s.Store.KV.Get(ctx, key); err != nil || !ok {
		t.Fatal("补测没有持久化", err)
	}
	s.resumePendingNodeProbes(ctx)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("持久队列未恢复")
	}
	s.resumePendingNodeProbes(ctx)
	s.resumePendingNodeProbes(ctx)
	close(release)
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	jobs := s.jobs.List("action")
	for _, job := range jobs {
		if len(job.ID) >= len("pending-node-test:") && job.ID[:len("pending-node-test:")] == "pending-node-test:" {
			finished, ok := s.jobs.Wait(waitCtx, job.ID)
			if !ok || finished.Status != jobDone {
				t.Fatalf("补测未完成：%+v", finished)
			}
		}
	}
	if calls.Load() != 1 {
		t.Fatal("重复执行补测", calls.Load())
	}
	if _, ok, _ := s.Store.KV.Get(ctx, key); ok {
		t.Fatal("完成后未移除队列记录")
	}
	if r := s.loadNodeTest(ctx, n); r == nil || r.TCP.Status != "passed" {
		t.Fatal("补测结果未保存")
	}
	if err := s.queueNodeProbe(ctx, n, 1); err != nil {
		t.Fatal(err)
	}
	s.Store.Ent.Node.UpdateOne(n).SetPort(n.Port + 100).ExecX(ctx)
	s.resumePendingNodeProbes(ctx)
	if _, ok, _ := s.Store.KV.Get(ctx, key); ok || calls.Load() != 1 {
		t.Fatal("配置变化后旧队列未失效")
	}
}
