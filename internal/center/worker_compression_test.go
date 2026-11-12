package center

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWorkerCompressionBoundsAndMalformedInput(t *testing.T) {
	r := gin.New()
	r.POST("/payload", func(c *gin.Context) {
		var out struct {
			Value string `json:"value"`
		}
		if bindWorker(c, &out, 1024) {
			c.Status(204)
		}
	})
	zip := func(b []byte) []byte {
		var out bytes.Buffer
		w := gzip.NewWriter(&out)
		w.Write(b)
		w.Close()
		return out.Bytes()
	}
	for _, tc := range []struct {
		body     []byte
		encoding string
		want     int
	}{
		{zip([]byte(`{"value":"ok"}`)), "gzip", 204},
		{[]byte(`{"value":"ok"}`), "", 204},
		{zip(append([]byte(`{"value":"`), append(bytes.Repeat([]byte("x"), 2048), []byte(`"}`)...)...)), "gzip", 400},
		{[]byte("broken"), "gzip", 400},
		{[]byte(`{"value":"ok"}`), "br", 400},
		{[]byte(`{"value":"ok","extra":true}`), "", 400},
		{bytes.Repeat([]byte("x"), 1025), "", 400},
	} {
		req := httptest.NewRequest("POST", "/payload", bytes.NewReader(tc.body))
		req.Header.Set("Content-Encoding", tc.encoding)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			b, _ := io.ReadAll(w.Result().Body)
			t.Fatalf("encoding %q got %d %s", tc.encoding, w.Code, b)
		}
	}
}
