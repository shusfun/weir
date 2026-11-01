package gate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

// testAgent 连到一个假中心（认证直接通过，其余请求交给 handle）；extHTTP 信任 httptest 的证书，ghHost 指向 gh。
func testAgent(t *testing.T, gh *httptest.Server, handle http.HandlerFunc) *Agent {
	t.Helper()
	center := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/agent/challenge":
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": "c"})
		case "/api/v1/agent/auth":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "t"})
		default:
			handle(w, r)
		}
	}))
	t.Cleanup(center.Close)
	dir := t.TempDir()
	key := make([]byte, 64)
	cfg := Config{Center: center.URL, GateID: 1, PrivateKey: base64.StdEncoding.EncodeToString(key)}
	cc, err := newCenterClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cc.http, cc.long = center.Client(), center.Client()
	a := &Agent{P: Paths{Data: dir, Socket: filepath.Join(dir, "none.sock")}, cfg: cfg, center: cc, helper: newHelperClient(filepath.Join(dir, "none.sock")),
		pids: map[string]int{}, uploadBackoff: time.Millisecond}
	if gh != nil {
		u, _ := url.Parse(gh.URL)
		a.extHTTP, a.ghHost = gh.Client(), u.Host
	}
	return a
}

// releasePkg 生成发布包；bad 为真时 SHA256SUMS 与 weir 不符。
func releasePkg(t *testing.T, weir []byte, bad bool) []byte {
	t.Helper()
	install := []byte("#!/bin/sh\n")
	sumWeir := sumOf(weir)
	if bad {
		sumWeir = sumOf([]byte("other"))
	}
	files := []struct {
		name string
		data []byte
	}{{"weir", weir}, {"install.sh", install}, {"SHA256SUMS", []byte(sumWeir + "  weir\n" + sumOf(install) + "  install.sh\n")}}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		_ = tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(f.data)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// waitIdle 等后台自更新结束。
func waitIdle(t *testing.T, a *Agent) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		a.up.mu.Lock()
		idle := a.up.run == nil
		a.up.mu.Unlock()
		if idle {
			return
		}
	}
	t.Fatal("self update did not finish")
}

// 心跳回复带目标版本时在后台从 GitHub 下载：镜像失败换下一个地址，按官方摘要与包内 SHA256SUMS 校验后交给辅助程序。
func TestFollowCenterUpdatesFromGitHub(t *testing.T) {
	weir := []byte("weir binary 1.2.3")
	pkg := releasePkg(t, weir, false)
	gh := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/m/") {
			w.WriteHeader(502)
			return
		}
		http.ServeContent(w, r, "pkg.tar.gz", time.Time{}, bytes.NewReader(pkg))
	}))
	t.Cleanup(gh.Close)
	direct := gh.URL + "/o/r/releases/download/v1.2.3/weir-1.2.3-linux-amd64.tar.gz"
	var centerHits int
	var mu sync.Mutex
	a := testAgent(t, gh, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/agent/heartbeat":
			_ = json.NewEncoder(w).Encode(contracts.HeartbeatReply{Release: &contracts.ReleaseTarget{Version: "1.2.3", SHA256: sumOf(pkg), URLs: []string{gh.URL + "/m/" + direct, direct}}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/agent/release/"):
			mu.Lock()
			centerHits++
			mu.Unlock()
			w.WriteHeader(404)
		}
	})
	got := make(chan []byte, 1)
	a.replaceFn = func(_ context.Context, version, sum string, data []byte) error {
		if version != "1.2.3" || sum != sumOf(data) {
			t.Errorf("replace %s %s", version, sum)
		}
		got <- data
		return nil
	}
	if err := a.heartbeat(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-got:
		if !bytes.Equal(data, weir) {
			t.Fatalf("replaced with %q", data)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("update not started")
	}
	waitIdle(t, a)
	if st := a.updateStatus(); st != nil {
		t.Fatalf("status after success = %+v", st)
	}
	if centerHits != 0 {
		t.Fatalf("center download used although GitHub worked")
	}
	if left, _ := os.ReadDir(filepath.Join(a.P.Data, "update")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

// GitHub 与中心都失败时状态为 failed 并写明两边原因；10 分钟内心跳不再触发，手动更新（旧中心只给 version）不受限。
func TestSelfUpdateFallbackAndBackoff(t *testing.T) {
	weir := []byte("weir from center")
	gh := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	t.Cleanup(gh.Close)
	var mu sync.Mutex
	centerHits, centerOK := 0, false
	var beats []contracts.Heartbeat
	a := testAgent(t, gh, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/agent/heartbeat":
			var hb contracts.Heartbeat
			_ = json.NewDecoder(r.Body).Decode(&hb)
			beats = append(beats, hb)
			_ = json.NewEncoder(w).Encode(contracts.HeartbeatReply{Release: &contracts.ReleaseTarget{Version: "1.2.3", SHA256: strings.Repeat("a", 64), URLs: []string{gh.URL + "/o/r/x.tar.gz"}}})
		case r.URL.Path == "/api/v1/agent/release/1.2.3":
			centerHits++
			if !centerOK {
				w.WriteHeader(500)
				return
			}
			w.Header().Set("X-Release-SHA256", sumOf(weir))
			_, _ = w.Write(weir)
		}
	})
	var replaced [][]byte
	a.replaceFn = func(_ context.Context, _, _ string, data []byte) error { replaced = append(replaced, data); return nil }
	hits := func() int { mu.Lock(); defer mu.Unlock(); return centerHits }

	if err := a.heartbeat(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, a)
	st := a.updateStatus()
	if st == nil || st.State != "failed" || !strings.Contains(st.Error, "从 GitHub 下载失败") || !strings.Contains(st.Error, "HTTP 404") || !strings.Contains(st.Error, "从中心下载程序失败") {
		t.Fatalf("failed status = %+v", st)
	}
	if hits() != 1 {
		t.Fatalf("center hits = %d", hits())
	}
	// 冷却期内心跳不重试，但继续上报失败状态。
	if err := a.heartbeat(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, a)
	mu.Lock()
	lastBeat := beats[len(beats)-1]
	mu.Unlock()
	if hits() != 1 || lastBeat.SelfUpdate == nil || lastBeat.SelfUpdate.State != "failed" {
		t.Fatalf("retried within backoff (hits %d) or status not reported: %+v", hits(), lastBeat.SelfUpdate)
	}
	// 手动更新不受冷却限制；旧中心只给 version，直接从中心下载。
	mu.Lock()
	centerOK = true
	mu.Unlock()
	res := a.runAction(t.Context(), contracts.Action{Kind: contracts.ActionReleaseReplace, Payload: map[string]any{"version": "1.2.3"}, ExpiresAt: time.Now().Add(time.Minute)})
	if !res.OK || hits() != 2 || len(replaced) != 1 || !bytes.Equal(replaced[0], weir) {
		t.Fatalf("manual update = %+v hits=%d replaced=%d", res, hits(), len(replaced))
	}
	if a.updateStatus() != nil {
		t.Fatalf("status after manual success = %+v", a.updateStatus())
	}
	// 冷却过后心跳再次触发。
	a.up.mu.Lock()
	a.up.failedAt = time.Now().Add(-updateRetry - time.Second)
	a.up.mu.Unlock()
	mu.Lock()
	centerOK = false
	mu.Unlock()
	if err := a.heartbeat(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, a)
	if hits() != 3 {
		t.Fatalf("heartbeat after backoff did not retry: hits=%d", hits())
	}
}

