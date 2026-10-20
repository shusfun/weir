package control

import (
	"encoding/json"
	"github.com/imzyb/MiGate/packages/contracts"
	"math"
	"strconv"
	"time"
)

// Project a fixed telemetry contract; unknown agent extensions, raw host files
// or malformed observations must never become arbitrary browser metadata.
func sanitizeHostObservation(value any, now time.Time) contracts.HostObservation {
	unknown := contracts.HostObservation{Source: "linux-procfs-statfs", Validity: "unknown"}
	raw, e := json.Marshal(value)
	if e != nil || len(raw) > 4096 {
		return unknown
	}
	var h contracts.HostObservation
	if json.Unmarshal(raw, &h) != nil || h.Source != unknown.Source {
		return unknown
	}
	at, e := time.Parse(time.RFC3339, h.ObservedAt)
	if e != nil || at.After(now.Add(time.Minute)) || now.Sub(at) > 90*time.Second {
		return unknown
	}
	if h.CPUPercent == nil || h.CPUInterval == nil || math.IsNaN(*h.CPUPercent) || math.IsInf(*h.CPUPercent, 0) || *h.CPUPercent < 0 || *h.CPUPercent > 100 || math.IsNaN(*h.CPUInterval) || math.IsInf(*h.CPUInterval, 0) || *h.CPUInterval < 1 || *h.CPUInterval > 60 {
		h.CPUPercent = nil
		h.CPUInterval = nil
	}
	if h.LogicalCPUs != nil && (*h.LogicalCPUs < 1 || *h.LogicalCPUs > 65536) {
		h.LogicalCPUs = nil
	}
	if len(h.Load) != 3 {
		h.Load = nil
	} else {
		for _, n := range h.Load {
			if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1e6 {
				h.Load = nil
				break
			}
		}
	}
	if h.UptimeSeconds != nil && (math.IsNaN(*h.UptimeSeconds) || math.IsInf(*h.UptimeSeconds, 0) || *h.UptimeSeconds < 0 || *h.UptimeSeconds > 3155760000) {
		h.UptimeSeconds = nil
	}
	if h.MemoryTotal == nil || h.MemoryAvailable == nil || *h.MemoryTotal == 0 || *h.MemoryAvailable > *h.MemoryTotal {
		h.MemoryTotal = nil
		h.MemoryAvailable = nil
	}
	if h.DiskTotal == nil || h.DiskAvailable == nil || *h.DiskTotal == 0 || *h.DiskAvailable > *h.DiskTotal {
		h.DiskTotal = nil
		h.DiskAvailable = nil
	}
	for _, v := range []**string{&h.NetworkRX, &h.NetworkTX} {
		if *v != nil {
			n, e := strconv.ParseUint(**v, 10, 64)
			if e != nil || strconv.FormatUint(n, 10) != **v {
				*v = nil
			}
		}
	}
	h.Validity = "partial"
	if h.LogicalCPUs != nil && len(h.Load) == 3 && h.UptimeSeconds != nil && h.MemoryTotal != nil && h.DiskTotal != nil && h.NetworkRX != nil && h.NetworkTX != nil {
		h.Validity = "available"
	}
	if h.LogicalCPUs == nil && h.Load == nil && h.UptimeSeconds == nil && h.MemoryTotal == nil && h.DiskTotal == nil && h.NetworkRX == nil && h.NetworkTX == nil {
		h.Validity = "unknown"
	}
	return h
}
