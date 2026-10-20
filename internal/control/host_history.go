package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/device"
)

type hostSample struct {
	At          time.Time `json:"at"`
	ObservedAt  string    `json:"observed_at"`
	Quality     string    `json:"quality"`
	CPU         *float64  `json:"cpu_percent"`
	CPUInterval *float64  `json:"cpu_interval_seconds"`
	Memory      *float64  `json:"memory_percent"`
	Disk        *float64  `json:"disk_used_percent"`
	Load        *float64  `json:"load_1m"`
}

func hostHistorySample(d *ent.Device, now time.Time) hostSample {
	p := hostSample{At: now.UTC(), Quality: "unknown"}
	if contact, _ := deviceQuality(d, now); !contact {
		return p
	}
	h := sanitizeHostObservation(d.Metadata["host_status"], now)
	at, e := time.Parse(time.RFC3339, h.ObservedAt)
	if e != nil || at.After(now.Add(5*time.Second)) || now.Sub(at) > 60*time.Second || h.Validity == "unknown" {
		return p
	}
	p.ObservedAt = h.ObservedAt
	p.CPU = h.CPUPercent
	p.CPUInterval = h.CPUInterval
	if h.MemoryTotal != nil && h.MemoryAvailable != nil {
		n := 100 * float64(*h.MemoryTotal-*h.MemoryAvailable) / float64(*h.MemoryTotal)
		p.Memory = &n
	}
	if h.DiskTotal != nil && h.DiskAvailable != nil {
		n := 100 * float64(*h.DiskTotal-*h.DiskAvailable) / float64(*h.DiskTotal)
		p.Disk = &n
	}
	if len(h.Load) == 3 {
		n := h.Load[0]
		p.Load = &n
	}
	if p.CPU != nil || p.Memory != nil || p.Disk != nil || p.Load != nil {
		p.Quality = "partial"
	}
	if p.CPU != nil && p.Memory != nil && p.Disk != nil && p.Load != nil {
		p.Quality = "available"
	}
	return p
}

// One actual observation per device/minute. Never synthesize old history from
// the current sample; stale or missing telemetry is recorded as unknown.
func (s *Server) sampleHosts(ctx context.Context, now time.Time) error {
	ds, e := s.Store.Device.Query().Where(device.StatusEQ("approved")).All(ctx)
	if e != nil {
		return e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, d := range ds {
		raw, _ := json.Marshal(hostHistorySample(d, now))
		key := fmt.Sprintf("%d:%d", d.ID, now.Unix()/60)
		if _, e = tx.ExecContext(ctx, `INSERT INTO migate.resources(kind,key,owner_id,data,created_at) VALUES('host-sample',$1,$2,$3,$4) ON CONFLICT(kind,key) DO NOTHING`, key, d.ID, raw, now); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM migate.resources WHERE kind='host-sample' AND created_at<$1`, now.Add(-7*24*time.Hour)); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Server) hostHistory(c *gin.Context) {
	id, e := strconv.Atoi(c.Param("id"))
	if e != nil || id <= 0 {
		failure(c, 400, "invalid_device")
		return
	}
	minutes, e := strconv.Atoi(c.DefaultQuery("minutes", "60"))
	if e != nil || (minutes != 60 && minutes != 360 && minutes != 1440) {
		failure(c, 400, "invalid_window")
		return
	}
	if _, e = s.Store.Device.Get(c.Request.Context(), id); e != nil {
		if ent.IsNotFound(e) {
			failure(c, 404, "device_not_found")
		} else {
			failure(c, 503, "observation_unavailable")
		}
		return
	}
	now := time.Now().UTC()
	rows, e := s.DB.QueryContext(c.Request.Context(), `SELECT data FROM migate.resources WHERE kind='host-sample' AND owner_id=$1 AND created_at>=$2 ORDER BY created_at,id LIMIT 1441`, id, now.Add(-time.Duration(minutes)*time.Minute))
	if e != nil {
		failure(c, 503, "observation_unavailable")
		return
	}
	defer rows.Close()
	items := []hostSample{}
	for rows.Next() {
		var raw []byte
		var p hostSample
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &p) != nil {
			failure(c, 503, "observation_unavailable")
			return
		}
		items = append(items, p)
	}
	if rows.Err() != nil {
		failure(c, 503, "observation_unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"items": items, "observed_at": now, "source": "accepted_linux_host_observations", "interval_seconds": 60, "retention_days": 7, "minutes": minutes})
}
