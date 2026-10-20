package agent

import (
	"errors"
	"github.com/imzyb/MiGate/packages/contracts"
	"io"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func readHostFile(name string) (string, error) {
	f, e := os.Open(name)
	if e != nil {
		return "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 65537))
	if e != nil || len(b) > 65536 {
		return "", errors.New("host_sample_unavailable")
	}
	return string(b), nil
}
func hostFloat(s string) (float64, bool) {
	v, e := strconv.ParseFloat(s, 64)
	return v, e == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}
func parseHostMemory(raw string) (*uint64, *uint64) {
	values := map[string]uint64{}
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[2] != "kB" {
			continue
		}
		if f[0] != "MemTotal:" && f[0] != "MemAvailable:" {
			continue
		}
		n, e := strconv.ParseUint(f[1], 10, 64)
		if e != nil || n > math.MaxUint64/1024 {
			return nil, nil
		}
		if _, exists := values[f[0]]; exists {
			return nil, nil
		}
		values[f[0]] = n * 1024
	}
	total, ok := values["MemTotal:"]
	available, has := values["MemAvailable:"]
	if !ok || !has || total == 0 || available > total {
		return nil, nil
	}
	return &total, &available
}
func parseHostNetwork(raw string) (*string, *string) {
	var rx, tx uint64
	count := 0
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		name, body, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "lo" {
			continue
		}
		f := strings.Fields(body)
		if name == "" || seen[name] || len(f) != 16 {
			return nil, nil
		}
		seen[name] = true
		r, e := strconv.ParseUint(f[0], 10, 64)
		if e != nil {
			return nil, nil
		}
		v, e := strconv.ParseUint(f[8], 10, 64)
		if e != nil || r > math.MaxUint64-rx || v > math.MaxUint64-tx {
			return nil, nil
		}
		rx += r
		tx += v
		count++
	}
	if count == 0 {
		return nil, nil
	}
	r, t := strconv.FormatUint(rx, 10), strconv.FormatUint(tx, 10)
	return &r, &t
}
func observeHost(dataDir string) contracts.HostObservation {
	h := contracts.HostObservation{Source: "linux-procfs-statfs", ObservedAt: time.Now().UTC().Format(time.RFC3339), Validity: "unknown"}
	if runtime.GOOS != "linux" {
		return h
	}
	cpus := runtime.NumCPU()
	h.LogicalCPUs = &cpus
	if raw, e := readHostFile("/proc/meminfo"); e == nil {
		h.MemoryTotal, h.MemoryAvailable = parseHostMemory(raw)
	}
	if raw, e := readHostFile("/proc/net/dev"); e == nil {
		h.NetworkRX, h.NetworkTX = parseHostNetwork(raw)
	}
	if raw, e := readHostFile("/proc/loadavg"); e == nil {
		f := strings.Fields(raw)
		if len(f) >= 3 {
			v := []float64{}
			for _, s := range f[:3] {
				n, ok := hostFloat(s)
				if !ok {
					v = nil
					break
				}
				v = append(v, n)
			}
			if len(v) == 3 {
				h.Load = v
			}
		}
	}
	if raw, e := readHostFile("/proc/uptime"); e == nil {
		f := strings.Fields(raw)
		if len(f) > 0 {
			if v, ok := hostFloat(f[0]); ok {
				h.UptimeSeconds = &v
			}
		}
	}
	h.DiskTotal, h.DiskAvailable = hostDisk(dataDir)
	h.Validity = "partial"
	if h.MemoryTotal != nil && h.NetworkRX != nil && len(h.Load) == 3 && h.UptimeSeconds != nil && h.DiskTotal != nil {
		h.Validity = "available"
	}
	return h
}
