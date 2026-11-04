package gate

import (
	"net"
	"strconv"
	"testing"
)

func TestFreeLoopbackPortRejectsUnboundReservedPort(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	reserved := first.Addr().(*net.TCPAddr).Port
	first.Close()
	original := listenLoopback
	defer func() { listenLoopback = original }()
	calls := 0
	listenLoopback = func(network, address string) (net.Listener, error) {
		calls++
		if calls == 1 {
			return net.Listen(network, net.JoinHostPort("127.0.0.1", strconv.Itoa(reserved)))
		}
		return net.Listen(network, address)
	}
	port, err := freeLoopbackPort(reserved)
	if err != nil {
		t.Fatal(err)
	}
	if port == reserved || calls < 2 {
		t.Fatal("reused a statistics port before its process bound it")
	}
}
