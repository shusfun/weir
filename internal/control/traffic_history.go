package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"strconv"
	"time"
)

type trafficSample struct {
	At            time.Time `json:"at"`
	Total         string    `json:"total"`
	Identity      string    `json:"identity,omitempty"`
	Quality       string    `json:"quality"`
	Reason        string    `json:"reason,omitempty"`
	Delta         *string   `json:"delta_bytes"`
	Interval      int64     `json:"interval_seconds"`
	ReportAt      time.Time `json:"report_at"`
	BaselineFresh bool      `json:"baseline_fresh,omitempty"`
}

func trafficDelta(previous, current trafficSample) (*string, int64, string) {
	interval := int64(current.At.Sub(previous.At).Seconds())
	if previous.At.IsZero() {
		interval = 0
	}
	if current.Quality != "fresh" {
		return nil, interval, current.Reason
	}
	if previous.At.IsZero() || !previous.BaselineFresh || interval < 30 || interval > 120 {
		return nil, interval, "sample_gap"
	}
	if previous.Identity != current.Identity {
		return nil, interval, "grant_set_changed"
	}
	before, e1 := strconv.ParseInt(previous.Total, 10, 64)
	after, e2 := strconv.ParseInt(current.Total, 10, 64)
	if e1 != nil || e2 != nil || before < 0 || after < before {
		return nil, interval, "counter_reset"
	}
	value := strconv.FormatInt(after-before, 10)
	return &value, interval, ""
}

// Persist only real accepted grant counters. Missing/old meter reports produce
// an unknown sample; historical gaps are never synthesized as zero traffic.
func (s *Server) sampleTraffic(ctx context.Context, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(69442910)`).Scan(&locked); err != nil || !locked {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT d.id,d.last_seen,d.metadata,COALESCE(sum(g.used),0)::text,COALESCE(string_agg(g.id::text,',' ORDER BY g.id),'') FROM migate.devices d LEFT JOIN migate.grants g ON g.device_id=d.id WHERE d.status='approved' GROUP BY d.id ORDER BY d.id`)
	if err != nil {
		return err
	}
	type point struct {
		id              int
		seen            time.Time
		meta            map[string]any
		total, identity string
	}
	points := []point{}
	for rows.Next() {
		var p point
		var raw []byte
		if err = rows.Scan(&p.id, &p.seen, &raw, &p.total, &p.identity); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &p.meta); err != nil {
			rows.Close()
			return err
		}
		points = append(points, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range points {
		key := fmt.Sprintf("%d:%d", p.id, now.Unix()/60)
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM migate.resources WHERE kind='traffic-sample' AND key=$1)`, key).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		current := trafficSample{At: now.UTC(), Total: p.total, Identity: digest(p.identity), Quality: "unknown", Reason: "meter_unknown"}
		var report []byte
		if err = tx.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE kind='meter-observation' AND key=$1`, strconv.Itoa(p.id)).Scan(&report); err != nil && err != sql.ErrNoRows {
			return err
		}
		var observed struct {
			At time.Time `json:"at"`
		}
		if len(report) > 0 && json.Unmarshal(report, &observed) != nil {
			return fmt.Errorf("invalid meter observation")
		}
		current.ReportAt = observed.At
		if p.seen.After(now.Add(5*time.Second)) || now.Sub(p.seen) > 60*time.Second {
			current.Reason = "device_offline"
		} else if p.meta["traffic_status"] == "fresh" && !observed.At.IsZero() && !observed.At.After(now.Add(5*time.Second)) && now.Sub(observed.At) <= 90*time.Second {
			current.Quality = "fresh"
			current.Reason = ""
		}
		var previous trafficSample
		var raw []byte
		err = tx.QueryRowContext(ctx, `SELECT data FROM migate.resources WHERE kind='traffic-sample' AND owner_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, p.id).Scan(&raw)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if len(raw) > 0 && json.Unmarshal(raw, &previous) != nil {
			return fmt.Errorf("invalid traffic sample")
		}
		current.BaselineFresh = current.Quality == "fresh"
		current.Delta, current.Interval, current.Reason = trafficDelta(previous, current)
		if current.Delta == nil {
			current.Quality = "unknown"
		}
		// A valid baseline still enables a delta after an initial gap. Store
		// baseline quality separately from the derived interval quality.
		data, _ := json.Marshal(current)
		if _, err = tx.ExecContext(ctx, `INSERT INTO migate.resources(kind,key,owner_id,data,created_at) VALUES('traffic-sample',$1,$2,$3,$4) ON CONFLICT(kind,key) DO NOTHING`, key, p.id, data, now); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM migate.resources WHERE kind='traffic-sample' AND created_at<$1`, now.Add(-7*24*time.Hour)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) trafficHistory(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		failure(c, 400, "invalid_device")
		return
	}
	minutes, err := strconv.Atoi(c.DefaultQuery("minutes", "60"))
	if err != nil || (minutes != 60 && minutes != 360 && minutes != 1440) {
		failure(c, 400, "invalid_window")
		return
	}
	if _, err = s.Store.Device.Get(c.Request.Context(), id); err != nil {
		failure(c, 404, "device_not_found")
		return
	}
	now := time.Now().UTC()
	rows, err := s.DB.QueryContext(c.Request.Context(), `SELECT data FROM migate.resources WHERE kind='traffic-sample' AND owner_id=$1 AND created_at>=$2 ORDER BY created_at,id LIMIT 1441`, id, now.Add(-time.Duration(minutes)*time.Minute))
	if err != nil {
		failure(c, 503, "observation_unavailable")
		return
	}
	defer rows.Close()
	items := []trafficSample{}
	for rows.Next() {
		var raw []byte
		var sample trafficSample
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &sample) != nil {
			failure(c, 503, "observation_unavailable")
			return
		}
		sample.Identity = ""
		sample.BaselineFresh = false
		items = append(items, sample)
	}
	if rows.Err() != nil {
		failure(c, 503, "observation_unavailable")
		return
	}
	c.JSON(200, gin.H{"items": items, "observed_at": now, "source": "accepted_grant_settlements", "interval_seconds": 60, "retention_days": 7, "minutes": minutes})
}
