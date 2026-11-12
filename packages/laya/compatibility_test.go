package laya

import "testing"

func TestCallerCompatibilityAndDegradation(t *testing.T) {
	for _, tc := range []struct {
		version, rng     string
		policy           DegradationPolicy
		base, background bool
	}{
		{"v0.18.0", ">=0.18.0 <0.19.0", DegradeReject, true, true},
		{"v0.19.0", ">=0.18.0 <0.19.0", DegradeReject, false, false},
		{"v0.19.0", ">=0.18.0 <0.19.0", DegradeBaseOnly, true, false},
		{"v0.19.0", ">=0.19.0 <0.20.0", DegradeBaseOnly, true, true},
	} {
		d, err := CheckCompatibility(Compatibility{InstalledVersion: tc.version, VerifiedRange: tc.rng, OutOfRange: tc.policy})
		if err != nil || d.EnableBase != tc.base || d.EnableBackground != tc.background {
			t.Fatalf("%+v => %+v %v", tc, d, err)
		}
	}
	for _, v := range []string{"01.2.3", "1.2.3-01", "1.2.3-../../bad", "1.2"} {
		if _, err := CheckCompatibility(Compatibility{InstalledVersion: v, VerifiedRange: ">=1.0.0", OutOfRange: DegradeReject}); err == nil {
			t.Fatalf("accepted %s", v)
		}
	}
}
