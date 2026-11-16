package center

import (
	"testing"
	"time"

	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
)

func TestObjectStateMissingStaleRevokedAndFailed(t *testing.T) {
	g := &ent.Gate{Status: gate.StatusActive, ConfigRevision: 1, AppliedRevision: 1}
	if v := gateObjectState(g); v.Status != "unknown" {
		t.Fatalf("missing: %+v", v)
	}
	old := time.Now().Add(-time.Hour)
	g.LastSeenAt = &old
	if v := gateObjectState(g); v.Status != "stale" {
		t.Fatalf("stale: %+v", v)
	}
	now := time.Now()
	g.LastSeenAt = &now
	g.FailedRevision = 1
	if v := gateObjectState(g); v.Status != "failed" {
		t.Fatalf("failed: %+v", v)
	}
	g.Status = gate.StatusRevoked
	if v := gateObjectState(g); v.Status != "revoked" {
		t.Fatalf("revoked: %+v", v)
	}
	u := &ent.User{Enabled: true, Unlimited: true, ExpiresAt: &old}
	if v := userObjectState(u); v.Status != "failed" || v.Label != "已到期" {
		t.Fatalf("expired unlimited: %+v", v)
	}
}

func TestNodeStateRequiresCurrentExternalProtocolEvidence(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name, status, external, udp, want string
		at                                time.Time
	}{
		{"full", "passed", "passed", "passed", "ok", now},
		{"local-only", "passed", "unknown", "passed", "partial", now},
		{"udp-missing", "passed", "passed", "unknown", "partial", now},
		{"tcp-only", "passed", "passed", "not_applicable", "partial", now},
		{"udp-failed", "failed", "passed", "failed", "failed", now},
		{"old-result", "passed", "passed", "passed", "stale", now.Add(-time.Hour)},
		{"config-changed", "unknown", "unknown", "unknown", "unknown", now},
		{"running", "running", "unknown", "unknown", "pending", now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := nodeTestObjectState(&nodeTestResult{Status: tc.status, External: tc.external, At: tc.at, TCP: nodeprobe.Check{Status: "passed"}, UDP: nodeprobe.Check{Status: tc.udp}})
			if v.Status != tc.want || v.Reason == "" || v.ObservedAt == nil {
				t.Fatalf("unexpected state: %+v", v)
			}
		})
	}
}

func TestObjectStateProxyEvidenceCoverage(t *testing.T) {
	now := time.Now()
	g := &ent.Gate{Status: gate.StatusActive, LastSeenAt: &now, ConfigRevision: 2, AppliedRevision: 2}
	e := &ent.Exit{Enabled: true, Kind: "socks5"}
	udp := true
	for _, tc := range []struct {
		name  string
		probe exitProbe
		has   bool
		want  string
	}{
		{"missing", exitProbe{}, false, "unknown"},
		{"stale", exitProbe{OK: true, Mode: "proxy", At: now.Add(-time.Hour), UDP: &udp}, true, "stale"},
		{"failure", exitProbe{At: now}, true, "failed"},
		{"port-only", exitProbe{OK: true, At: now}, true, "partial"},
		{"udp-unknown", exitProbe{OK: true, Mode: "proxy", At: now}, true, "partial"},
		{"complete", exitProbe{OK: true, Mode: "proxy", At: now, UDP: &udp}, true, "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := exitObjectState(e, g, tc.probe, tc.has)
			if v.Status != tc.want || v.Reason == "" || len(v.Next) == 0 {
				t.Fatalf("state: %+v", v)
			}
		})
	}
	g.Status = gate.StatusRevoked
	if v := exitObjectState(e, g, exitProbe{OK: true, At: now, UDP: &udp}, true); v.Status != "revoked" {
		t.Fatalf("revocation must override evidence: %+v", v)
	}
	e.Enabled = false
	if v := exitObjectState(e, g, exitProbe{}, false); v.Status != "disabled" {
		t.Fatalf("disabled: %+v", v)
	}
}

func TestObjectStateSourceCacheIsNotConnectivity(t *testing.T) {
	now, old := time.Now(), time.Now().Add(-48*time.Hour)
	for _, tc := range []struct {
		name   string
		source ent.SubSource
		hours  int
		want   string
	}{
		{"never-fetched", ent.SubSource{Enabled: true}, 0, "unknown"},
		{"failed-without-cache", ent.SubSource{Enabled: true, LastError: "timeout"}, 0, "failed"},
		{"failed-with-cache", ent.SubSource{Enabled: true, NodeCount: 4, FetchedAt: &old, LastError: "timeout"}, 24, "partial"},
		{"fresh-cache", ent.SubSource{Enabled: true, NodeCount: 4, FetchedAt: &now}, 24, "partial"},
		{"overdue-cache", ent.SubSource{Enabled: true, NodeCount: 4, FetchedAt: &old}, 24, "stale"},
		{"manual-refresh", ent.SubSource{Enabled: true, NodeCount: 4, FetchedAt: &old}, 0, "partial"},
		{"empty", ent.SubSource{Enabled: true, FetchedAt: &now}, 24, "warning"},
		{"disabled", ent.SubSource{LastError: "timeout", FetchedAt: &old}, 24, "disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := sourceObjectState(&tc.source, tc.hours)
			if v.Status != tc.want || v.ObservedAt != tc.source.FetchedAt {
				t.Fatalf("state: %+v", v)
			}
		})
	}
}
