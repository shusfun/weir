package subgroup

import (
	"context"
	"time"
)

type Stage struct {
	Operation         string
	SourceID          int
	Started, Finished time.Time
	Error             string
	CacheFallback     bool
}

type Observer func(context.Context, Stage)
type observerKey struct{}

func WithObserver(ctx context.Context, observer Observer) context.Context {
	return context.WithValue(ctx, observerKey{}, observer)
}

func beginStage(ctx context.Context, operation string, sourceID int) func(string, bool) {
	started := time.Now().UTC()
	return func(err string, cached bool) {
		if observer, ok := ctx.Value(observerKey{}).(Observer); ok && observer != nil {
			observer(ctx, Stage{Operation: operation, SourceID: sourceID, Started: started, Finished: time.Now().UTC(), Error: err, CacheFallback: cached})
		}
	}
}
