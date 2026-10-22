package center

import (
	"context"
	"strconv"
	"sync"

	"github.com/imzyb/MiGate/internal/store/ent/gate"
)

func itoa(v int) string { return strconv.Itoa(v) }

// gateWaiters 让等待中的 /agent/config 长轮询在配置版本变化时立即返回。
// 中心是单进程，进程内通知即可；进程重启后 Gate 会重新长轮询。
type gateWaiters struct {
	mu sync.Mutex
	ch map[int]chan struct{}
}

func (w *gateWaiters) wait(gid int) <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ch == nil {
		w.ch = map[int]chan struct{}{}
	}
	c, ok := w.ch[gid]
	if !ok {
		c = make(chan struct{})
		w.ch[gid] = c
	}
	return c
}

func (w *gateWaiters) notify(gid int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c, ok := w.ch[gid]; ok {
		close(c)
		delete(w.ch, gid)
	}
}

// bumpGate 让该 Gate 的期望配置版本加一并唤醒等待中的长轮询，返回新版本号。
func (s *Server) bumpGate(ctx context.Context, gid int) (int64, error) {
	if err := s.Store.Ent.Gate.UpdateOneID(gid).AddConfigRevision(1).Exec(ctx); err != nil {
		return 0, err
	}
	g, err := s.Store.Ent.Gate.Query().Where(gate.IDEQ(gid)).Only(ctx)
	if err != nil {
		return 0, err
	}
	s.waiters.notify(gid)
	return g.ConfigRevision, nil
}

// bumpGates 对一组 Gate 去重后逐个加版本。
func (s *Server) bumpGates(ctx context.Context, gids []int) error {
	seen := map[int]bool{}
	for _, gid := range gids {
		if seen[gid] {
			continue
		}
		seen[gid] = true
		if _, err := s.bumpGate(ctx, gid); err != nil {
			return err
		}
	}
	return nil
}
