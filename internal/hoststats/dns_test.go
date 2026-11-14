package hoststats

import (
	"net/netip"
	"testing"
)

func TestResolverDifferenceIsNotPollution(t *testing.T) {
	for _, tc := range []struct {
		system, doh, want string
		known             bool
	}{
		{"1.1.1.1", "8.8.8.8", "answers_differ", true},
		{"1.1.1.1", "1.1.1.1", "clean", true},
		{"127.0.0.1", "1.1.1.1", "polluted", true},
		{"10.0.0.1", "1.1.1.1", "polluted", true},
		{"", "1.1.1.1", "", false},
	} {
		var system []netip.Addr
		if tc.system != "" {
			system = []netip.Addr{netip.MustParseAddr(tc.system)}
		}
		got, known := compareDNSAnswers(system, []netip.Addr{netip.MustParseAddr(tc.doh)})
		if got != tc.want || known != tc.known {
			t.Fatalf("%+v: %s %v", tc, got, known)
		}
	}
}
