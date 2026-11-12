package center

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/subgroup"
)

const subscriptionRunKind = "subscription-run"

type subscriptionStage struct {
	ProfileID     int       `json:"profile_id"`
	SourceID      int       `json:"source_id,omitempty"`
	Operation     string    `json:"operation"`
	Started       time.Time `json:"started"`
	Finished      time.Time `json:"finished"`
	Outcome       string    `json:"outcome"`
	ErrorCode     string    `json:"error_code,omitempty"`
	Origin        string    `json:"origin"`
	CacheFallback bool      `json:"cache_fallback,omitempty"`
}

type subscriptionRun struct {
	ID        string              `json:"id"`
	TraceID   string              `json:"trace_id"`
	ProfileID int                 `json:"profile_id"`
	Started   time.Time           `json:"started"`
	Finished  time.Time           `json:"finished"`
	Outcome   string              `json:"outcome"`
	Complete  bool                `json:"complete"`
	Stages    []subscriptionStage `json:"stages,omitempty"`
}

type subscriptionRecorder struct {
	mu  sync.Mutex
	run subscriptionRun
}
type subscriptionRecorderKey struct{}

func (s *Server) startSubscriptionRun(ctx context.Context, profile int) (context.Context, func(error)) {
	if _, ok := ctx.Value(subscriptionRecorderKey{}).(*subscriptionRecorder); ok {
		return ctx, func(error) {}
	}
	ctx = withTrace(ctx, taskTrace(ctx))
	r := &subscriptionRecorder{run: subscriptionRun{ID: randomHex(16), TraceID: taskTrace(ctx), ProfileID: profile, Started: time.Now().UTC(), Complete: true, Stages: []subscriptionStage{}}}
	ctx = context.WithValue(ctx, subscriptionRecorderKey{}, r)
	var once sync.Once
	return ctx, func(err error) {
		once.Do(func() {
			r.mu.Lock()
			r.run.Finished, r.run.Outcome = time.Now().UTC(), "succeeded"
			for _, stage := range r.run.Stages {
				if stage.Outcome == "failed" {
					r.run.Outcome = "failed"
				}
			}
			if err != nil {
				r.run.Outcome = "failed"
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
				r.run.Outcome = "timed_out"
			} else if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
				r.run.Outcome = "cancelled"
			}
			data := toMap(r.run)
			r.mu.Unlock()
			storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			_, saveErr := s.Store.Ent.Resource.Create().SetKind(subscriptionRunKind).SetKey(r.run.ID).SetData(data).Save(storeCtx)
			s.tasks.note("subscription-timeline", saveErr)
		})
	}
}

func subscriptionStageResult(ctx context.Context, profile int, operation, origin string, started time.Time, err error) {
	stage := subgroup.Stage{Operation: operation, Started: started, Finished: time.Now().UTC()}
	if err != nil {
		stage.Error = err.Error()
	}
	recordSubscriptionStage(ctx, profile, stage, origin)
}

func writeSubscriptionBody(c *gin.Context, ctx context.Context, profile int, contentType string, body []byte) error {
	started := time.Now().UTC()
	c.Header("Content-Type", contentType)
	c.Status(200)
	n, err := c.Writer.Write(body)
	if err == nil && n != len(body) {
		err = io.ErrShortWrite
	}
	stageErr := err
	if err != nil {
		stageErr = errors.New("response_write_failed")
	} else if ctx.Err() != nil {
		err, stageErr = ctx.Err(), ctx.Err()
	}
	subscriptionStageResult(ctx, profile, "subscription.response", "center.writeSubscriptionBody", started, stageErr)
	return err
}

func recordSubscriptionStage(ctx context.Context, profile int, stage subgroup.Stage, origin string) {
	r, ok := ctx.Value(subscriptionRecorderKey{}).(*subscriptionRecorder)
	if !ok {
		return
	}
	outcome := "succeeded"
	if stage.Error != "" {
		outcome = "failed"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.run.Stages) >= 512 {
		r.run.Complete = false
		return
	}
	// Raw upstream errors may include URLs. Persist only internal error codes.
	code := subscriptionStageCode(stage.Error)
	r.run.Stages = append(r.run.Stages, subscriptionStage{ProfileID: profile, SourceID: stage.SourceID, Operation: stage.Operation, Started: stage.Started, Finished: stage.Finished, Outcome: outcome, ErrorCode: code, Origin: origin, CacheFallback: stage.CacheFallback})
}

func subscriptionStageCode(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "converter_") {
		for _, code := range []string{"converter_endpoint_invalid", "converter_target_unsupported", "converter_settings_unsupported", "converter_template_unsupported", "converter_template_required", "converter_input_invalid", "converter_protocol_unsupported", "converter_empty_input", "converter_input_too_large", "converter_settings_unavailable", "converter_not_configured", "converter_secret_unavailable", "converter_request_failed", "converter_result_invalid"} {
			if raw == code {
				return raw
			}
		}
	}
	for _, code := range []string{"source_rules_invalid", "manual_rules_invalid", "group_rules_invalid"} {
		if raw == code || strings.HasPrefix(raw, code+":") || strings.HasPrefix(raw, code+"[") {
			return code
		}
	}
	switch raw {
	case "source_result_save_failed", "source_content_invalid", "source_no_supported_nodes", "node_count_drop_rejected", "source_failed", "profile_member_inactive", "profile_graph_invalid", "profile_expansion_exceeded", "unknown_profile",
		"unsupported_target", "invalid_yaml_template", "template_target_unsupported", "empty_output", "format_conversion_failed", "response_write_failed", "profile_output_too_large":
		return raw
	case context.Canceled.Error():
		return "cancelled"
	case context.DeadlineExceeded.Error():
		return "timed_out"
	default:
		return "source_fetch_failed"
	}
}

func (s *Server) listSubscriptionRuns(c *gin.Context) {
	rows, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(subscriptionRunKind)).Order(ent.Desc(resource.FieldID)).Limit(100).All(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	items := []subscriptionRun{}
	for _, row := range rows {
		var run subscriptionRun
		raw, _ := json.Marshal(row.Data)
		if err := json.Unmarshal(raw, &run); err != nil {
			failInternal(c, 503, "query_failed", err)
			return
		}
		run.Stages = nil
		items = append(items, run)
	}
	c.JSON(200, gin.H{"items": items})
}

func (s *Server) loadSubscriptionRun(ctx context.Context, id string) (subscriptionRun, error) {
	var run subscriptionRun
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(subscriptionRunKind), resource.KeyEQ(id)).Only(ctx)
	if err != nil {
		return run, err
	}
	raw, _ := json.Marshal(row.Data)
	err = json.Unmarshal(raw, &run)
	return run, err
}

func (s *Server) getSubscriptionRun(c *gin.Context) {
	id := c.Param("id")
	if !validTraceHex(id, 32) {
		failure(c, 404, "subscription_run_not_found")
		return
	}
	run, err := s.loadSubscriptionRun(c.Request.Context(), id)
	if ent.IsNotFound(err) {
		failure(c, 404, "subscription_run_not_found")
		return
	}
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, run)
}
