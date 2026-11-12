package center

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

type communicationKey struct {
	gate   int
	route  string
	bucket int64
}
type communicationBuffer struct {
	mu              sync.Mutex
	flushMu         sync.Mutex
	pending         map[communicationKey]taskqueue.Communication
	lastFlush       time.Time
	droppedRequests int64
}
type countedBody struct {
	io.ReadCloser
	n int64
}

func (b *countedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.n += int64(n)
	return n, err
}

func (s *Server) agentCommunication(c *gin.Context) {
	body := &countedBody{ReadCloser: c.Request.Body}
	c.Request.Body = body
	c.Next()
	route := c.FullPath()
	if route == "" {
		return
	}
	entry := taskqueue.Communication{GateID: gateFrom(c).ID, Route: route, Bucket: time.Now().UTC().Truncate(time.Minute), Requests: 1, Received: body.n, Sent: int64(max(0, c.Writer.Size()))}
	if c.Writer.Status() >= 400 {
		entry.Errors = 1
	}
	s.communication.add(entry)
}

func (b *communicationBuffer) add(entry taskqueue.Communication) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil {
		b.pending = map[communicationKey]taskqueue.Communication{}
	}
	key := communicationKey{entry.GateID, entry.Route, entry.Bucket.UnixMilli()}
	if _, ok := b.pending[key]; !ok && len(b.pending) >= 10000 {
		b.droppedRequests += entry.Requests
		return
	}
	old := b.pending[key]
	entry.Requests += old.Requests
	entry.Received += old.Received
	entry.Sent += old.Sent
	entry.Errors += old.Errors
	b.pending[key] = entry
}

func (s *Server) flushCommunication(ctx context.Context, now time.Time, force bool) error {
	b := &s.communication
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	b.mu.Lock()
	if !force && now.Sub(b.lastFlush) < time.Minute {
		b.mu.Unlock()
		return nil
	}
	pending := b.pending
	b.pending = nil
	b.lastFlush = now
	b.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	entries := make([]taskqueue.Communication, 0, len(pending))
	for _, entry := range pending {
		entries = append(entries, entry)
	}
	if err := s.Store.Tasks.AddCommunication(ctx, entries); err != nil {
		for _, entry := range entries {
			b.add(entry)
		}
		return err
	}
	return nil
}

func (s *Server) workerObservationRoutes(admin *gin.RouterGroup) {
	admin.GET("/workers/system", s.systemStatus)
	s.workerPolicyRoutes(admin)
	s.diagnosticRoutes(admin)
	s.observationRoutes(admin)
	admin.GET("/workers/statistics", func(c *gin.Context) {
		now := time.Now().UTC()
		window := 24 * time.Hour
		switch c.Query("range") {
		case "", "24h":
		case "1h":
			window = time.Hour
		case "7d":
			window = 7 * 24 * time.Hour
		default:
			failure(c, 400, "invalid_statistics_range")
			return
		}
		stats, err := s.Store.Tasks.Statistics(c.Request.Context(), now.Add(-window), now)
		if err != nil {
			failInternal(c, 503, "task_statistics_unavailable", err)
			return
		}
		c.JSON(200, stats)
	})
	admin.GET("/workers/tasks", func(c *gin.Context) {
		rs, err := s.Store.Tasks.List(c.Request.Context(), 200)
		if err != nil {
			failInternal(c, 503, "tasks_unavailable", err)
			return
		}
		for i := range rs {
			rs[i].Result = nil
		}
		c.JSON(200, gin.H{"items": rs})
	})
	admin.GET("/workers/tasks/:id", func(c *gin.Context) {
		r, err := s.Store.Tasks.Get(c.Request.Context(), c.Param("id"))
		if err != nil {
			failure(c, 404, "task_not_found")
			return
		}
		r.Ciphertext = ""
		r.Result = nil
		r.Task.Payload = nil
		r.Task.LeaseToken = ""
		a, err := s.Store.Tasks.Activity(c.Request.Context(), r.Task.ID)
		if err != nil {
			failInternal(c, 503, "task_activity_unavailable", err)
			return
		}
		c.JSON(200, gin.H{"task": r, "activity": a})
	})
	admin.POST("/workers/tasks/:id/timeline", s.workerTaskTimeline)
	admin.POST("/workers/tasks/:id/cancel", func(c *gin.Context) {
		if err := s.Store.Tasks.Cancel(c.Request.Context(), c.Param("id")); err != nil {
			failure(c, 409, "task_not_cancellable")
			return
		}
		_ = s.Store.Notifications.Publish(c.Request.Context(), "workers")
		s.audit(c, "worker.cancel", c.Param("id"), "ok")
		c.Status(202)
	})
	admin.GET("/workers/communication", func(c *gin.Context) {
		if err := s.flushCommunication(c.Request.Context(), time.Now(), true); err != nil {
			failInternal(c, 503, "communication_unavailable", err)
			return
		}
		hours := 24
		if strings.TrimSpace(c.Query("range")) == "1h" {
			hours = 1
		}
		entries, err := s.Store.Tasks.Communication(c.Request.Context(), time.Now().Add(-time.Duration(hours)*time.Hour))
		if err != nil {
			failInternal(c, 503, "communication_unavailable", err)
			return
		}
		s.communication.mu.Lock()
		dropped := s.communication.droppedRequests
		s.communication.mu.Unlock()
		c.JSON(200, gin.H{"items": entries, "hours": hours, "dropped_requests_since_start": dropped, "complete": dropped == 0, "measurement": "HTTP request body bytes read and response body bytes written; excludes headers, TCP and TLS"})
	})
}

