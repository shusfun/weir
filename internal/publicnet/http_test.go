package publicnet

import (
	"context"
	"net/netip"
	"testing"
)

type resolver []netip.Addr

func (r resolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) { return r, nil }

func TestPrivateAndMetadataCannotBeDialed(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "::1", "fd00::1", "fe80::1", "::ffff:127.0.0.1", "64:ff9b::a00:1", "2002:7f00:1::", "0.1.2.3"} {
		t.Run(ip, func(t *testing.T) {
			addr := netip.MustParseAddr(ip)
			if Public(addr) {
				t.Fatal("private accepted")
			}
			_, err := (Transport{Resolver: resolver{netip.MustParseAddr("8.8.8.8"), addr}}).DialContext(t.Context(), "tcp", "public.example:443")
			if err == nil || err.Error() != "public_address_private" {
				t.Fatalf("mixed DNS not refused: %v", err)
			}
		})
	}
}
func TestRedirectCannotTargetPrivate(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1/", "http://[::1]/", "http://169.254.169.254/latest/meta-data/", "http://localhost./", "http://u:p@example.com/"} {
		if _, err := URL(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := URL("https://github.com/example/repo"); err != nil {
		t.Fatal(err)
	}
}
