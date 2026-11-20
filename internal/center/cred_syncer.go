package center

import (
	"context"
	"log/slog"
	"sync"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/store/ent/gate"
)

// credSyncer 把节点、代理、链式代理变化后的凭据全量同步（SyncAll：内部凭据、每个用户的凭据、Gate 版本）
// 从请求里移到后台：请求只改库并登记一次同步，立即返回；后台单协程把短时间内的多次登记合并成一次 SyncAll。
// 这样并发操作不再在 cred-sync 锁上排队，页面也不必等全部用户同步完。
// 没有启动后台协程时（测试、命令行工具），Request 退回到同步执行，行为与原来一致。
type credSyncer struct {
	mu        sync.Mutex
	started   bool
	running   bool
	requested uint64
	done      uint64
	lastErr   string
	lastTrace string
	lastAt    time.Time
	pending   []string
	wake      chan struct{}
}

const (
	credSyncDebounce = 300 * time.Millisecond
	credSyncRetry    = 5 * time.Second
)

// CredSyncStatus 是 /sync-status 的返回：requested > done 表示还有改动没下发到 VPS。
type CredSyncStatus struct {
	Requested    uint64     `json:"requested"`
	Done         uint64     `json:"done"`
	Running      bool       `json:"running"`
	LastError    string     `json:"last_error,omitempty"`
	LastTrace    string     `json:"last_trace,omitempty"`
	LastAt       *time.Time `json:"last_at,omitempty"`
	GatesPending int        `json:"gates_pending"`
	GatesFailed  int        `json:"gates_failed"`
}

// RequestCredSync 登记一次全量凭据同步，返回本次登记的序号。
func (s *Server) RequestCredSync(ctx context.Context) (uint64, error) {
	q := &s.creds
	q.mu.Lock()
	q.requested++
	seq := q.requested
	if trace := taskTrace(ctx); trace != "" {
		if q.started {
			q.pending = append(q.pending, trace)
		}
	}
	started := q.started
	q.mu.Unlock()
	if !started {
		err := s.SyncAll(ctx)
		q.finish(seq, nil, err)
		return seq, err
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return seq, nil
}

func (q *credSyncer) finish(seq uint64, traces []string, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.running = false
	q.lastAt = time.Now()
	if err != nil {
		q.lastErr = redact(err.Error())
		// 失败的登记退回队列，下一轮重试时仍能关联到原请求
		q.pending = append(traces, q.pending...)
		return
	}
	q.lastErr = ""
	if seq > q.done {
		q.done = seq
	}
}

func (s *Server) credSyncStatus() CredSyncStatus {
	q := &s.creds
	q.mu.Lock()
	defer q.mu.Unlock()
	st := CredSyncStatus{Requested: q.requested, Done: q.done, Running: q.running, LastError: q.lastErr, LastTrace: q.lastTrace}
	if !q.lastAt.IsZero() {
		at := q.lastAt
		st.LastAt = &at
	}
	return st
}

// runCredSync 是后台协程：随 StartBackground 启动，ctx 结束时退出。
func (s *Server) runCredSync(ctx context.Context) {
	q := &s.creds
	q.mu.Lock()
	q.wake = make(chan struct{}, 1)
	q.started = true
	q.mu.Unlock()
	defer func() {
		q.mu.Lock()
		q.started = false
		q.mu.Unlock()
	}()
	var retry <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.wake:
		case <-retry:
		}
		retry = nil
		// 合并紧接着到来的登记
		select {
		case <-ctx.Done():
			return
		case <-time.After(credSyncDebounce):
		}
		q.mu.Lock()
		if q.done >= q.requested {
			q.mu.Unlock()
			continue
		}
		seq, traces := q.requested, q.pending
		q.pending, q.running = nil, true
		round := randomHex(16)
		if len(traces) == 1 {
			round = traces[0]
		}
		q.lastTrace = round
		q.mu.Unlock()
		rctx := withTrace(ctx, round)
		finish := operationStage(rctx, "cred.sync", "seq", seq, "merged", len(traces), "links", traces)
		s.state.RLock()
		var err error
		if s.Store != nil {
			err = s.SyncAll(rctx)
		} else {
			err = context.Canceled
		}
		s.state.RUnlock()
		finish(err)
		q.finish(seq, traces, err)
		if err != nil {
			slog.Warn("cred sync", "seq", seq, "trace", round, "err", redact(err.Error()))
			retry = time.After(credSyncRetry)
		}
	}
}

func (s *Server) credSyncRoutes(auth, admin *gin.RouterGroup) {
	admin.GET("/sync-status", func(c *gin.Context) {
		status := s.credSyncStatus()
		pending := s.Store.Ent.Gate.Query().Where(gate.StatusEQ(gate.StatusActive), func(selector *entsql.Selector) {
			selector.Where(entsql.ColumnsLT(selector.C(gate.FieldAppliedRevision), selector.C(gate.FieldConfigRevision)))
		})
		ctx := c.Request.Context()
		var err error
		status.GatesPending, err = pending.Clone().Count(ctx)
		if err == nil {
			status.GatesFailed, err = pending.Where(func(selector *entsql.Selector) {
				selector.Where(entsql.ColumnsEQ(selector.C(gate.FieldFailedRevision), selector.C(gate.FieldConfigRevision)))
			}).Count(ctx)
		}
		if err != nil {
			failInternal(c, 503, "query_failed", err)
			return
		}
		c.JSON(200, status)
	})
	// 普通用户只看是否还有改动在下发，不看内部错误与追踪编号。
	auth.GET("/me/sync-status", func(c *gin.Context) {
		st := s.credSyncStatus()
		c.JSON(200, gin.H{"pending": st.Requested > st.Done})
	})
}
