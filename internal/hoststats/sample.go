// Package hoststats reads host and supervised-process observations without
// executing commands. A sampler belongs to one host and must not be shared.
package hoststats

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

type counter struct {
	ticks, start uint64
	pid          int
}

type Sampler struct {
	mu                  sync.Mutex
	ProcRoot            string
	prevTotal, prevBusy uint64
	prevAt              time.Time
	processes           map[string]counter
}

func ptr[T any](v T) *T { return &v }

func (s *Sampler) Sample(disk string, pids map[string]int) *contracts.HostSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	root := s.ProcRoot
	if root == "" {
		root = "/proc"
	}
	now := time.Now().UTC()
	h := &contracts.HostSample{ObservedAt: now, Processes: map[string]contracts.ProcessSample{}}
	read := func(name string) ([]byte, error) { return os.ReadFile(filepath.Join(root, name)) }
	var total, busy uint64
	if b, err := read("stat"); err == nil {
		line, _, _ := strings.Cut(string(b), "\n")
		f := strings.Fields(line)
		if len(f) >= 9 && f[0] == "cpu" {
			v := make([]uint64, 8)
			valid := true
			for i := range v {
				var err error
				v[i], err = strconv.ParseUint(f[i+1], 10, 64)
				valid = valid && err == nil
			}
			if valid {
				for _, n := range v {
					total += n
				}
				busy = total - v[3] - v[4]
			}
		}
	}
	interval := now.Sub(s.prevAt)
	validCPU := s.prevTotal > 0 && !s.prevAt.IsZero() && interval > 0 && interval <= time.Minute && total > s.prevTotal && busy >= s.prevBusy
	if validCPU {
		h.CPUPercent = ptr(float64(busy-s.prevBusy) * 100 / float64(total-s.prevTotal))
	}
	if b, err := read("meminfo"); err == nil {
		mem := map[string]uint64{}
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			if n, err := strconv.ParseUint(f[1], 10, 64); err == nil {
				mem[strings.TrimSuffix(f[0], ":")] = n * 1024
			}
		}
		t, tok := mem["MemTotal"]
		a, aok := mem["MemAvailable"]
		if tok && aok && t > 0 && a <= t {
			h.MemTotal, h.MemUsed = ptr(t), ptr(t-a)
		}
	}
	if b, err := read("loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if n, err := strconv.ParseFloat(f[0], 64); err == nil {
				h.Load1 = ptr(n)
			}
		}
	}
	if b, err := read("uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if n, err := strconv.ParseFloat(f[0], 64); err == nil && n >= 0 {
				h.UptimeSecs = ptr(uint64(n))
			}
		}
	}
	if b, err := read("net/dev"); err == nil {
		var rx, tx uint64
		valid := true
		for _, line := range strings.Split(string(b), "\n") {
			name, rest, ok := strings.Cut(line, ":")
			if !ok || strings.TrimSpace(name) == "lo" {
				continue
			}
			f := strings.Fields(rest)
			if len(f) < 9 {
				valid = false
				break
			}
			r, e1 := strconv.ParseUint(f[0], 10, 64)
			t, e2 := strconv.ParseUint(f[8], 10, 64)
			if e1 != nil || e2 != nil {
				valid = false
				break
			}
			rx += r
			tx += t
		}
		if valid {
			h.NetRxBytes, h.NetTxBytes = ptr(rx), ptr(tx)
		}
	}
	h.DiskTotal, h.DiskUsed = diskUsage(disk)
	next := map[string]counter{}
	for name, pid := range pids {
		p := contracts.ProcessSample{ObservedAt: now, State: "unknown"}
		if pid == 0 {
			p.State = "stopped"
			h.Processes[name] = p
			continue
		}
		b, err := read(fmt.Sprintf("%d/stat", pid))
		if err != nil {
			p.Error = "读取进程统计失败：" + err.Error()
			h.Processes[name] = p
			continue
		}
		// comm is parenthesized and may itself contain spaces and ')'.
		end := strings.LastIndexByte(string(b), ')')
		if end < 0 {
			p.Error = "进程统计格式无效"
			h.Processes[name] = p
			continue
		}
		f := strings.Fields(string(b[end+1:]))
		if len(f) < 22 {
			p.Error = "进程统计字段不足"
			h.Processes[name] = p
			continue
		}
		if f[0] == "Z" || f[0] == "X" {
			p.State = "stopped"
			h.Processes[name] = p
			continue
		}
		user, e1 := strconv.ParseUint(f[11], 10, 64)
		sys, e2 := strconv.ParseUint(f[12], 10, 64)
		start, e3 := strconv.ParseUint(f[19], 10, 64)
		rss, e4 := strconv.ParseUint(f[21], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			p.Error = "进程统计数值无效"
			h.Processes[name] = p
			continue
		}
		p.State = "running"
		p.MemoryRSS = ptr(rss * uint64(os.Getpagesize()))
		cur := counter{ticks: user + sys, start: start, pid: pid}
		prev, ok := s.processes[name]
		if validCPU && ok && prev.pid == pid && prev.start == start && cur.ticks >= prev.ticks {
			p.CPUPercent = ptr(float64(cur.ticks-prev.ticks) * 100 / float64(total-s.prevTotal))
		}
		next[name] = cur
		h.Processes[name] = p
	}
	s.processes = next
	s.prevTotal, s.prevBusy, s.prevAt = total, busy, now
	return h
}

// Run samples even when no console is open; cancellation releases its ticker.
func (s *Sampler) Run(ctx context.Context, disk string, pids func() map[string]int, accept func(*contracts.HostSample)) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		accept(s.Sample(disk, pids()))
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
