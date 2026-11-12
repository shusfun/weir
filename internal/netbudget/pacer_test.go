package netbudget

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

type shortWriter struct{}

func (shortWriter) Write(body []byte) (int, error) { return len(body) / 2, nil }

func TestWriterCancellationAndShortWrite(t *testing.T) {
	var pacer Pacer
	var output bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if n, err := pacer.Writer(ctx, &output).Write([]byte("private")); n != 0 || !errors.Is(err, context.Canceled) || output.Len() != 0 {
		t.Fatalf("cancelled writer emitted data: %d %v", n, err)
	}
	if n, err := pacer.Writer(t.Context(), shortWriter{}).Write([]byte("abcd")); n != 2 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write: %d %v", n, err)
	}
	body := bytes.Repeat([]byte("a"), 40<<10)
	if n, err := pacer.Writer(t.Context(), &output).Write(body); n != len(body) || err != nil || !bytes.Equal(output.Bytes(), body) {
		t.Fatal("chunked response changed", n, err)
	}
}
