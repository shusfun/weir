package agent

import (
	"math"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func TestHostMemoryUnknownAndBounds(t *testing.T) {
	a, b := parseHostMemory("MemTotal: 1024 kB\nMemAvailable: 512 kB\n")
	if a == nil || b == nil || *a != 1048576 || *b != 524288 {
		t.Fatal("valid memory sample")
	}
	for _, s := range []string{"", "MemTotal: 1 kB", "MemTotal: 1 kB\nMemAvailable: 2 kB", "MemTotal: 2 kB\nMemTotal: 3 kB\nMemAvailable: 1 kB", "MemTotal: " + strconv.FormatUint(math.MaxUint64, 10) + " kB\nMemAvailable: 0 kB"} {
		if a, b := parseHostMemory(s); a != nil || b != nil {
			t.Fatal("unknown sample represented as memory")
		}
	}
}
func TestHostNetworkExactAndUnknown(t *testing.T) {
	line := func(name, rx, tx string) string {
		return name + ": " + rx + " 0 0 0 0 0 0 0 " + tx + " 0 0 0 0 0 0 0\n"
	}
	a, b := parseHostNetwork(line("lo", "999", "999") + line("eth0", "9007199254740993", "2") + line("wg0", "3", "4"))
	if a == nil || b == nil || *a != "9007199254740996" || *b != "6" {
		t.Fatal("network precision or loopback exclusion")
	}
	for _, s := range []string{"", line("lo", "1", "1"), "eth0: missing", line("eth0", "1", "2") + line("eth0", "3", "4"), line("eth0", "18446744073709551615", "1") + line("eth1", "1", "1")} {
		if a, b := parseHostNetwork(s); a != nil || b != nil {
			t.Fatal("invalid/overflow network represented as zero")
		}
	}
	for _, s := range []string{"NaN", "Inf", "-1", "bad"} {
		if _, ok := hostFloat(s); ok {
			t.Fatal("invalid load or uptime accepted")
		}
	}
}

func TestActualLinuxHostSample(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only procfs sample")
	}
	h := observeHost(t.TempDir())
	if h.Validity != "available" || h.MemoryTotal == nil || h.DiskTotal == nil || h.NetworkRX == nil || h.LogicalCPUs == nil {
		t.Fatalf("Linux host sample unavailable: %s", h.Validity)
	}
	at, e := time.Parse(time.RFC3339, h.ObservedAt)
	if e != nil || time.Since(at) > 5*time.Second {
		t.Fatal("missing sample provenance")
	}
	h = observeHost("/migate-owned-nonexistent-host-fixture")
	if h.Validity != "partial" || h.DiskTotal != nil {
		t.Fatal("missing filesystem falsely healthy")
	}
}
