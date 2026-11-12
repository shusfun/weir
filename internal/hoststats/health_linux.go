//go:build linux

package hoststats

import (
	"os"
	"strconv"
	"strings"

	"github.com/shusfun/weir/packages/contracts"
)

func readHealth(procRoot string) *contracts.HealthCheck {
	h := &contracts.HealthCheck{}
	if text, err := os.ReadFile("/proc/sys/net/ipv4/tcp_congestion_control"); err == nil {
		cc := strings.TrimSpace(string(text))
		qdisc := ""
		if q, err := os.ReadFile("/proc/sys/net/core/default_qdisc"); err == nil {
			qdisc = strings.TrimSpace(string(q))
		}
		if qdisc != "" {
			cc = cc + "+" + qdisc
		}
		h.Congestion = &cc
	}
	if text, err := os.ReadFile("/proc/sys/net/core/rmem_max"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(text))); err == nil {
			h.UDPBuffer = &n
		}
	}
	if text, err := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_count"); err == nil {
		count, err1 := strconv.ParseFloat(strings.TrimSpace(string(text)), 64)
		maxText, err2 := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_max")
		max, err3 := strconv.ParseFloat(strings.TrimSpace(string(maxText)), 64)
		if err1 == nil && err2 == nil && err3 == nil && max > 0 {
			free := (max - count) / max
			h.Conntrack = &free
		}
	}
	if text, err := os.ReadFile("/proc/sys/fs/file-nr"); err == nil {
		fields := strings.Fields(string(text))
		if len(fields) >= 3 {
			used, err1 := strconv.ParseFloat(fields[0], 64)
			max, err2 := strconv.ParseFloat(fields[2], 64)
			if err1 == nil && err2 == nil && max > 0 {
				free := (max - used) / max
				h.FileHandles = &free
			}
		}
	}
	if procRoot == "" {
		procRoot = "/proc"
	}
	if text, err := os.ReadFile(procRoot + "/stat"); err == nil {
		for _, line := range strings.Split(string(text), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 9 && fields[0] == "cpu" {
				var total, steal uint64
				ok := true
				for i := 1; i < len(fields) && i <= 8; i++ {
					n, err := strconv.ParseUint(fields[i], 10, 64)
					if err != nil {
						ok = false
						break
					}
					total += n
					if i == 8 {
						steal = n
					}
				}
				if ok && total > 0 {
					pct := float64(steal) * 100 / float64(total)
					h.CPUSteal = &pct
				}
			}
		}
	}
	if ipv6, known := ipv6Usable(procRoot); known {
		h.IPv6 = &ipv6
	}
	if entries, err := os.ReadDir("/sys/class/net"); err == nil {
		for _, e := range entries {
			if e.Name() == "lo" {
				continue
			}
			raw, err := os.ReadFile("/sys/class/net/" + e.Name() + "/mtu")
			if err != nil {
				continue
			}
			if n, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && n > 0 {
				h.MTU = &n
				break
			}
		}
	}
	return h
}
