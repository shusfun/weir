package center

import (
	"testing"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/store/ent"
)

func TestGateUpdatePausePreservesManualTarget(t *testing.T) {
	s := &Server{Config: Config{GateUpdatesPaused: true}}
	g := &ent.Gate{ID: 1, Arch: "amd64"}
	if target := s.heartbeatRelease(t.Context(), g, "older"); target != nil {
		t.Fatal("paused rollout sent an automatic update")
	}
	if target := s.releaseTarget(t.Context(), g.Arch, false); target.Version != buildinfo.Version {
		t.Fatal("paused rollout blocked the explicit update target")
	}
	s.Config.GateUpdatesPaused = false
	if target := s.heartbeatRelease(t.Context(), g, "older"); target == nil || target.Version != buildinfo.Version {
		t.Fatal("resumed rollout did not follow the center")
	}
}
