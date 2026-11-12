package laya

import "context"

type correlationKey struct{}

// WithTrace associates local operations without sending identifiers to third parties.
func WithTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

func traceID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}
