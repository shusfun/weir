package center

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/internal/store/ent"
)

type pendingNodeProbe struct {
	NodeID          int       `json:"node_id"`
	Actor           int       `json:"actor"`
	Binding         string    `json:"binding"`
	Trace           httpTrace `json:"trace"`
	Deployed        bool      `json:"deployed,omitempty"`
	ExecutionGateID int       `json:"execution_gate_id,omitempty"`
}

const pendingNodeProbePrefix = "pending-node-probe:"

func (s *Server) queueNodeProbe(ctx context.Context, n *ent.Node, actor int) error {
	g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
	if err != nil {
		return err
	}
	binding, err := s.probeFingerprint(ctx, n, g)
	if err != nil {
		return err
	}
	trace, _ := ctx.Value(httpTraceKey{}).(httpTrace)
	if trace.ID == "" {
		trace = newHTTPTrace("")
	}
	b, _ := json.Marshal(pendingNodeProbe{NodeID: n.ID, Actor: actor, Binding: binding, Trace: trace})
	s.nodeProbes.Lock()
	err = s.Store.KV.Set(ctx, pendingNodeProbePrefix+itoa(n.ID), string(b), 0)
	s.nodeProbes.Unlock()
	if err != nil {
		return err
	}
	return s.storeNodeTest(ctx, n.ID, nodeTestResult{Status: "pending", Why: "待实测：等待配置应用、VPS 在线及测试内核就绪", TraceID: trace.ID, At: time.Now().UTC(), Location: "gate", External: "unknown", TCP: nodeprobe.Check{Status: "unknown"}, UDP: nodeprobe.Check{Status: "unknown"}, Binding: binding})
}

func (s *Server) clearPendingNodeProbe(ctx context.Context, key, expected string) error {
	s.nodeProbes.Lock()
	defer s.nodeProbes.Unlock()
	raw, exists, err := s.Store.KV.Get(ctx, key)
	if err != nil || !exists || raw != expected {
		return err
	}
	return s.Store.KV.Del(ctx, key)
}

