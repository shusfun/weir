package control

import (
	"github.com/imzyb/MiGate/ent"
	"testing"
	"time"
)

func TestDeviceQualityNeverTreatsMissingOrFutureDataAsHealthy(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		seen           time.Time
		status         string
		contact, meter bool
	}{
		{time.Time{}, "fresh", false, false},
		{now.Add(-61 * time.Second), "fresh", false, false},
		{now.Add(time.Minute), "fresh", false, false},
		{now, "unknown", true, false},
		{now, "persistence_error", true, false},
		{now, "fresh", true, true},
	} {
		contact, meter := deviceQuality(&ent.Device{LastSeen: test.seen, Metadata: map[string]any{"traffic_status": test.status}}, now)
		if contact != test.contact || meter != test.meter {
			t.Fatalf("quality for %s/%s = %v/%v", test.seen, test.status, contact, meter)
		}
	}
}
