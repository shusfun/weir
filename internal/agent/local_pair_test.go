package agent

import "testing"

func TestLoopbackCenterOnlyAcceptsPlainLocalOrigins(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:19810", "http://[::1]:19810", "http://localhost:19810", "http://127.0.0.1:19810/"} {
		if !loopbackCenter(ok) {
			t.Fatalf("%s should be accepted", ok)
		}
	}
	for _, bad := range []string{"http://10.0.0.1:19810", "http://example.com", "https://127.0.0.1:19810", "http://u:p@127.0.0.1:19810", "http://127.0.0.1:19810/api", "http://127.0.0.1:19810?x=1"} {
		if loopbackCenter(bad) {
			t.Fatalf("%s should be refused", bad)
		}
	}
}
