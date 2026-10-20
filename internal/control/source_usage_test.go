package control

import (
	"github.com/imzyb/MiGate/ent"
	"testing"
	"time"
)

func TestMergedSourceUsage(t *testing.T) {
	now := time.Now().UTC()
	row := func(id int, total, upload, download, expire string) *ent.Resource {
		return &ent.Resource{ID: id, Data: map[string]any{"enabled": true, "last_update": now.Format(time.RFC3339), "upstream_usage": map[string]any{"status": "available", "total": total, "upload": upload, "download": download, "expire": expire}}}
	}
	a := row(1, "9223372036854775807", "9007199254740993", "1", "500")
	b := row(2, "9223372036854775807", "2", "3", "300")
	r := mergedSourceUsage([]*ent.Resource{a, b, a}, now)
	if r.Status != "available" || r.Included != 2 || r.Total == nil || *r.Total != "18446744073709551614" || *r.Upload != "9007199254740995" || *r.Expire != "300" {
		t.Fatalf("exact aggregate invalid: %+v", r)
	}
	b.Data["exclude_traffic"] = true
	r = mergedSourceUsage([]*ent.Resource{a, b}, now)
	if r.Excluded != 1 || r.Included != 1 || *r.Total != "9223372036854775807" || *r.Expire != "300" {
		t.Fatal("excluded traffic affected expiry or counters")
	}
	b.Data["enabled"] = false
	r = mergedSourceUsage([]*ent.Resource{a, b}, now)
	if r.Disabled != 1 || r.Excluded != 0 || *r.Expire != "500" {
		t.Fatal("disabled source still included")
	}
	delete(a.Data["upstream_usage"].(map[string]any), "download")
	r = mergedSourceUsage([]*ent.Resource{a}, now)
	if r.Status != "unknown" || r.Download != nil || r.Remaining != nil || r.Total == nil {
		t.Fatal("missing counter silently zeroed")
	}
	a.Data["last_error"] = "refresh_failed"
	r = mergedSourceUsage([]*ent.Resource{a}, now)
	if r.Total != nil || r.Expire != nil {
		t.Fatal("failed observation counted fresh")
	}
	a.Data["last_error"] = ""
	a.Data["last_update"] = now.Add(-2 * time.Hour).Format(time.RFC3339)
	r = mergedSourceUsage([]*ent.Resource{a}, now)
	if r.Upload != nil {
		t.Fatal("expired observation counted")
	}
	c := row(3, "10", "15", "2", "0")
	r = mergedSourceUsage([]*ent.Resource{c}, now)
	if *r.Remaining != "0" || *r.Expire != "0" {
		t.Fatal("negative remaining or unlimited expiry")
	}
	r = mergedSourceUsage(nil, now)
	if r.Status != "empty" || r.Total != nil {
		t.Fatal("empty sources represented as zero")
	}
}
