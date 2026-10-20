package agent

import (
	"testing"
	"time"
)

func TestHostCPUIntervalResetAndGuest(t *testing.T) {
	at := time.Now()
	before := parseHostCPU("cpu 100 10 40 1000 50 0 0 0 400 40\ncpu0 1", at)
	after := parseHostCPU("cpu 120 10 60 1060 50 0 0 0 900 90", at.Add(15*time.Second))
	value, seconds := cpuUsage(before, after)
	if value == nil || *value != 40 || seconds == nil || *seconds != 15 {
		t.Fatal("guest double counted or idle normalization wrong")
	}
	for _, bad := range []cpuSample{{}, parseHostCPU("cpu malformed", at.Add(15*time.Second)), parseHostCPU("cpu 120 10 60 1060 50 0 0 0", at.Add(61*time.Second)), parseHostCPU("cpu 120 10 60 1060 50 0 0 0", at.Add(time.Millisecond)), parseHostCPU("cpu 99 10 60 1060 50 0 0 0", at.Add(15*time.Second)), parseHostCPU("cpu 100 10 40 1000 50 0 0 0", at.Add(15*time.Second))} {
		if v, _ := cpuUsage(before, bad); v != nil {
			t.Fatal("invalid baseline/reset/gap represented as CPU")
		}
	}
	sampler := hostCPUSampler{}
	if v, _ := sampler.sample("cpu 100 10 40 1000 50 0 0 0", at); v != nil {
		t.Fatal("first sample must be unknown")
	}
	if v, _ := sampler.sample("cpu 120 10 60 1060 50 0 0 0", at.Add(15*time.Second)); v == nil || *v != 40 {
		t.Fatal("second sample missing")
	}
	if parseHostCPU("cpu 18446744073709551616 0 0 0 0 0 0 0", at).valid {
		t.Fatal("overflow accepted")
	}
}
