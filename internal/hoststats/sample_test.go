package hoststats

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSamplerProcessIdentityAndUnknown(t *testing.T) {
	root := t.TempDir()
	s := Sampler{ProcRoot: root}
	write := func(name, text string) {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stat := func(ticks, start uint64) string {
		f := make([]string, 22)
		for i := range f {
			f[i] = "0"
		}
		f[0] = "S"
		f[11] = strconv.FormatUint(ticks, 10)
		f[19] = strconv.FormatUint(start, 10)
		f[21] = "10"
		return "42 (core with ) name) " + strings.Join(f, " ")
	}
	write("stat", "cpu 100 0 0 700 0 0 0 0 50 0\n")
	write("meminfo", "MemTotal: 1000 kB\nMemAvailable: 600 kB\n")
	write("42/stat", stat(100, 10))
	first := s.Sample("", map[string]int{"xray": 42, "sing-box": 0})
	if first.CPUPercent != nil || first.Processes["xray"].CPUPercent != nil || first.Processes["sing-box"].State != "stopped" {
		t.Fatalf("first=%+v", first)
	}
	write("stat", "cpu 200 0 0 800 0 0 0 0 100 0\n")
	write("42/stat", stat(150, 10))
	h := s.Sample("", map[string]int{"xray": 42})
	p := h.Processes["xray"]
	if h.CPUPercent == nil || *h.CPUPercent != 50 || p.CPUPercent == nil || *p.CPUPercent != 25 || p.MemoryRSS == nil || *p.MemoryRSS != uint64(10*os.Getpagesize()) {
		t.Fatalf("host=%+v process=%+v", h, p)
	}
	if h.MemUsed == nil || *h.MemUsed != 400*1024 {
		t.Fatalf("memory=%+v", h)
	}
	write("stat", "cpu 300 0 0 900 0 0 0 0 100 0\n")
	write("42/stat", stat(160, 20))
	if p := s.Sample("", map[string]int{"xray": 42}).Processes["xray"]; p.CPUPercent != nil || p.State != "running" {
		t.Fatalf("PID reuse=%+v", p)
	}
	if err := os.Remove(filepath.Join(root, "42/stat")); err != nil {
		t.Fatal(err)
	}
	if p := s.Sample("", map[string]int{"xray": 42}).Processes["xray"]; p.State != "unknown" || p.MemoryRSS != nil || p.Error == "" {
		t.Fatalf("unreadable=%+v", p)
	}
	s.prevAt = time.Now().Add(-2 * time.Minute)
	if h := s.Sample("", nil); h.CPUPercent != nil {
		t.Fatal("stale CPU reported")
	}
	write("meminfo", "MemTotal: 1000 kB\n")
	if h := s.Sample("", nil); h.MemUsed != nil {
		t.Fatal("missing available memory became zero")
	}
}