// 队列独立于内存任务，重启后由维护任务与配置应用事件恢复。
func (s *Server) resumePendingNodeProbes(ctx context.Context) {
	s.state.RLock()
	defer s.state.RUnlock()
	if s.Store == nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-s.events.stopped():
			cancel()
		case <-ctx.Done():
		}
	}()
	keys, err := s.Store.KV.Keys(ctx, pendingNodeProbePrefix, 10000)
	if err != nil {
		slog.Error("pending node probes", "err", redact(err.Error()))
		return
	}
	for _, key := range keys {
		raw, ok, err := s.Store.KV.Get(ctx, key)
		if err != nil || !ok {
			continue
		}
		var pending pendingNodeProbe
		if json.Unmarshal([]byte(raw), &pending) != nil {
			continue
		}
		n, err := s.Store.Ent.Node.Get(ctx, pending.NodeID)
		if ent.IsNotFound(err) {
			_ = s.clearPendingNodeProbe(ctx, key, raw)
			continue
		}
		if err != nil {
			continue
		}
		g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
		if err != nil || g.Status != "active" || g.AppliedRevision < g.ConfigRevision || g.LastSeenAt == nil || time.Since(*g.LastSeenAt) > 90*time.Second {
			continue
		}
		binding, err := s.probeFingerprint(ctx, n, g)
		if err != nil {
			continue
		}
		if binding != pending.Binding {
			_ = s.clearPendingNodeProbe(ctx, key, raw)
			continue
		}
		if !pending.Deployed {
			if _, _, err = s.gateKernels(ctx, g, map[string]bool{"mihomo": true}); err != nil {
				continue
			}
		}
		s.nodeProbes.Lock()
		if j, ok := s.jobs.Get(s.nodeProbes.jobs[n.ID]); ok && j.Status == jobRunning {
			s.nodeProbes.Unlock()
			continue
		}
		if s.nodeProbes.jobs == nil {
			s.nodeProbes.jobs = map[int]string{}
		}
		jobID := "pending-node-test:" + itoa(n.ID) + ":" + binding[:16]
		s.nodeProbes.jobs[n.ID] = jobID
		p := pending
		s.jobs.StartWith(jobSpec{ID: jobID, Kind: "action", Title: "自动补测节点「" + n.Name + "」", Owner: p.Actor, Trace: p.Trace.ID}, func(bg context.Context, job JobRun) (map[string]any, error) {
			s.state.RLock()
			defer s.state.RUnlock()
			if s.Store == nil {
				return nil, context.Canceled
			}
			bg, stop := context.WithCancel(bg)
			defer stop()
			go func() {
				select {
				case <-s.events.stopped():
					stop()
				case <-bg.Done():
				}
			}()
			bg = context.WithValue(bg, httpTraceKey{}, p.Trace)
			bg = context.WithValue(bg, jobCtxKey{}, &job)
			var output map[string]any
			err := s.Store.Lock.Do(bg, key, func(bg context.Context) error {
				currentRaw, exists, err := s.Store.KV.Get(bg, key)
				if err != nil {
					return err
				}
				if !exists || currentRaw != raw {
					return nil
				}
				current, err := s.Store.Ent.Node.Get(bg, p.NodeID)
				if err != nil {
					return err
				}
				gate, err := s.Store.Ent.Gate.Get(bg, current.GateID)
				if err != nil {
					return err
				}
				fingerprint, err := s.probeFingerprint(bg, current, gate)
				if err != nil {
					return err
				}
				if fingerprint != p.Binding {
					return errors.New("业务配置已变化，旧补测任务已失效")
				}
				res := map[string]any{}
				var testErr error
				if !p.Deployed {
					res, testErr = s.testNode(bg, current, current.Settings)
				}
				if missingMihomo(testErr) {
					return testErr
				}
				location := "gate"
				if colocated(gate) {
					location = "colocated"
				}
				r := nodeTestResult{Status: "passed", At: time.Now().UTC(), JobID: job.e.job.ID, TraceID: p.Trace.ID, Location: location, ExecutionGateID: gate.ID, External: "unknown", Binding: p.Binding, TCP: nodeprobe.Check{Status: "passed"}, UDP: nodeprobe.Check{Status: "unknown", Why: "本机候选配置实测仅验证下载"}}
				if ms, ok := res["latency_ms"].(float64); ok {
					r.TCP.LatencyMS = int64(ms)
				}
				if testErr != nil {
					r.Status, r.Why = "failed", safeCheckOutput(testErr.Error())
					r.TCP = nodeprobe.Check{Status: "failed", Why: r.Why}
				}
				if p.Deployed || testErr == nil && externalProbeSupported() && !colocated(gate) {
					bg = context.WithValue(bg, nodeProbeExecutionKey{}, p.ExecutionGateID)
					r, testErr = s.runDeployedNodeTest(bg, current.ID, p.Actor, job)
					r.At = time.Now().UTC()
					if testErr != nil {
						r.Why = safeCheckOutput(testErr.Error())
					}
				}
				r.Binding = p.Binding
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(bg), 10*time.Second)
				defer cancel()
				if err = s.storeNodeTest(cleanup, p.NodeID, r); err != nil {
					return err
				}
				if err = s.clearPendingNodeProbe(cleanup, key, raw); err != nil {
					return err
				}
				_, err = s.Store.Ent.Audit.Create().SetActor(p.Actor).SetAction("node.test").SetTarget("node:" + itoa(p.NodeID)).SetResult(r.Status).SetDetail(map[string]any{"trace_id": p.Trace.ID, "job_id": r.JobID, "automatic": true}).Save(cleanup)
				if err != nil {
					return err
				}
				s.notifyConsole("gate", itoa(current.GateID))
				output = map[string]any{"test": r}
				return testErr
			})
			return output, err
		})
		s.nodeProbes.Unlock()
	}
}