func (s *Server) workerTaskTimeline(c *gin.Context) {
	ctx := c.Request.Context()
	current, err := s.Store.Tasks.Get(ctx, c.Param("id"))
	if err != nil {
		failure(c, 404, "task_not_found")
		return
	}
	if current.Task.Kind == contracts.TaskTraceRead || current.GateID < 1 {
		failure(c, 409, "gate_timeline_unavailable")
		return
	}
	trace := current.Task.TraceID
	candidates, err := s.workerCandidates(ctx, contracts.TaskTraceRead, current.GateID)
	if err != nil {
		failure(c, 409, "gate_timeline_unavailable")
		return
	}
	found := false
	for _, id := range candidates {
		if id == current.GateID {
			found = true
			break
		}
	}
	if !found {
		failure(c, 409, "gate_timeline_unavailable")
		return
	}
	request := contracts.TraceReadTask{TraceID: trace, Limit: 200}
	raw, _, err := s.dispatchWorkerTask(withTrace(ctx, trace), contracts.TaskTraceRead, request, contracts.TaskRequirements{TargetGateID: current.GateID}, []int{current.GateID})
	if err != nil {
		failDetail(c, 503, "gate_timeline_unavailable", "无法读取这台 VPS 的本地环节记录", nil)
		return
	}
	var result contracts.TraceReadResult
	if err = contracts.DecodeStrict(raw, &result, contracts.MaxTaskResult); err != nil {
		failInternal(c, 502, "gate_timeline_invalid", err)
		return
	}
	s.audit(c, "worker.timeline", current.Task.ID, "ok")
	c.JSON(200, result)
}

func (s *Server) validateTraceReadResult(ctx context.Context, record taskqueue.Record, raw []byte) error {
	body, err := s.Crypto.Decrypt(record.Ciphertext)
	if err != nil {
		return err
	}
	var request contracts.TraceReadTask
	if err = contracts.DecodeStrict([]byte(body), &request, contracts.MaxTaskPayload); err != nil {
		return err
	}
	if request.TraceID != record.Task.TraceID || request.Limit < 1 || request.Limit > 200 || request.After < 0 {
		return errors.New("trace_request_mismatch")
	}
	var result contracts.TraceReadResult
	if err = contracts.DecodeStrict(raw, &result, contracts.MaxTaskResult); err != nil {
		return err
	}
	if result.CollectedAt.IsZero() || len(result.Records) > request.Limit || len(result.MissingReason) > 100 {
		return errors.New("trace_result_bounds_invalid")
	}
	for _, item := range result.Records {
		id, decodeErr := hex.DecodeString(item.TaskID)
		if decodeErr != nil || len(id) != 24 || item.TraceID != request.TraceID || (!contracts.KnownTask(item.Kind) && item.Kind!="config.apply") || item.Attempt < 1 || item.Stage == "" || len(item.Stage) > 100 || len(item.Origin) == 0 || len(item.Origin) > 256 || len(item.ErrorCode) > 100 || item.Started.IsZero() || item.Finished.Before(item.Started) {
			return errors.New("trace_record_invalid")
		}
		switch item.Outcome {
		case "succeeded", "failed", "cancelled", "timeout":
		default:
			return errors.New("trace_outcome_invalid")
		}
	}
	return ctx.Err()
}
