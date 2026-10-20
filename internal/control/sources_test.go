package control

import (
	"testing"
	"time"
)

func TestSourceCachePolicy(t *testing.T) {
	now := time.Now().UTC()
	data := map[string]any{"last_update": now.Add(-2 * time.Hour).Format(time.RFC3339), "links": []string{"trojan://synthetic@example.com:443#fixture"}, "last_error": "refresh_failed"}
	if _, stale, err := sourceLinks(data, now); err == nil || !stale {
		t.Fatal("failed refresh served stale cache by default")
	}
	data["allow_stale"] = true
	if links, stale, err := sourceLinks(data, now); err != nil || !stale || len(links) != 1 {
		t.Fatal("explicit bounded fallback unavailable", err)
	}
	data["max_stale_seconds"] = 3600
	if _, _, err := sourceLinks(data, now); err == nil {
		t.Fatal("expired fallback served")
	}
	data["last_update"] = now.Format(time.RFC3339)
	data["last_error"] = ""
	if _, stale, err := sourceLinks(data, now); err != nil || stale {
		t.Fatal("fresh source rejected", err)
	}
	data["enabled"] = false
	if _, _, err := sourceLinks(data, now); err == nil {
		t.Fatal("disabled source served")
	}
}
