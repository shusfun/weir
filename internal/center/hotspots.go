package center

import (
	"context"
	"math/bits"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const hotspotWindow = 15

var hotspotOperations = [...]string{"worker.maintenance", "worker.tasks.list", "worker.tasks.statistics", "traffic.domains.country-data"}

type hotspotSample struct {
	Operation string     `json:"operation"`
	Calls     int        `json:"calls"`
	Failures  int        `json:"failures"`
	Slow      int        `json:"slow"`
	P95MS     *int64     `json:"p95_ms"`
	MaxMS     *int64     `json:"max_ms"`
	Anomaly   *time.Time `json:"last_anomaly_at"`
	Trace     string     `json:"trace_id,omitempty"`
}

type hotspotMinute struct {
	minute                int64
	counts                [64]int
	calls, failures, slow int
	max                   int64
	anomaly               time.Time
	trace                 string
}

type hotspotMetrics struct {
	mu      sync.Mutex
	started time.Time
	minutes [len(hotspotOperations)][hotspotWindow]hotspotMinute
}

type hotspotContextKey struct{}

func (s *Server) hotspotContext(ctx context.Context) context.Context {
	if !validTraceHex(traceOf(ctx), 32) {
		ctx = context.WithValue(ctx, httpTraceKey{}, httpTrace{ID: randomHex(16)})
	}
	return context.WithValue(ctx, hotspotContextKey{}, &s.hotspots)
}

func (m *hotspotMetrics) record(operation, trace string, now time.Time, duration time.Duration, failed bool) {
	for index, name := range hotspotOperations {
		if name != operation {
			continue
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.started.IsZero() {
			m.started = now
		}
		minute := now.Unix() / 60
		bucket := &m.minutes[index][minute%hotspotWindow]
		if bucket.minute != minute {
			*bucket = hotspotMinute{minute: minute}
		}
		ms := duration.Milliseconds()
		bucket.calls++
		bucket.counts[bits.Len64(uint64(ms))]++
		bucket.max = max(bucket.max, ms)
		slow := duration > time.Second
		if slow {
			bucket.slow++
		}
		if failed {
			bucket.failures++
		}
		if (failed || slow) && now.After(bucket.anomaly) {
			bucket.trace, bucket.anomaly = trace, now
		}
		return
	}
}

func (m *hotspotMetrics) snapshot(now time.Time) (time.Time, []hotspotSample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started.IsZero() {
		m.started = now
	}
	minute := now.Unix() / 60
	start := time.Unix((minute-hotspotWindow+1)*60, 0).UTC()
	if m.started.After(start) {
		start = m.started
	}
	items := make([]hotspotSample, 0, len(hotspotOperations))
	for index, name := range hotspotOperations {
		item := hotspotSample{Operation: name}
		var counts [64]int
		var maximum int64
		for _, bucket := range m.minutes[index] {
			if bucket.minute <= minute-hotspotWindow || bucket.minute > minute {
				continue
			}
			item.Calls += bucket.calls
			item.Failures += bucket.failures
			item.Slow += bucket.slow
			maximum = max(maximum, bucket.max)
			for bin, count := range bucket.counts {
				counts[bin] += count
			}
			if !bucket.anomaly.IsZero() && (item.Anomaly == nil || bucket.anomaly.After(*item.Anomaly)) {
				anomaly := bucket.anomaly
				item.Trace, item.Anomaly = bucket.trace, &anomaly
			}
		}
		if item.Calls > 0 {
			item.MaxMS = &maximum
		}
		if item.Calls >= 5 {
			rank := (95*item.Calls + 99) / 100
			for bin, count := range counts {
				rank -= count
				if rank <= 0 {
					p95 := min(maximum, int64((uint64(1)<<bin)-1))
					item.P95MS = &p95
					break
				}
			}
		}
		items = append(items, item)
	}
	return start, items
}

func (s *Server) listHotspots(c *gin.Context) {
	now := time.Now().UTC()
	start, items := s.hotspots.snapshot(now)
	c.JSON(200, gin.H{"coverage_start": start, "sampled_at": now, "items": items})
}
