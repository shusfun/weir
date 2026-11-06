package nodeprobe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
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
	// A real HTTP proxy forwards the absolute request to an isolated endpoint.
	token := "isolated-test-token"
	mode := "valid"
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer proxy.Close()
	o := Options{Target: "http://isolated.example.test/download", Token: token}
	for _, m := range []string{"valid", "bad", "status"} {
		mode = m
		r := download(context.Background(), proxy.Listener.Addr().String(), o)
		if (r.Status == "passed") != (m == "valid") {
			t.Fatalf("%s: %+v", m, r)
		}
	}
}
