package control

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHostTelemetryProjection(t *testing.T) {
	now := time.Now().UTC()
	v := map[string]any{"source": "linux-procfs-statfs", "observed_at": now.Format(time.RFC3339), "validity": "available", "logical_cpus": 2, "load": []float64{1, 2, 3}, "uptime_seconds": 12, "memory_total": 1024, "memory_available": 512, "disk_total": 2048, "disk_available": 1024, "network_rx": "9007199254740993", "network_tx": "0", "raw_config": "private"}
	h := sanitizeHostObservation(v, now)
	if h.Validity != "available" || h.NetworkRX == nil || *h.NetworkRX != "9007199254740993" {
		t.Fatal("valid host sample lost")
	}
	raw, _ := json.Marshal(h)
	var projected map[string]any
	json.Unmarshal(raw, &projected)
	if _, ok := projected["raw_config"]; ok {
		t.Fatal("unapproved telemetry leaked")
	}
	v["memory_available"] = 2048
	v["network_rx"] = "-1"
	h = sanitizeHostObservation(v, now)
	if h.Validity != "partial" || h.MemoryTotal != nil || h.NetworkRX != nil {
		t.Fatal("bad counters shown as valid")
	}
	for _, at := range []string{now.Add(-91 * time.Second).Format(time.RFC3339), now.Add(61 * time.Second).Format(time.RFC3339), "bad"} {
		v["observed_at"] = at
		if sanitizeHostObservation(v, now).Validity != "unknown" {
			t.Fatal("stale or future host accepted")
		}
	}
	if sanitizeHostObservation(nil, now).Validity != "unknown" {
		t.Fatal("missing host sample zeroed")
	}
}
