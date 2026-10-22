package gate

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imzyb/MiGate/packages/contracts"
)

var cpuPrev struct {
	sync.Mutex
	busy, total uint64
	at          time.Time
}

func u64(v uint64) *uint64   { return &v }
func f64(v float64) *float64 { return &v }

// sampleHost 只读取固定的 /proc 文件与数据盘用量；读不到的字段留空，中心显示为「未知」。
// CPU 使用率取相邻两次有效采样的差值；首次、计数回退或间隔超过 60 秒时为未知。
func sampleHost() *contracts.HostSample {
	s := &contracts.HostSample{ObservedAt: time.Now().UTC()}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if v, err := strconv.ParseFloat(f[0], 64); err == nil {
				s.Load1 = f64(v)
			}
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		mem := map[string]uint64{}
		for _, line := range strings.Split(string(b), "\n") {
			if f := strings.Fields(line); len(f) >= 2 {
				v, _ := strconv.ParseUint(f[1], 10, 64)
				mem[strings.TrimSuffix(f[0], ":")] = v * 1024
			}
		}
		if t, a := mem["MemTotal"], mem["MemAvailable"]; t > 0 && a <= t {
			s.MemTotal, s.MemUsed = u64(t), u64(t-a)
		}
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if v, err := strconv.ParseFloat(f[0], 64); err == nil {
				s.UptimeSecs = u64(uint64(v))
			}
		}
	}
	if f, err := os.Open("/proc/net/dev"); err == nil {
		var rx, tx uint64
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			name, rest, ok := strings.Cut(sc.Text(), ":")
			if !ok || strings.TrimSpace(name) == "lo" {
				continue
			}
			if v := strings.Fields(rest); len(v) >= 9 {
				r, _ := strconv.ParseUint(v[0], 10, 64)
				t, _ := strconv.ParseUint(v[8], 10, 64)
				rx, tx = rx+r, tx+t
			}
		}
		f.Close()
		s.NetRxBytes, s.NetTxBytes = u64(rx), u64(tx)
	}
	if b, err := os.ReadFile("/proc/stat"); err == nil {
		line, _, _ := strings.Cut(string(b), "\n")
		if f := strings.Fields(line); len(f) >= 8 && f[0] == "cpu" {
			var vals []uint64
			for _, x := range f[1:] {
				v, _ := strconv.ParseUint(x, 10, 64)
				vals = append(vals, v)
			}
			var total uint64
			for _, v := range vals {
				total += v
			}
			idle := vals[3] + vals[4]
			if len(vals) > 8 { // guest 已计入 user，不重复计
				total -= vals[8]
			}
			busy := total - idle
			cpuPrev.Lock()
			if !cpuPrev.at.IsZero() && time.Since(cpuPrev.at) <= time.Minute && total > cpuPrev.total && busy >= cpuPrev.busy {
				s.CPUPercent = f64(float64(busy-cpuPrev.busy) * 100 / float64(total-cpuPrev.total))
			}
			cpuPrev.busy, cpuPrev.total, cpuPrev.at = busy, total, time.Now()
			cpuPrev.Unlock()
		}
	}
	s.DiskTotal, s.DiskUsed = diskUsage("/")
	return s
}
