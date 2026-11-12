package center

import (
	"context"
	"encoding/json"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

const kernelEventLimit = 80

func validKernelEvents(events []contracts.KernelEvent) bool {
	if len(events) > 40 {
		return false
	}
	for _, e := range events {
		if e.At.IsZero() || time.Since(e.At) > 48*time.Hour || e.At.After(time.Now().Add(time.Minute)) {
			return false
		}
		if e.Kernel != "xray" && e.Kernel != "sing-box" {
			return false
		}
		switch e.Kind {
		case "start", "stop", "crash", "recover", "hot_users":
		default:
			return false
		}
		if len(e.Reason) > 200 || len(e.TraceID) > 64 {
			return false
		}
		if e.PID != nil && *e.PID <= 0 {
			return false
		}
		if e.Disconnects != nil && *e.Disconnects < 0 {
			return false
		}
	}
	return true
}

func (s *Server) appendKernelEvents(ctx context.Context, gateID int, revision int64, trace string, events []contracts.KernelEvent) error {
	if len(events) == 0 {
		return nil
	}
	key := "gate-kernel-events:" + itoa(gateID)
	var current []map[string]any
	if raw, ok, err := s.Store.KV.Get(ctx, key); err != nil {
		return err
	} else if ok && raw != "" {
		_ = json.Unmarshal([]byte(raw), &current)
	}
	seen := map[string]bool{}
	for _, item := range current {
		seen[kernelEventKey(item)] = true
	}
	for _, e := range events {
		if e.Kind == "crash" {
			s.triggerOps(ctx, gateID, "kernel.crash", e.Kernel+" 异常退出："+e.Reason)
		}
		item := map[string]any{
			"at": e.At, "kernel": e.Kernel, "kind": e.Kind, "pid": e.PID, "disconnects": e.Disconnects,
			"reason": e.Reason, "trace_id": firstNonEmpty(e.TraceID, trace), "revision": revision,
		}
		if seen[kernelEventKey(item)] {
			continue
		}
		seen[kernelEventKey(item)] = true
		current = append(current, item)
	}
	if len(current) > kernelEventLimit {
		current = current[len(current)-kernelEventLimit:]
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return err
	}
	return s.Store.KV.Set(ctx, key, string(raw), 0)
}

func kernelEventKey(item map[string]any) string {
	b, _ := json.Marshal([]any{item["at"], item["kernel"], item["kind"], item["pid"], item["reason"], item["trace_id"]})
	return string(b)
}

func (s *Server) kernelEvents(ctx context.Context, gateID int) []map[string]any {
	raw, ok, err := s.Store.KV.Get(ctx, "gate-kernel-events:"+itoa(gateID))
	if err != nil || !ok || raw == "" {
		return nil
	}
	var out []map[string]any
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}
