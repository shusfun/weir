package center

import (
	"net"
	"testing"
	"time"
)

func TestServerCloseStopsMCPListener(t *testing.T) {
	s, _ := newInstalledServer(t)
	m, err := s.ensureMCP()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", m.addr, time.Second)
	if err != nil {
		t.Fatalf("MCP listener did not start: %v", err)
	}
	_ = conn.Close()

	s.Close()
	conn, err = net.DialTimeout("tcp", m.addr, 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("MCP listener still accepts connections after center shutdown")
	}
}
