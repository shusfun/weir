package gate

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestClientCompressesOnlyNegotiatedLargeWorkerPayloads(t *testing.T) {
	var encoding string
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoding = r.Header.Get("Content-Encoding")
		var rd io.Reader = r.Body
		if encoding == "gzip" {
			g, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer g.Close()
			rd = g
		}
		received, _ = io.ReadAll(rd)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	c := &centerClient{center: srv.URL, http: srv.Client()}
	body := contracts.MeterReport{Epoch: "test", Seq: 1}
	for i := 0; i < 2000; i++ {
		body.Usage = append(body.Usage, contracts.MeterSample{StatsKey: strings.Repeat("k", 40), Up: int64(i)})
	}
	for _, enabled := range []bool{false, true} {
		c.gzipPayload.Store(enabled)
		resp, err := c.raw(context.Background(), srv.Client(), "POST", "/agent/meter", body, false)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if (encoding == "gzip") != enabled || !strings.Contains(string(received), `"epoch":"test"`) {
			t.Fatalf("negotiation: %q %d", encoding, len(received))
		}
	}
	resp, err := c.raw(context.Background(), srv.Client(), "POST", "/agent/heartbeat", body, false)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if encoding != "" {
		t.Fatal("unnegotiated route compressed")
	}
}
