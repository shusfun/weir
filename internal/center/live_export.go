package center

import (
	"context"
	"encoding/json"
	"time"
)

// LiveSnapshot 让这台 Gate 开始实时上报，等到至少一帧后返回管理员视图下这一帧换算出的速率（JSON）。供端到端测试用真实内核验证计数链路。
func (s *Server) LiveSnapshot(ctx context.Context, gid int, wait time.Duration) ([]byte, error) {
	s.ensureLive(ctx, []int{gid})
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if fs := s.live.frames(gid, time.Time{}); len(fs) > 0 {
			m, err := s.buildLinkMap(ctx, 0)
			if err != nil {
				return nil, err
			}
			return json.Marshal(m.eventFor(gid, fs[len(fs)-1]))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, context.DeadlineExceeded
}