// 官方摘要对得上、但包内 SHA256SUMS 与程序不符时不替换，退回中心。
func TestSelfUpdateRejectsTamperedPackage(t *testing.T) {
	pkg := releasePkg(t, []byte("weir"), true)
	gh := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(pkg) }))
	t.Cleanup(gh.Close)
	a := testAgent(t, gh, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	a.replaceFn = func(context.Context, string, string, []byte) error { t.Error("tampered package replaced"); return nil }
	err := a.selfUpdate(t.Context(), contracts.ReleaseTarget{Version: "1.2.3", SHA256: sumOf(pkg), URLs: []string{gh.URL + "/o/r/p.tar.gz"}})
	if err == nil || !strings.Contains(err.Error(), "SHA256SUMS") || !strings.Contains(err.Error(), "中心") {
		t.Fatalf("err = %v", err)
	}
}

func TestGithubURLs(t *testing.T) {
	a := &Agent{}
	for raw, ok := range map[string]bool{
		"https://github.com/o/r/releases/download/v1/a.tar.gz":                  true,
		"https://gh-proxy.com/https://github.com/o/r/releases/download/v1/a":    true,
		"https://mirror.example/gh/https://github.com/o/r/releases/download/v1": true,
		"http://github.com/o/r":                                    false,
		"https://evil.example/o/r":                                 false,
		"https://user@github.com/o/r":                              false,
		"https://gh-proxy.com/https://evil.example/x":              false,
		"https://gh-proxy.com/?q=https://github.com/o/r":           false,
		"https://github.com.evil.example/o/r":                      false,
		"http://gh-proxy.com/https://github.com/o/r":               false,
		"https://gh-proxy.com/https://github.com/https://evil.x/a": false,
	} {
		if _, err := a.githubURLs([]string{raw}); (err == nil) != ok {
			t.Errorf("%s: err = %v, want ok=%v", raw, err, ok)
		}
	}
	if _, err := a.githubURLs(nil); err == nil {
		t.Error("empty list accepted")
	}
}

