package gate

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/shusfun/weir/internal/hoststats"
	"github.com/shusfun/weir/packages/contracts"
)

var hostSampler hoststats.Sampler

func u64(v uint64) *uint64 { return &v }

// netCounters 是除回环外全部网卡的收发累计；读不到时为空。
func netCounters() (*uint64, *uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil, nil
	}
	defer f.Close()
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
	return u64(rx), u64(tx)
}

// The fallback supports older helpers without process observations.
func sampleHost() *contracts.HostSample {
	hostSampler.RefreshDNS(context.Background())
	return hostSampler.Sample("/", nil)
}
