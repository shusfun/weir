package dsh

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

func namespace(value string) string {
	if namespacePattern.MatchString(value) {
		return value
	}
	return "dsh"
}

func ValidNamespace(value string) bool { return namespacePattern.MatchString(value) }

func allowedEnvironment(key string) bool {
	return key == "DEEPSEEK_BASE_URL"
}

// Event contains operation metadata, never prompts, credentials or tool arguments.
type Event struct {
	Operation string
	TraceID   string
	Started   time.Time
	Finished  time.Time
	Status    string
	Code      string
}

type Observer func(context.Context, Event)

type observationKey struct{}
type observation struct {
	id       string
	observer Observer
}

// WithObservation associates runtime and tool operations with the host's trace.
func WithObservation(ctx context.Context, traceID string, observer Observer) context.Context {
	return context.WithValue(ctx, observationKey{}, observation{traceID, observer})
}

func observe(ctx context.Context, operation string, fallback Observer) func(error) {
	started := time.Now().UTC()
	meta, _ := ctx.Value(observationKey{}).(observation)
	if meta.observer != nil {
		fallback = meta.observer
	}
	return func(err error) {
		if fallback == nil {
			return
		}
		e := Event{Operation: operation, TraceID: meta.id, Started: started, Finished: time.Now().UTC(), Status: "succeeded"}
		if err != nil {
			e.Status, e.Code = "failed", "operation_failed"
		}
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			e.Status, e.Code = "cancelled", "context_cancelled"
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			e.Status, e.Code = "timeout", "deadline_exceeded"
		}
		fallback(ctx, e)
	}
}
