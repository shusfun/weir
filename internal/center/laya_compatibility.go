package center

import (
	"context"
	"errors"
	"time"
)

func (s *Server) gateLayaVersion(ctx context.Context, gateID int) (string, error) {
	resources, err := s.workerResources(ctx, gateID)
	if err != nil {
		return "", err
	}
	if resources != nil {
		observation, ok := resources.Resources.Components["laya"]
		if ok && freshObservation(observation.ObservedAt, time.Now(), 2*time.Minute) && observation.Version != "" {
			return observation.Version, nil
		}
	}
	return "", errors.New("laya_runtime_version_unknown")
}

func (s *Server) currentLayaCompatibility(ctx context.Context, configured layaSettings) layaCompatibilityStatus {
	profile, err := s.aiProfile(ctx)
	if err != nil {
		return layaCompatibilityStatus{Code: "configuration_unavailable", Reason: "兼容档案不可用；后台分析已暂停"}
	}
	var version string
	if configured.Mode == "gate" {
		version, _ = s.gateLayaVersion(ctx, configured.GateID)
	} else if _, revision, err := s.readLayaSettings(ctx); err == nil {
		s.layaHealth.RLock()
		if s.layaHealth.revision == revision && freshObservation(s.layaHealth.model.ObservedAt, time.Now(), 2*time.Minute) {
			version = s.layaHealth.model.Version
		}
		s.layaHealth.RUnlock()
	}
	return resolveLayaCompatibility(*profile.Effective.LayaCompatibility, version)
}
