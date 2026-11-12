package center

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/packages/contracts"
)

func validHeartbeatV2(h contracts.Heartbeat) bool {
	digest, err := hex.DecodeString(h.Digest)
	if h.Protocol != contracts.HeartbeatProtocol || err != nil || len(digest) != 32 || strings.ToLower(h.Digest) != h.Digest {
		return false
	}
	if h.Host != nil && !validHeartbeatHost(h.Host, time.Now()) {
		return false
	}
	if !validKernelEvents(h.KernelEvents) {
		return false
	}
	if !h.Full {
		copy := h
		copy.Protocol, copy.Digest, copy.Host, copy.KernelEvents = 0, "", nil, nil
		return jsonText(copy) == jsonText(contracts.Heartbeat{})
	}
	if len(h.Version) == 0 || len(h.Version) > 100 || len(h.Hostname) > 255 || (h.Arch != "amd64" && h.Arch != "arm64") || len(h.Center) > 255 || len(h.ListeningPorts) > 4096 || len(h.Kernels) > 4 || len(h.PanelCerts) > 200 {
		return false
	}
	for _, p := range h.ListeningPorts {
		if p.Port < 1 || p.Port > 65535 || (p.Proto != "tcp" && p.Proto != "udp") || len(p.Process) > 255 {
			return false
		}
	}
	for name, version := range h.Kernels {
		if (name != "xray" && name != "sing-box") || len(version) > 100 {
			return false
		}
	}
	if h.SelfUpdate != nil && !validSelfUpdate(h.SelfUpdate) {
		return false
	}
	if h.Firewall != nil && (len(h.Firewall.Rules) > 500 || len(h.Firewall.Error) > 2000 || len(h.Firewall.Tool) > 100) {
		return false
	}
	if h.Panel != nil && (len(h.Panel.Error) > 2000 || len(h.Panel.Version) > 100 || len(h.Panel.Kind) > 100) {
		return false
	}
	actual, err := contracts.HeartbeatDigest(h)
	return err == nil && actual == h.Digest
}

func validHeartbeatHost(h *contracts.HostSample, now time.Time) bool {
	if h.ObservedAt.IsZero() || now.Sub(h.ObservedAt) > 90*time.Second || h.ObservedAt.After(now.Add(time.Minute)) || len(h.Processes) > 32 || len(h.ObservationID) > 32 {
		return false
	}
	finite := func(v *float64, max float64) bool {
		return v == nil || !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 && *v <= max
	}
	if !finite(h.CPUPercent, 100) || !finite(h.Load1, 100000) {
		return false
	}
	for _, v := range []*uint64{h.MemTotal, h.MemUsed, h.DiskTotal, h.DiskUsed} {
		if v != nil && *v > math.MaxInt64 {
			return false
		}
	}
	if h.MemUsed != nil && h.MemTotal != nil && *h.MemUsed > *h.MemTotal {
		return false
	}
	if h.DiskUsed != nil && h.DiskTotal != nil && *h.DiskUsed > *h.DiskTotal {
		return false
	}
	for name, p := range h.Processes {
		if len(name) > 100 || len(p.Error) > 2000 || !finite(p.CPUPercent, 100) {
			return false
		}
	}
	return true
}

func withoutObservationTime(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := map[string]any{}
	for k, v := range in {
		if k != "at" {
			out[k] = v
		}
	}
	return out
}

func firewallFields(f *contracts.FirewallStatus) map[string]any {
	if f == nil {
		return nil
	}
	rules := append([]string(nil), f.Rules...)
	sort.Strings(rules)
	return map[string]any{"tool": f.Tool, "active": f.Active, "rules": rules, "error": f.Error}
}

// saveHeartbeatSnapshot updates only changed business observations. Host
// sampling and liveness are independent and never cause a Gate row update.
func (s *Server) saveHeartbeatSnapshot(ctx context.Context, g *ent.Gate, h contracts.Heartbeat, now time.Time) (*ent.Gate, bool, error) {
	h.ListeningPorts = append([]contracts.ListeningPort(nil), h.ListeningPorts...)
	sort.Slice(h.ListeningPorts, func(i, j int) bool {
		a, b := h.ListeningPorts[i], h.ListeningPorts[j]
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.Proto != b.Proto {
			return a.Proto < b.Proto
		}
		return a.Process < b.Process
	})
	ports := make([]map[string]any, 0, len(h.ListeningPorts))
	for _, p := range h.ListeningPorts {
		ports = append(ports, map[string]any{"port": p.Port, "proto": p.Proto, "process": p.Process})
	}
	kernels := h.Kernels
	if kernels == nil {
		kernels = map[string]string{}
	}
	upd := g.Update()
	changed := false
	if g.Version != h.Version {
		upd.SetVersion(h.Version)
		changed = true
	}
	if g.Hostname != h.Hostname {
		upd.SetHostname(h.Hostname)
		changed = true
	}
	if g.Arch != h.Arch {
		upd.SetArch(h.Arch)
		changed = true
	}
	if g.CenterURL != h.Center {
		upd.SetCenterURL(h.Center)
		changed = true
	}
	observationsChanged := jsonText(g.ListeningPorts) != jsonText(ports) || jsonText(g.KernelsRunning) != jsonText(kernels)
	if observationsChanged {
		upd.SetListeningPorts(ports).SetKernelsRunning(kernels)
		changed = true
	}
	if g.LastSeenAt == nil || g.Version == "" {
		upd.SetLastSeenAt(now)
		changed = true
	}
	var su map[string]any
	if h.SelfUpdate != nil {
		su = selfUpdateFields(h.SelfUpdate)
	}
	if jsonText(withoutObservationTime(g.SelfUpdate)) != jsonText(withoutObservationTime(su)) {
		if su == nil {
			upd.ClearSelfUpdate()
		} else {
			upd.SetSelfUpdate(su)
		}
		changed = true
	}
	fw := firewallFields(h.Firewall)
	if jsonText(withoutObservationTime(g.Firewall)) != jsonText(fw) {
		if fw == nil {
			upd.ClearFirewall()
		} else {
			fw["at"] = now
			upd.SetFirewall(fw)
		}
		changed = true
	}
	// This rollout field remains available to old node-test callers. Resource
	// samples are supplied from transient state by the store read adapter.
	if jsonText(g.Host["node_test_version"]) != jsonText(h.NodeTestVersion) {
		upd.SetHost(map[string]any{"node_test_version": h.NodeTestVersion})
		changed = true
	}
	if !changed {
		return g, observationsChanged, nil
	}
	updated, err := upd.Save(ctx)
	return updated, observationsChanged, err
}

