package nodeprobe

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestDNSResponseValidation(t *testing.T) {
	_, q, err := dnsQuery()
	if err != nil {
		t.Fatal(err)
	}
	r := q
	r.Questions = append([]dnsmessage.Question(nil), q.Questions...)
	r.Response = true
	r.RCode = dnsmessage.RCodeNameError
	b, _ := r.Pack()
	if !validDNS(b, q) {
		t.Fatal("valid NXDOMAIN rejected")
	}
	r.ID++
	b, _ = r.Pack()
	if validDNS(b, q) {
		t.Fatal("wrong transaction accepted")
	}
	r.ID = q.ID
	r.Questions[0].Type = dnsmessage.TypeAAAA
	b, _ = r.Pack()
	if validDNS(b, q) {
		t.Fatal("wrong question accepted")
	}
	if validDNS([]byte("invalid"), q) {
		t.Fatal("malformed response accepted")
	}
}

func TestDNSProbeRoundTrip(t *testing.T) {
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go func() {
		buf := make([]byte, 4096)
		n, addr, err := c.ReadFrom(buf)
		if err != nil {
			return
		}
		var q dnsmessage.Message
		if q.Unpack(buf[:n]) != nil {
			return
		}
		q.Response, q.RCode = true, dnsmessage.RCodeNameError
		b, _ := q.Pack()
		c.WriteTo(b, addr)
	}()
	if r := dnsCheck(t.Context(), c.LocalAddr().String()); r.Status != "passed" {
		t.Fatalf("round trip: %+v", r)
	}
}

func TestDownloadChecksBytesAndStatus(t *testing.T) {
	token := "isolated-test-token"
	mode := "valid"
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("grant missing")
		}
		switch mode {
		case "valid":
			w.Write(Payload(token))
		case "bad":
			w.Write(Payload("different"))
		case "status":
			w.WriteHeader(403)
		}
	}))
	defer endpoint.Close()
	proxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	go func() {
		for {
			c, err := proxy.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				var greeting [2]byte
				if _, err := io.ReadFull(c, greeting[:]); err != nil {
					return
				}
				methods := make([]byte, int(greeting[1]))
				if _, err := io.ReadFull(c, methods); err != nil {
					return
				}
				_, _ = c.Write([]byte{5, 0})
				var connect [4]byte
				if _, err := io.ReadFull(c, connect[:]); err != nil || connect[3] != 1 {
					return
				}
				var address [6]byte
				if _, err := io.ReadFull(c, address[:]); err != nil {
					return
				}
				dst, err := net.Dial("tcp", net.JoinHostPort(net.IP(address[:4]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(address[4:])))))
				if err != nil {
					return
				}
				defer dst.Close()
				_, _ = c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				go func() { _, _ = io.Copy(dst, c) }()
				_, _ = io.Copy(c, dst)
			}()
		}
	}()
	o := Options{Target: endpoint.URL + "/download", Token: token}
	for _, m := range []string{"valid", "bad", "status"} {
		mode = m
		r := download(context.Background(), proxy.Addr().String(), o)
		if (r.Status == "passed") != (m == "valid") {
			t.Fatalf("%s: %+v", m, r)
		}
	}
}
