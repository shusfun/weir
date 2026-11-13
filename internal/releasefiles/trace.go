package releasefiles

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

type traceKey struct{}

// WithTraceID resumes a durable operation without inventing a shared span.
func WithTraceID(ctx context.Context, id string) context.Context {
	var span [8]byte
	_, _ = rand.Read(span[:])
	return WithTrace(ctx, "00-"+id+"-"+hex.EncodeToString(span[:])+"-01")
}

func WithTrace(ctx context.Context, parent string) context.Context {
	parts := strings.Split(parent, "-")
	id := ""
	if len(parts) == 4 && parts[0] == "00" && len(parts[1]) == 32 && len(parts[2]) == 16 && len(parts[3]) == 2 && strings.ToLower(parent) == parent && parts[1] != strings.Repeat("0", 32) && parts[2] != strings.Repeat("0", 16) {
		_, spanErr := hex.DecodeString(parts[2])
		flags, flagErr := hex.DecodeString(parts[3])
		if _, err := hex.DecodeString(parts[1]); err == nil && spanErr == nil && flagErr == nil && flags[0]&0xfe == 0 {
			id = parts[1]
		}
	}
	if id == "" {
		var b [16]byte
		_, _ = rand.Read(b[:])
		id = hex.EncodeToString(b[:])
	}
	return context.WithValue(ctx, traceKey{}, id)
}

func Trace(ctx context.Context) string {
	id, _ := ctx.Value(traceKey{}).(string)
	return id
}

func Traceparent(ctx context.Context) string {
	id := Trace(ctx)
	if id == "" {
		return ""
	}
	var span [8]byte
	_, _ = rand.Read(span[:])
	return "00-" + id + "-" + hex.EncodeToString(span[:]) + "-01"
}
