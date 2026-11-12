package hoststats

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIPv6LoopbackIsNotPublic(t *testing.T) {
	dir := t.TempDir()
	netDir := filepath.Join(dir, "net")
	if err := os.MkdirAll(netDir, 0755); err != nil {
		t.Fatal(err)
	}
	// ::1 on lo, plus a link-local on eth0. Neither is public IPv6.
	body := "00000000000000000000000000000001 01 80 10 80 lo\nfe800000000000000000000000000001 02 40 20 80 eth0\n"
	if err := os.WriteFile(filepath.Join(netDir, "if_inet6"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "ipv6_route"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	usable, known := ipv6Usable(dir)
	if !known || usable {
		t.Fatalf("loopback was treated as usable IPv6: usable=%v known=%v", usable, known)
	}
}