func (s *Server) agentHeartbeatV2(c *gin.Context, h contracts.Heartbeat) {
	if !validHeartbeatV2(h) {
		failure(c, 400, "invalid_heartbeat")
		return
	}
	g, ctx := gateFrom(c), c.Request.Context()
	if err := s.appendKernelEvents(ctx, g.ID, g.ConfigRevision, "", h.KernelEvents); err != nil {
		failInternal(c, 503, "save_failed", err)
		return
	}
	now := time.Now().UTC()
	reply := contracts.HeartbeatReply{HeartbeatProtocol: contracts.HeartbeatProtocol, ConfigRevision: g.ConfigRevision}
	err := s.Store.Lock.Do(ctx, "gate-heartbeat:"+itoa(g.ID), func(ctx context.Context) error {
		observed, err := s.Store.GateObservation(ctx, g.ID)
		if err != nil {
			return err
		}
		if observed == nil {
			observed = &store.GateObservation{}
		}
		if !h.Full && observed.Digest != h.Digest {
			reply.NeedFull = true
			// Receipt proves liveness, but does not acknowledge a missing snapshot.
			observed.SeenAt = now
			return s.Store.SetGateObservation(ctx, g.ID, *observed)
		}
		first := g.LastSeenAt == nil
		if h.Full {
			g, err = s.Store.Ent.Gate.Get(ctx, g.ID)
			if err != nil {
				return err
			}
			var changed bool
			g, changed, err = s.saveHeartbeatSnapshot(ctx, g, h, now)
			if err != nil {
				return err
			}
			if g.PublicHost == "" {
				if ip := publicAddress(c.ClientIP()); ip != "" {
					g, err = g.Update().SetPublicHost(ip).Save(ctx)
					if err != nil {
						return err
					}
				}
			}
			s.absorbPanelReport(ctx, g, h)
			if changed && g.AppliedRevision >= g.ConfigRevision {
				s.refreshGateReachability(g.ID)
			}
			before := g.ConfigRevision
			g, err = s.ensureStatsPorts(ctx, g)
			if err != nil {
				return err
			}
			if g.ConfigRevision != before {
				s.waiters.notify(g.ID)
				_ = s.Store.Notifications.Publish(ctx, "gate:"+itoa(g.ID))
			}
			observed.Digest = h.Digest
		}
		if h.Host != nil {
			b, err := json.Marshal(h.Host)
			if err != nil {
				return err
			}
			var host map[string]any
			if err = json.Unmarshal(b, &host); err != nil {
				return err
			}
			host["node_test_version"] = g.Host["node_test_version"]
			observed.Host = host
			c.Set("observation_id", h.Host.ObservationID)
			s.recordHost(ctx, g.ID, h.Host, now)
		}
		observed.SeenAt = now
		if err = s.Store.SetGateObservation(ctx, g.ID, *observed); err != nil {
			return err
		}
		if first {
			s.notifyConsole("gate", itoa(g.ID))
		}
		reply.Digest = h.Digest
		reply.ConfigRevision = g.ConfigRevision
		return nil
	})
	if err != nil {
		failInternal(c, 503, "heartbeat_unavailable", err)
		return
	}
	reply.Center = s.centerOrigin(ctx)
	reply.Release = s.heartbeatRelease(ctx, g, g.Version)
	c.JSON(200, reply)
}

// CheckpointOfflineGates writes the final observed time once after a Gate goes
// offline, rather than rewriting its business row on every heartbeat.
func (s *Server) CheckpointOfflineGates(ctx context.Context, now time.Time) error {
	gates, err := s.Store.Ent.Gate.Query().Where(gate.StatusEQ(gate.StatusActive)).All(ctx)
	if err != nil {
		return err
	}
	for _, g := range gates {
		observed, err := s.Store.GateObservation(ctx, g.ID)
		if err != nil {
			return err
		}
		if observed == nil || now.Sub(observed.SeenAt) < 90*time.Second {
			continue
		}
		if _, err = s.Store.Ent.Gate.Update().Where(gate.IDEQ(g.ID), gate.Or(gate.LastSeenAtIsNil(), gate.LastSeenAtLT(observed.SeenAt))).SetLastSeenAt(observed.SeenAt).Save(ctx); err != nil {
			return err
		}
		s.triggerOps(ctx, g.ID, "gate.offline", "VPS 超过 90 秒没有心跳，最后一次在 "+observed.SeenAt.UTC().Format(time.RFC3339))
	}
	return nil
}
