package control

import (
	"testing"
	"time"
)

func TestTrafficDeltaNeverInventsZeroOrBridgesUnknown(t *testing.T) {
	now := time.Now().UTC()
	previous := trafficSample{At: now, Total: "100", Identity: "same", Quality: "unknown", BaselineFresh: true}
	for _, test := range []struct {
		name              string
		previous, current trafficSample
		want              string
		reason            string
	}{
		{"actual delta", previous, trafficSample{At: now.Add(time.Minute), Total: "164", Identity: "same", Quality: "fresh"}, "64", ""},
		{"real zero", previous, trafficSample{At: now.Add(time.Minute), Total: "100", Identity: "same", Quality: "fresh"}, "0", ""},
		{"missing", previous, trafficSample{At: now.Add(time.Minute), Total: "164", Identity: "same", Quality: "unknown", Reason: "meter_unknown"}, "", "meter_unknown"},
		{"gap", previous, trafficSample{At: now.Add(5 * time.Minute), Total: "164", Identity: "same", Quality: "fresh"}, "", "sample_gap"},
		{"changed grants", previous, trafficSample{At: now.Add(time.Minute), Total: "164", Identity: "different", Quality: "fresh"}, "", "grant_set_changed"},
		{"reset", previous, trafficSample{At: now.Add(time.Minute), Total: "1", Identity: "same", Quality: "fresh"}, "", "counter_reset"},
		{"bad counter", previous, trafficSample{At: now.Add(time.Minute), Total: "NaN", Identity: "same", Quality: "fresh"}, "", "counter_reset"},
		{"first sample", trafficSample{}, trafficSample{At: now, Total: "100", Identity: "same", Quality: "fresh"}, "", "sample_gap"},
	} {
		t.Run(test.name, func(t *testing.T) {
			delta, _, reason := trafficDelta(test.previous, test.current)
			if reason != test.reason {
				t.Fatalf("reason=%s", reason)
			}
			if test.want == "" && delta != nil || test.want != "" && (delta == nil || *delta != test.want) {
				t.Fatal("wrong interval delta")
			}
		})
	}
}
