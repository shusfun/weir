package gate

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestSocksUDPMeasuresUpstreamNotLocalRelay(t *testing.T) {
	reject := socksReply(t, 0x07)
	allow := socksReply(t, 0x00)
	ctx := t.Context()
	supported, known := socksUDP(ctx, reject, "", "")
	if !known || supported {
		t.Fatalf("rejected associate = supported %v known %v", supported, known)
	}
	supported, known = socksUDP(ctx, allow, "", "")
	if !known || !supported {
		t.Fatalf("accepted associate = supported %v known %v", supported, known)
	}
}

func socksReply(t *testing.T, code byte) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				buf := make([]byte, 32)
				if _, err := io.ReadFull(c, buf[:3]); err != nil {
					return
				}
				_, _ = c.Write([]byte{0x05, 0x00})
				if _, err := io.ReadFull(c, buf[:4]); err != nil {
					return
				}
				_, _ = c.Write([]byte{0x05, code, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			}(c)
		}
	}()
	return ln.Addr().String()
}
