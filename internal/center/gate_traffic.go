package center

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

const gateTrafficKind = "gate-network-usage"
const gateTrafficConfigKind = "gate-network-settings"

var errGateTrafficRemoved = errors.New("gate_not_found")

// Deletion holds this same lock through commit. Check identity after acquiring
// it so a request authenticated before deletion cannot recreate meter rows.
func (s *Server) withGateTraffic(ctx context.Context, gid int, sample bool, fn func(context.Context, *ent.Client) error) error {
	return s.Store.Lock.Do(ctx, "gate-network:"+itoa(gid), func(ctx context.Context) error {
		tx, err := s.Store.Ent.Tx(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		g, err := tx.Gate.Get(ctx, gid)
		if ent.IsNotFound(err) || err == nil && sample && g.Status != gate.StatusActive {
			return errGateTrafficRemoved
		}
		if err != nil {
			return err
		}
		if err := fn(ctx, tx.Client()); err != nil {
			return err
		}
		return tx.Commit()
	})
}

type gateTrafficSettings struct {
	Quota             uint64    `json:"quota"`
	Mode              string    `json:"mode"`
	ResetDay          int       `json:"reset_day"`
	AutoDeprioritize  bool      `json:"auto_deprioritize"`
	Calibration       *uint64   `json:"calibration,omitempty"`
	CalibrationBase   uint64    `json:"calibration_base,string"`
	CalibrationPeriod time.Time `json:"calibration_period"`
}

type gateNetworkUsage struct {
	Start      time.Time `json:"start"`
	Next       time.Time `json:"next"`
	ObservedAt time.Time `json:"observed_at"`
	BootID     string    `json:"boot_id"`
	RX         uint64    `json:"rx,string"`
	TX         uint64    `json:"tx,string"`
	TotalRX    uint64    `json:"total_rx,string"`
	TotalTX    uint64    `json:"total_tx,string"`
	Partial    bool      `json:"partial"`
}

// A reset day beyond the month's last day is clamped, without letting
// time.Date roll February 31 into March. Billing boundaries use UTC.
func networkPeriod(at time.Time, day int) (time.Time, time.Time) {
	at = at.UTC()
	boundary := func(year int, month time.Month) time.Time {
		last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
		return time.Date(year, month, min(day, last), 0, 0, 0, 0, time.UTC)
	}
	start := boundary(at.Year(), at.Month())
	if at.Before(start) {
		start = boundary(at.Year(), at.Month()-1)
	}
	return start, boundary(start.Year(), start.Month()+1)
}

func readGateTraffic(ctx context.Context, db *ent.Client, gid int) (gateTrafficSettings, gateNetworkUsage, error) {
	settings := gateTrafficSettings{Mode: "sum", ResetDay: 1}
	var usage gateNetworkUsage
	for _, spec := range []struct {
		kind string
		dest any
	}{{gateTrafficConfigKind, &settings}, {gateTrafficKind, &usage}} {
		row, err := db.Resource.Query().Where(resource.KindEQ(spec.kind), resource.KeyEQ(itoa(gid))).Only(ctx)
		if ent.IsNotFound(err) {
			continue
		}
		if err != nil {
			return settings, usage, err
		}
		if err = decodeResource(row, spec.dest); err != nil {
			return settings, usage, err
		}
	}
	return settings, usage, nil
}

func advanceNetworkUsage(v gateNetworkUsage, h *contracts.HostSample, day int) (gateNetworkUsage, error) {
	if !v.ObservedAt.IsZero() && !h.ObservedAt.After(v.ObservedAt) {
		return v, nil
	}
	start, next := networkPeriod(h.ObservedAt, day)
	rx, tx := *h.NetRxBytes, *h.NetTxBytes
	if v.Start != start {
		// A sample spanning the reset boundary cannot be split accurately.
		// Start a new baseline and expose the gap, rather than move bytes
		// from the preceding billing period into the new one.
		v = gateNetworkUsage{Start: start, Next: next, Partial: true}
	}
	if !v.ObservedAt.IsZero() {
		drx, dtx := uint64(0), uint64(0)
		if h.BootID != "" && v.BootID != "" && h.BootID != v.BootID {
			drx, dtx = rx, tx
			v.Partial = true
		} else if rx >= v.RX && tx >= v.TX {
			drx, dtx = rx-v.RX, tx-v.TX
		} else {
			// Interface replacement/reset is not proof of a reboot.
			v.Partial = true
		}
		if math.MaxUint64-v.TotalRX < drx || math.MaxUint64-v.TotalTX < dtx {
			return v, errors.New("network_counter_overflow")
		}
		v.TotalRX += drx
		v.TotalTX += dtx
	}
	v.RX, v.TX, v.BootID, v.ObservedAt = rx, tx, h.BootID, h.ObservedAt.UTC()
	return v, nil
}

func networkBytes(v gateNetworkUsage, mode string) uint64 {
	switch mode {
	case "outbound":
		return v.TotalTX
	case "max":
		return max(v.TotalRX, v.TotalTX)
	default:
		if math.MaxUint64-v.TotalRX < v.TotalTX {
			return math.MaxUint64
		}
		return v.TotalRX + v.TotalTX
	}
}

func billedNetworkBytes(s gateTrafficSettings, v gateNetworkUsage) uint64 {
	used := networkBytes(v, s.Mode)
	if s.Calibration != nil && s.CalibrationPeriod.Equal(v.Start) {
		delta := uint64(0)
		if used > s.CalibrationBase {
			delta = used - s.CalibrationBase
		}
		if math.MaxUint64-*s.Calibration < delta {
			return math.MaxUint64
		}
		return *s.Calibration + delta
	}
	return used
}

func (s *Server) recordGateNetwork(ctx context.Context, gid int, h *contracts.HostSample, now time.Time) (err error) {
	if h == nil || h.NetRxBytes == nil || h.NetTxBytes == nil || !validHeartbeatHost(h, now) {
		return nil
	}
	finish := operationStage(ctx, "traffic.host.sample", "gate_id", gid)
	defer func() { finish(err) }()
	err = s.withGateTraffic(ctx, gid, true, func(ctx context.Context, db *ent.Client) error {
		settings, usage, err := readGateTraffic(ctx, db, gid)
		if err != nil {
			return err
		}
		if !usage.ObservedAt.IsZero() && !h.ObservedAt.After(usage.ObservedAt) {
			return nil
		}
		usage, err = advanceNetworkUsage(usage, h, settings.ResetDay)
		if err != nil {
			return err
		}
		return db.Resource.Create().SetKind(gateTrafficKind).SetKey(itoa(gid)).SetOwnerID(gid).SetData(toMap(usage)).OnConflictColumns(resource.FieldKind, resource.FieldKey).UpdateNewValues().Exec(ctx)
	})
	// An already authenticated heartbeat may have been waiting during revoke or
	// delete. Discard its late sample; the next authentication returns revocation.
	if errors.Is(err, errGateTrafficRemoved) {
		return nil
	}
	return err
}

func gateTrafficView(settings gateTrafficSettings, usage gateNetworkUsage, now time.Time) gin.H {
	start, next := networkPeriod(now, settings.ResetDay)
	state := objectState{Status: "unknown", Reason: "尚无本周期网卡计数", Next: []string{"configure"}}
	var used *uint64
	if usage.Start.Equal(start) && !usage.ObservedAt.IsZero() {
		n := billedNetworkBytes(settings, usage)
		used = &n
		state.ObservedAt = &usage.ObservedAt
		state.Status, state.Reason = "partial", "已观测网卡流量；启用前、重启前尾段或跨重置边界的流量可能缺失，可按供应商账单校准"
		if now.Sub(usage.ObservedAt) > 90*time.Second {
			state.Status, state.Reason = "stale", "网卡观测已过期，显示最后累计值"
		}
	}
	level := networkAlert(settings, usage, now)
	return gin.H{"settings": settings, "revision": version(settings), "meter_revision": version(usage), "used": used, "period_start": start, "period_end": next, "timezone": "UTC", "state": state, "alert": level, "auto_deprioritized": settings.AutoDeprioritize && level == "critical"}
}

func networkAlert(settings gateTrafficSettings, usage gateNetworkUsage, now time.Time) string {
	start, _ := networkPeriod(now, settings.ResetDay)
	if usage.ObservedAt.IsZero() || !usage.Start.Equal(start) {
		return "unknown"
	}
	used := billedNetworkBytes(settings, usage)
	level := "none"
	if settings.Quota > 0 {
		ratio := float64(used) / float64(settings.Quota)
		if ratio >= .95 {
			level = "critical"
		} else if ratio >= .8 {
			level = "warning"
		}
	}
	return level
}

func (s *Server) getGateTraffic(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	settings, usage, err := readGateTraffic(c.Request.Context(), s.Store.Ent, g.ID)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	c.JSON(200, gateTrafficView(settings, usage, time.Now()))
}

func (s *Server) putGateTraffic(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	var req struct {
		Quota            uint64  `json:"quota"`
		Mode             string  `json:"mode"`
		ResetDay         int     `json:"reset_day"`
		AutoDeprioritize bool    `json:"auto_deprioritize"`
		Calibration      *uint64 `json:"calibration"`
		Revision         string  `json:"revision"`
		MeterRevision    string  `json:"meter_revision"`
	}
	if !bind(c, &req) {
		return
	}
	if req.ResetDay < 1 || req.ResetDay > 31 || req.Mode != "sum" && req.Mode != "outbound" && req.Mode != "max" || req.Quota > 1<<53 || req.Calibration != nil && *req.Calibration > 1<<53 {
		failure(c, 400, "invalid_traffic_settings")
		return
	}
	ctx := c.Request.Context()
	finish := operationStage(ctx, "traffic.host.configure", "gate_id", g.ID)
	var view gin.H
	err := s.withGateTraffic(ctx, g.ID, false, func(ctx context.Context, db *ent.Client) error {
		settings, usage, err := readGateTraffic(ctx, db, g.ID)
		if err != nil {
			return err
		}
		if req.Revision != version(settings) {
			return errors.New("traffic_settings_changed")
		}
		if req.Calibration != nil && (req.MeterRevision != version(usage) || usage.ObservedAt.IsZero() || !usage.Start.Equal(func() time.Time { start, _ := networkPeriod(time.Now(), req.ResetDay); return start }())) {
			return errors.New("traffic_sample_changed")
		}
		if req.Mode != settings.Mode || req.ResetDay != settings.ResetDay {
			settings.Calibration = nil
		}
		settings.Mode, settings.Quota, settings.ResetDay = req.Mode, req.Quota, req.ResetDay
		settings.AutoDeprioritize = req.AutoDeprioritize
		if req.Calibration != nil {
			settings.Calibration, settings.CalibrationBase, settings.CalibrationPeriod = req.Calibration, networkBytes(usage, settings.Mode), usage.Start
		}
		if err = db.Resource.Create().SetKind(gateTrafficConfigKind).SetKey(itoa(g.ID)).SetOwnerID(g.ID).SetData(toMap(settings)).OnConflictColumns(resource.FieldKind, resource.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
			return err
		}
		view = gateTrafficView(settings, usage, time.Now())
		return nil
	})
	finish(err)
	if errors.Is(err, errGateTrafficRemoved) {
		failure(c, 404, "gate_not_found")
		return
	}
	if err != nil {
		failDetail(c, 409, "traffic_save_failed", err.Error(), nil)
		return
	}
	s.audit(c, "gate.traffic.configure", itoa(g.ID), "ok")
	c.JSON(200, view)
}
