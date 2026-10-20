package xray

import (
	"context"
	"os"
	"testing"
)

func TestLiveSingboxRaw(t *testing.T) {
	if os.Getenv("MIGATE_DUAL_LIVE") != "1" {
		t.Skip("isolated kernels only")
	}
	c, err := NewGRPCStatsClientWithEngineAndService(context.Background(), "127.0.0.1:19817", "singbox", "v2ray.core.app.stats.command.StatsService")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	values, err := c.queryStats(context.Background(), "", false)
	t.Logf("isolated raw counters: %+v error=%v", values, err)
	if err != nil || len(values) == 0 {
		t.Fatal("no actual counters")
	}
}