// asset.fetch：下载校验后分块上传。中心先报已有第一块；某块写入后回复丢失（500），Gate 重新读进度；
// 读到的进度落后时 PUT 得到 409，按回复的位置续传，不重复上传，也不从头再来。
func TestAssetFetchUploadsInChunksAndResumes(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), (3*contracts.UploadChunk+1000)/16)
	gh := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.bin", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(gh.Close)
	var mu sync.Mutex
	recv := append([]byte(nil), data[:contracts.UploadChunk]...) // 上次已传完第一块
	var puts []int64
	lostReply, staleGet := false, false
	a := testAgent(t, gh, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/api/v1/agent/uploads/up-1" {
			w.WriteHeader(404)
			return
		}
		state := func(code int, n int64) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(contracts.UploadState{Received: n})
		}
		if r.Method == "GET" {
			if staleGet {
				staleGet = false
				state(200, contracts.UploadChunk) // 落后的进度
				return
			}
			state(200, int64(len(recv)))
			return
		}
		off, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		puts = append(puts, off)
		body, _ := io.ReadAll(r.Body)
		if len(body) > contracts.UploadChunk {
			t.Errorf("chunk too large: %d", len(body))
		}
		if off != int64(len(recv)) {
			state(409, int64(len(recv)))
			return
		}
		recv = append(recv, body...)
		if off == 2*contracts.UploadChunk && !lostReply {
			lostReply, staleGet = true, true
			w.WriteHeader(500) // 已写入，但回复丢失
			return
		}
		state(200, int64(len(recv)))
	})
	var p map[string]any
	_ = convert(contracts.AssetFetch{UploadID: "up-1", URLs: []string{gh.URL + "/o/r/releases/download/v1/a.bin"}, SHA256: sumOf(data), Size: int64(len(data)), MaxSize: 64 << 20}, &p)
	res := a.runAction(t.Context(), contracts.Action{Kind: contracts.ActionAssetFetch, Payload: p, ExpiresAt: time.Now().Add(time.Minute)})
	if !res.OK {
		t.Fatalf("asset.fetch = %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if !bytes.Equal(recv, data) {
		t.Fatalf("center received %d bytes, want %d identical", len(recv), len(data))
	}
	c := int64(contracts.UploadChunk)
	if want := fmt.Sprint([]int64{c, 2 * c, c, 3 * c}); fmt.Sprint(puts) != want {
		t.Fatalf("PUT offsets = %v, want %s", puts, want)
	}
	if left, _ := os.ReadDir(filepath.Join(a.P.Data, "assets")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

func TestAssetFetchRejectsBadRequests(t *testing.T) {
	a := testAgent(t, nil, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected center call %s", r.URL.Path) })
	ok := "https://github.com/o/r/releases/download/v1/a"
	sum := strings.Repeat("a", 64)
	for name, req := range map[string]contracts.AssetFetch{
		"非 GitHub 地址":  {UploadID: "u", URLs: []string{"https://evil.example/a"}, SHA256: sum, Size: 1, MaxSize: 10},
		"上限超过 512 MiB": {UploadID: "u", URLs: []string{ok}, SHA256: sum, Size: 1, MaxSize: 513 << 20},
		"大小超过上限":       {UploadID: "u", URLs: []string{ok}, SHA256: sum, Size: 11, MaxSize: 10},
		"上传编号带路径":      {UploadID: "../x", URLs: []string{ok}, SHA256: sum, Size: 1, MaxSize: 10},
		"缺少摘要":         {UploadID: "u", URLs: []string{ok}, Size: 1, MaxSize: 10},
		"没有地址":         {UploadID: "u", SHA256: sum, Size: 1, MaxSize: 10},
	} {
		var p map[string]any
		_ = convert(req, &p)
		if _, err := a.assetFetch(t.Context(), p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSourceProbe(t *testing.T) {
	gh := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/missing") {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Range") != "bytes=0-1023" {
			t.Errorf("range = %q", r.Header.Get("Range"))
		}
		http.ServeContent(w, r, "a", time.Time{}, bytes.NewReader(make([]byte, 8192)))
	}))
	t.Cleanup(gh.Close)
	a := testAgent(t, gh, func(w http.ResponseWriter, r *http.Request) {})
	run := func(u string, n int64) contracts.ActionResult {
		return a.runAction(t.Context(), contracts.Action{Kind: contracts.ActionSourceProbe, Payload: map[string]any{"url": u, "bytes": float64(n)}, ExpiresAt: time.Now().Add(time.Minute)})
	}
	res := run(gh.URL+"/o/r/a", 1024)
	var out contracts.SourceProbeResult
	_ = convert(res.Result, &out)
	if !res.OK || out.Bytes != 1024 || out.Speed <= 0 || out.TTFBMs < 0 {
		t.Fatalf("probe = %+v %+v", res, out)
	}
	res = run(gh.URL+"/o/r/missing", 1024)
	_ = convert(res.Result, &out)
	if res.OK || !strings.Contains(res.Error, "HTTP 404") || out.Error != res.Error {
		t.Fatalf("missing = %+v", res)
	}
	if res = run(gh.URL+"/o/r/a", 5<<20); res.OK {
		t.Fatal("bytes over 4 MiB accepted")
	}
	if res = run("https://evil.example/a", 1024); res.OK {
		t.Fatal("non-GitHub url accepted")
	}
}
