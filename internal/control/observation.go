package control

import (
	"context"
	"fmt"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/audit"
	"github.com/imzyb/MiGate/ent/device"
	"github.com/imzyb/MiGate/ent/resource"
	"time"
)

func deviceQuality(d *ent.Device, now time.Time) (contact, meter bool) {
	contact = !d.LastSeen.IsZero() && !d.LastSeen.After(now.Add(5*time.Second)) && now.Sub(d.LastSeen) <= 60*time.Second
	meter = contact && d.Metadata["traffic_status"] == "fresh"
	return
}

// Deterministic observations run independently of optional remote classification.
// They report evidence, never automatically issue a task or change permission.
func (s *Server) securityWorker(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.configMu.RLock()
			if s.Config.Installed {
				s.observeSecurity(ctx, time.Now().UTC())
			}
			s.configMu.RUnlock()
		}
	}
}
func (s *Server) observeSecurity(ctx context.Context, now time.Time) {
	_ = s.sampleTraffic(ctx, now)
	_ = s.sampleHosts(ctx, now)
	s.observeCertificates(ctx, now)
	devices, err := s.Store.Device.Query().Where(device.StatusEQ("approved")).All(ctx)
	if err != nil {
		return
	}
	for _, d := range devices {
		contact, meter := deviceQuality(d, now)
		missing := !contact
		data := map[string]any{"device_id": d.ID, "last_seen": d.LastSeen, "observed_at": now.Format(time.RFC3339), "category": "device_contact", "status": "resolved", "severity": "info"}
		if missing {
			data["status"] = "unknown"
			data["severity"] = "warning"
			data["reason"] = "heartbeat_missing_over_60_seconds"
		}
		s.saveSecurityObservation(ctx, fmt.Sprintf("device:%d", d.ID), data, missing)
		meterMissing := !meter
		data = map[string]any{"device_id": d.ID, "observed_at": now.Format(time.RFC3339), "category": "meter_quality", "status": "resolved", "severity": "info"}
		if meterMissing {
			data["status"] = "unknown"
			data["severity"] = "warning"
			data["reason"] = "meter_not_fresh"
		}
		s.saveSecurityObservation(ctx, fmt.Sprintf("meter:%d", d.ID), data, meterMissing)
	}
	denied, err := s.Store.Audit.Query().Where(audit.ActionEQ("auth.login"), audit.ResultEQ("denied"), audit.CreatedAtGTE(now.Add(-time.Minute))).Count(ctx)
	if err != nil {
		return
	}
	data := map[string]any{"category": "login_denials", "count": denied, "window_seconds": 60, "observed_at": now.Format(time.RFC3339), "status": "resolved", "severity": "info"}
	if denied >= 10 {
		data["status"] = "observed"
		data["severity"] = "warning"
		data["reason"] = "denied_login_threshold"
	}
	s.saveSecurityObservation(ctx, "login-denials", data, denied >= 10)
}
func (s *Server) saveSecurityObservation(ctx context.Context, key string, data map[string]any, active bool) {
	r, err := s.Store.Resource.Query().Where(resource.KindEQ("security-event"), resource.KeyEQ(key)).Only(ctx)
	if err != nil {
		if active {
			s.Store.Resource.Create().SetKind("security-event").SetKey(key).SetData(data).Save(ctx)
		}
		return
	}
	if !active && r.Data["status"] == "resolved" {
		return
	}
	s.Store.Resource.UpdateOne(r).SetData(data).Save(ctx)
}
