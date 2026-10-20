package agent

import (
	"github.com/imzyb/MiGate/packages/contracts"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

type cpuSample struct {
	ticks [8]uint64
	at    time.Time
	valid bool
}
type hostCPUSampler struct {
	mu       sync.Mutex
	previous cpuSample
}

func parseHostCPU(raw string, at time.Time) cpuSample {
	line, _, _ := strings.Cut(raw, "\n")
	f := strings.Fields(line)
	s := cpuSample{at: at}
	if len(f) < 9 || f[0] != "cpu" {
		return s
	}
	for i := range s.ticks {
		n, e := strconv.ParseUint(f[i+1], 10, 64)
		if e != nil {
			return s
		}
		s.ticks[i] = n
	}
	s.valid = true
	return s
}

// Linux guest counters are already included in user/nice; use the first eight
// counters once. Idle includes iowait. Reset, first sample and gaps are unknown.
func cpuUsage(before, after cpuSample) (*float64, *float64) {
	seconds := after.at.Sub(before.at).Seconds()
	if !before.valid || !after.valid || seconds < 1 || seconds > 60 {
		return nil, nil
	}
	var total, idle uint64
	for i, n := range after.ticks {
		if n < before.ticks[i] {
			return nil, nil
		}
		delta := n - before.ticks[i]
		if delta > math.MaxUint64-total {
			return nil, nil
		}
		total += delta
		if i == 3 || i == 4 {
			idle += delta
		}
	}
	if total == 0 || idle > total {
		return nil, nil
	}
	value := 100 * float64(total-idle) / float64(total)
	return &value, &seconds
}
func (s *hostCPUSampler) sample(raw string, at time.Time) (*float64, *float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := parseHostCPU(raw, at)
	value, seconds := cpuUsage(s.previous, next)
	s.previous = next
	return value, seconds
}

func (a *Agent) observeHost() contracts.HostObservation {
	h := observeHost(a.Config.DataDir)
	raw, _ := readHostFile("/proc/stat")
	h.CPUPercent, h.CPUInterval = a.hostCPU.sample(raw, time.Now())
	return h
}
