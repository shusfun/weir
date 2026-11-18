package center

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
)

type stageValue func() slog.Value

func (value stageValue) LogValue() slog.Value { return value() }

// operationStage records identifiers and outcomes only; callers must not pass
// credentials, subscription URLs, or unfiltered upstream errors as attributes.
func operationStage(ctx context.Context, operation string, attrs ...any) func(error) {
	started := time.Now()
	pc, file, line, _ := runtime.Caller(1)
	function := ""
	if f := runtime.FuncForPC(pc); f != nil {
		function = f.Name()
	}
	file = filepath.ToSlash(file)
	if i := strings.LastIndex(file, "/internal/"); i >= 0 {
		file = file[i+1:]
	}
	trace, _ := ctx.Value(httpTraceKey{}).(httpTrace)
	traceID := taskTrace(ctx)
	base := []any{"operation", operation, "trace", traceID, "span", randomHex(8), "parent_span", trace.Span,
		"code.file.path", file, "code.line.number", line, "code.function.name", function,
		"service.version", buildinfo.Version, "vcs.ref.head.revision", buildinfo.Revision}
	base = append(base, attrs...)
	return func(err error) {
		now := time.Now()
		duration := now.Sub(started)
		if metrics, ok := ctx.Value(hotspotContextKey{}).(*hotspotMetrics); ok {
			metrics.record(operation, traceID, now, duration, err != nil)
		}
		outcome := "succeeded"
		if err != nil {
			outcome = "failed"
		}
		slog.InfoContext(ctx, "operation", append(base, "outcome", outcome, "duration_ms", float64(duration.Microseconds())/1000)...)
	}
}
