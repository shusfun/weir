package gate

import (
	"github.com/shusfun/weir/packages/contracts"
	"testing"
	"time"
)

func TestIdleLiveUsersOnlySuccessfulKernel(t *testing.T) {
	now := time.Now()
	expired := now.Add(-time.Minute)
	cfg := contracts.GateConfig{Inbounds: []contracts.Inbound{{ID: 1, Kernel: "xray"}, {ID: 2, Kernel: "sing-box"}}, Credentials: []contracts.Credential{{InboundID: 1, StatsKey: "idle"}, {InboundID: 1, StatsKey: "seen"}, {InboundID: 2, StatsKey: "other-kernel"}, {InboundID: 1, StatsKey: "expired", ExpiresAt: &expired}}}
	got := idleLiveUsers(cfg, "xray", map[string]bool{"seen": true}, now)
	if len(got) != 1 || got[0].StatsKey != "idle" || got[0].Up != 0 || got[0].Down != 0 {
		t.Fatalf("空闲=%+v", got)
	}
}
