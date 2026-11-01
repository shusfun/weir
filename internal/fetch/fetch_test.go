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

// 持续低速的来源被放弃、已下载部分保留，下一个来源从断点续传；最后一个来源不按低速放弃。
func TestSlowSourceSwitchesAndResumes(t *testing.T) {
	body := bytes.Repeat([]byte("w"), 4000)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		for i := 0; i < len(body); i += 100 {
			if _, err := w.Write(body[i : i+100]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(300 * time.Millisecond):
			}
		}
	}))
	t.Cleanup(slow.Close)
	good, calls := serve(t, body, 0, true)
	dest := filepath.Join(t.TempDir(), "k")
	var logs []string
	req := Request{URLs: []string{slow.URL, good.URL}, Dest: dest, SHA256: sum(body), MinSpeed: 1000, SlowFor: 2 * time.Second, Log: func(s string) { logs = append(logs, s) }}
	if err := Download(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(logs, "\n")
	if !strings.Contains(all, "下载过慢") || !strings.Contains(all, "续传") || *calls != 1 {
		t.Fatalf("calls=%d logs: %v", *calls, logs)
	}
	// 只有一个来源时即使慢也下完。
	req = Request{URLs: []string{slow.URL}, Dest: filepath.Join(t.TempDir(), "only"), SHA256: sum(body), MinSpeed: 1 << 20, SlowFor: time.Second}
	start := time.Now()
	if err := Download(context.Background(), req); err != nil || time.Since(start) < 5*time.Second {
		t.Fatalf("last source must not be dropped for speed: %v after %s", err, time.Since(start))
	}
}

// 非 HTTP 来源排在地址之后，内容同样按摘要校验。
func TestSourceAfterURLs(t *testing.T) {
	body := []byte("relayed kernel")
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	t.Cleanup(down.Close)
	relay := func(content []byte) Source {
		return Source{Name: "经 VPS hk", Fetch: func(ctx context.Context, dest string, progress func(int64, int64)) error {
			progress(int64(len(content)), int64(len(content)))
			return os.WriteFile(dest, content, 0o644)
		}}
	}
	dest := filepath.Join(t.TempDir(), "k")
	if err := Download(context.Background(), Request{URLs: []string{down.URL}, Sources: []Source{relay(body)}, Dest: dest, SHA256: sum(body)}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, body) {
		t.Fatalf("got %q", got)
	}
	err := Download(context.Background(), Request{Sources: []Source{relay([]byte("tampered"))}, Dest: filepath.Join(t.TempDir(), "x"), SHA256: sum(body)})
	if !errors.Is(err, ErrDigest) {
		t.Fatalf("want digest error, got %v", err)
	}
}

// Before 把非 HTTP 来源插到指定的 HTTP 地址之前；为 0 或超出时排在最后。
func TestStepsOrder(t *testing.T) {
	r := Request{URLs: []string{"https://a", "https://b"}, Sources: []Source{{Name: "first", Before: 1}, {Name: "mid", Before: 2}, {Name: "tail"}, {Name: "far", Before: 9}}}
	var got []string
	for _, s := range r.steps() {
		if s.src != nil {
			got = append(got, s.src.Name)
		} else {
			got = append(got, s.url)
		}
	}
	if strings.Join(got, ",") != "first,https://a,mid,https://b,tail,far" {
		t.Fatalf("order = %v", got)
	}
}
