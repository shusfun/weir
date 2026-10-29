package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// serve 提供 body；cut > 0 时第一次请求只给前 cut 字节就断开。
func serve(t *testing.T, body []byte, cut int, ranges bool) (*httptest.Server, *int32) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		start := 0
		if rg := r.Header.Get("Range"); rg != "" && ranges {
			start, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
			w.Header().Set("Content-Range", "bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(len(body)-1)+"/"+strconv.Itoa(len(body)))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)-start))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
		if n == 1 && cut > 0 {
			_, _ = w.Write(body[start:cut])
			if hj, ok := w.(http.Hijacker); ok {
				c, _, _ := hj.Hijack()
				_ = c.Close()
			}
			return
		}
		_, _ = w.Write(body[start:])
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestResumeAfterDisconnect(t *testing.T) {
	body := bytes.Repeat([]byte("weir-"), 40000)
	srv, calls := serve(t, body, 70000, true)
	dest := filepath.Join(t.TempDir(), "k.zip")
	var logs []string
	req := Request{URLs: []string{srv.URL}, Dest: dest, SHA256: sum(body), Log: func(s string) { logs = append(logs, s) }}
	if err := Download(context.Background(), req); err == nil {
		t.Fatal("first attempt should fail after disconnect")
	}
	if st, err := os.Stat(dest + ".part"); err != nil || st.Size() != 70000 {
		t.Fatalf("partial file kept: %v %v", st, err)
	}
	if err := Download(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, body) || *calls != 2 {
		t.Fatalf("resumed content wrong, calls=%d", *calls)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "续传") {
		t.Fatalf("log should mention resume: %v", logs)
	}
}

func TestFallsBackAndRejectsDigestMismatch(t *testing.T) {
	body := []byte("the real kernel")
	bad, _ := serve(t, []byte("tampered kernel"), 0, false)
	good, _ := serve(t, body, 0, false)
	dest := filepath.Join(t.TempDir(), "k")
	var logs []string
	err := Download(context.Background(), Request{URLs: []string{bad.URL, good.URL}, Dest: dest, SHA256: sum(body), Log: func(s string) { logs = append(logs, s) }})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(logs, "\n")
	if !strings.Contains(all, "摘要不一致") || !strings.Contains(all, "改用") {
		t.Fatalf("logs: %v", logs)
	}
	if err = Download(context.Background(), Request{URLs: []string{bad.URL}, Dest: filepath.Join(t.TempDir(), "x"), SHA256: sum(body)}); !errors.Is(err, ErrDigest) {
		t.Fatalf("want digest error, got %v", err)
	}
}

func TestIdleTimeoutAndCancel(t *testing.T) {
	stall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("abc"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer stall.Close()
	dest := filepath.Join(t.TempDir(), "k")
	start := time.Now()
	err := Download(context.Background(), Request{URLs: []string{stall.URL}, Dest: dest, SHA256: strings.Repeat("0", 64), Idle: 200 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "没有收到数据") || time.Since(start) > 3*time.Second {
		t.Fatalf("idle timeout: %v after %s", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	err = Download(ctx, Request{URLs: []string{stall.URL, stall.URL}, Dest: dest, SHA256: strings.Repeat("0", 64), Idle: 10 * time.Second})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := os.Stat(dest + ".part"); err != nil {
		t.Fatal("cancel should keep the partial file")
	}
}

func TestRequiresDigestAndSizeLimit(t *testing.T) {
	if err := Download(context.Background(), Request{URLs: []string{"https://example.com"}, Dest: "x"}); err == nil {
		t.Fatal("missing digest must be refused")
	}
	body := bytes.Repeat([]byte("x"), 5000)
	srv, _ := serve(t, body, 0, false)
	err := Download(context.Background(), Request{URLs: []string{srv.URL}, Dest: filepath.Join(t.TempDir(), "k"), SHA256: sum(body), MaxSize: 1000})
	if err == nil || !strings.Contains(err.Error(), "过大") {
		t.Fatalf("size limit: %v", err)
	}
}
