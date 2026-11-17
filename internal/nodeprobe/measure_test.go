package nodeprobe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type measureTransport func(*http.Request) (*http.Response, error)

func (f measureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEchoRetriesStartupDisconnectOnly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		calls  int
	}{
		{"startup", io.EOF, 200, 2},
		{"rejected", nil, 403, 1},
		{"certificate", errors.New("certificate verification failed"), 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: measureTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 && tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(`{"ip":"203.0.113.1"}`)), Header: make(http.Header)}, nil
			})}
			ip, _, err := readEcho(t.Context(), client, "https://example.test/echo")
			if calls != tc.calls || (tc.name == "startup" && (err != nil || ip != "203.0.113.1")) || (tc.name != "startup" && err == nil) {
				t.Fatalf("calls=%d ip=%q err=%v", calls, ip, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	client := &http.Client{Transport: measureTransport(func(*http.Request) (*http.Response, error) { cancel(); return nil, io.EOF })}
	if _, _, err := readEcho(ctx, client, "https://example.test/echo"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled startup: %v", err)
	}
}

func TestReadDownloadKeepsPartialBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()
	n, _, err := ReadDownload(t.Context(), srv.Client(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "下载没有完成") || n != 5 {
		t.Fatalf("partial = %d %v", n, err)
	}
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(Payload("weir-exit-probe"))
	}))
	defer ok.Close()
	n, ms, err := ReadDownload(t.Context(), ok.Client(), ok.URL)
	if err != nil || n != PayloadSize || ms < 0 {
		t.Fatalf("complete = %d %d %v", n, ms, err)
	}
}
