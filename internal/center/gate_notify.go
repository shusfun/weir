package center

import (
	"context"
	"strconv"
	"sync"

	"github.com/shusfun/weir/internal/store/ent/gate"
)

type revisionCauseKey struct{}

type revisionCause struct {
	Source string
	Actor  int
}

// WithRevisionCause 让随后的配置版本推进记下触发来源和操作者。
func WithRevisionCause(ctx context.Context, source string, actor int) context.Context {
	if source == "" {
		source = "system"
	}
	return context.WithValue(ctx, revisionCauseKey{}, revisionCause{Source: source, Actor: actor})
}

func causeFrom(ctx context.Context) revisionCause {
	c, _ := ctx.Value(revisionCauseKey{}).(revisionCause)
	if c.Source == "" {
		c.Source = "system"
	}
	return c
}

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
	prev, err := s.Store.Ent.Gate.Get(ctx, gid)
	if err != nil {
		return 0, err
	}
	old := prev.ConfigRevision
	if err = s.Store.Ent.Gate.UpdateOneID(gid).AddConfigRevision(1).Exec(ctx); err != nil {
		return 0, err
	}
	g, err := s.Store.Ent.Gate.Query().Where(gate.IDEQ(gid)).Only(ctx)
	if err != nil {
		return 0, err
	}
	s.recordRevision(ctx, gid, old, g.ConfigRevision)
	s.waiters.notify(gid)
	_ = s.Store.Notifications.Publish(ctx, "gate:"+itoa(gid))
	return g.ConfigRevision, nil
}

func (s *Server) recordRevision(ctx context.Context, gid int, old, next int64) {
	if s.Store == nil {
		return
	}
	cause := causeFrom(ctx)
	detail := map[string]any{"old_revision": old, "new_revision": next, "source": cause.Source, "trace_id": taskTrace(ctx)}
	_, _ = s.Store.Ent.Audit.Create().SetActor(cause.Actor).SetAction("config.revision").SetTarget("gate:" + itoa(gid)).SetResult("ok").SetDetail(detail).Save(ctx)
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
