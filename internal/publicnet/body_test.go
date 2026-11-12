package publicnet

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"testing"
)

func TestReadBodyBoundsWireAndDecompressedData(t *testing.T) {
	var encoded bytes.Buffer
	w := gzip.NewWriter(&encoded)
	w.Write(bytes.Repeat([]byte("x"), 4096))
	w.Close()
	for _, tc := range []struct {
		name, encoding string
		data           []byte
		wire, decoded  int64
		pass           bool
	}{
		{"identity", "", []byte("ok"), 2, 2, true},
		{"wire", "", []byte("oversized"), 2, 20, false},
		{"gzip", "gzip", encoded.Bytes(), 1024, 4096, true},
		{"bomb", "gzip", encoded.Bytes(), 1024, 100, false},
		{"invalid", "gzip", []byte("broken"), 100, 100, false},
		{"unsupported", "br", []byte("data"), 100, 100, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &http.Response{Header: http.Header{"Content-Encoding": []string{tc.encoding}}, Body: io.NopCloser(bytes.NewReader(tc.data))}
			_, err := ReadBody(r, tc.wire, tc.decoded)
			if (err == nil) != tc.pass {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
