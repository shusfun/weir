package center

import (
	"github.com/shusfun/weir/packages/contracts"
	"testing"
	"time"
)

// 稳定性：周期维护任务（额度分配与回收、凭据清理、证书续签、全量同步、订阅源刷新等）不得改变内核配置。
// 配置版本不变，Gate 就不会重新应用、不会重启内核。
func TestMaintenanceTasksKeepKernelConfig(t *testing.T) {
	s, admin := newInstalledServer(t)
	if _, err := s.kernelCache().Put("xray", "26.3.27", "amd64", []byte("xray-binary")); err != nil {
		t.Fatal(err)
	}
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{})
	mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": int(g.id), "preset": "vless-reality", "skip_probe": true}, 200)
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "quota@example.test", "quota": 1 << 30}, 200)
	before := g.config(0)
	ctx, now := t.Context(), time.Now()
	for name, run := range map[string]func() error{
		"reset-usage":   func() error { return s.ResetDueUsage(ctx, now) },
		"grant-sync":    func() error { return s.ReconcileAccountGrants(ctx) },
		"probe-creds":   func() error { return s.cleanupProbeCredentials(ctx, now, false) },
		"reclaim":       func() error { return s.ReclaimStaleBudgets(ctx, now.Add(time.Hour)) },
		"renew-certs":   func() error { return s.RenewCerts(ctx, now) },
		"admins-nodes":  func() error { return s.EnsureAdminsAllNodes(ctx) },
		"offline-check": func() error { return s.CheckpointOfflineGates(ctx, now.Add(time.Hour)) },
		"sync-all":      func() error { return s.SyncAll(ctx) },
	} {
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s.RefreshSourceExits(ctx)
		if after := g.config(0); after.Revision != before.Revision {
			t.Fatalf("%s advanced the kernel config revision %d -> %d", name, before.Revision, after.Revision)
		}
	}
}
