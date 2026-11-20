package center

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/gate"
)

type revisionCauseKey struct{}

type revisionCause struct {
	Source string
	Actor  int
}

func retryRevocationTransaction(ctx context.Context, run func() error) (err error) {
	for range 4 {
		err = run()
		var state interface{ SQLState() string }
		if !errors.As(err, &state) || state.SQLState() != "40001" || ctx.Err() != nil {
			return err
		}
	}
	return err
}

func advanceRevocations(ctx context.Context, tx *ent.Tx, ids []int) (err error) {
	ids = dedupe(ids)
	slices.Sort(ids)
	finish := operationStage(ctx, "gate.revocation.record", "gates", ids)
	defer func() { finish(err) }()
	cause := causeFrom(ctx)
	for _, id := range ids {
		device, err := tx.Gate.UpdateOneID(id).AddConfigRevision(1).Save(ctx)
		if err != nil {
			return err
		}
		detail := map[string]any{"old_revision": device.ConfigRevision - 1, "new_revision": device.ConfigRevision, "source": cause.Source, "trace_id": taskTrace(ctx)}
		if err := tx.Audit.Create().SetActor(cause.Actor).SetAction("config.revision").SetTarget("gate:" + itoa(id)).SetResult("ok").SetDetail(detail).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) notifyRevocations(ctx context.Context, ids []int) {
	for _, id := range dedupe(ids) {
		s.waiters.notify(id)
		_ = s.Store.Notifications.Publish(ctx, "gate:"+itoa(id))
	}
}

func (s *Server) configRevisionTrace(ctx context.Context, device *ent.Gate) string {
	latest, err := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("config.revision"), audit.TargetEQ("gate:"+itoa(device.ID))).Order(ent.Desc(audit.FieldID)).First(ctx)
	if err == nil {
		revision, registered := latest.Detail["new_revision"].(float64)
		trace, _ := latest.Detail["trace_id"].(string)
		if registered && int64(revision) == device.ConfigRevision && trace != "" {
			return trace
		}
	}
	return taskTrace(ctx)
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

// gateWaiters 在配置版本变化时唤醒当前任务通道的配置投递。
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
