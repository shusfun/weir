package center

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/packages/contracts"
)

func xrayZip(body string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("xray")
	_, _ = w.Write([]byte(body))
	_ = zw.Close()
	return buf.Bytes()
}

// fakeReleases 扮演 GitHub：Xray 三个版本（发布时间、下载量各不同），每个版本 amd64 与 arm64 两个下载包。
func fakeReleases(t *testing.T, s *Server) *int32 {
	t.Helper()
	assets := map[string][]byte{}
	var downloads int32
	type rel struct {
		v     string
		age   time.Duration
		count int
	}
	rels := []rel{{"26.10.17", 3 * 24 * time.Hour, 900}, {"26.3.27", 60 * 24 * time.Hour, 500}, {"25.1.30", 200 * 24 * time.Hour, 800}}
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/XTLS/Xray-core/releases":
			var list []map[string]any
			for _, x := range rels {
				var as []map[string]any
				for _, arch := range []string{"amd64", "arm64"} {
					name := "Xray-linux-64.zip"
					if arch == "arm64" {
						name = "Xray-linux-arm64-v8a.zip"
					}
					path := "/dl/" + x.v + "/" + name
					assets[path] = xrayZip("xray-" + x.v + "-" + arch)
					sum := sha256.Sum256(assets[path])
					as = append(as, map[string]any{"name": name, "browser_download_url": srv.URL + path, "digest": "sha256:" + hex.EncodeToString(sum[:]), "download_count": x.count})
				}
				list = append(list, map[string]any{"tag_name": "v" + x.v, "published_at": time.Now().Add(-x.age).Format(time.RFC3339), "assets": as})
			}
			_ = json.NewEncoder(w).Encode(list)
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			atomic.AddInt32(&downloads, 1)
			if b, ok := assets[r.URL.Path]; ok {
				_, _ = w.Write(b)
				return
			}
			http.NotFound(w, r)
		case r.URL.Path == "/repos/shusfun/weir/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v0.1.20", "published_at": time.Now().Format(time.RFC3339), "body": "notes"}, {"tag_name": "v0.1.12"}})
		default:
			_ = json.NewEncoder(w).Encode([]any{})
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	s.GitHub = &ghrelease.Client{HTTP: srv.Client(), API: srv.URL, Host: u.Host}
	return &downloads
}

func waitJob(t *testing.T, admin *testClient, id string) map[string]any {
	t.Helper()
	for i := 0; i < 200; i++ {
		j := mustJSON(t, admin, "GET", "/api/v1/admin/jobs/"+url.PathEscape(id), nil, 200)["job"].(map[string]any)
		if j["status"] != "running" {
			return j
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", id)
	return nil
}

func TestKernelJobsArchesRecommendationAndNotices(t *testing.T) {
	s, admin, g := setupGate(t)
	downloads := fakeReleases(t, s)
	// 再接入一台 arm64 的 VPS：需要的架构变成 amd64 + arm64，内核页不再选架构。
	g2 := pairFakeGate(t, admin)
	if code, _ := g2.call("POST", "/api/v1/agent/heartbeat", contracts.Heartbeat{Version: "test", Hostname: "arm", Arch: "arm64"}); code != 200 {
		t.Fatalf("heartbeat = %d", code)
	}
	_ = g
	k := mustJSON(t, admin, "GET", "/api/v1/admin/kernels", nil, 200)["kernels"].(map[string]any)["xray"].(map[string]any)
	if fmt.Sprint(k["arches"]) != "[amd64 arm64]" {
		t.Fatalf("arches = %v", k["arches"])
	}
	// 推荐：26.10.17 下载量最高但发布不满 14 天；25.1.30 与 26.3.27 都有驱动，下载量 800 > 500。
	rec := mustJSON(t, admin, "GET", "/api/v1/admin/kernels/xray/recommended", nil, 200)["recommended"].(map[string]any)
	if rec["version"] != "25.1.30" || rec["latest"] != "26.10.17" || rec["latest_driver"] != "xray-26.10" {
		t.Fatalf("recommended = %v", rec)
	}
	// 通知：arm64 还没有任何 Xray / sing-box 版本；mihomo 一个都没有；有新的 Weir 版本（测试构建不提示更新）。
	notices := mustJSON(t, admin, "GET", "/api/v1/admin/notices", nil, 200)["notices"].([]any)
	ids := map[string]map[string]any{}
	for _, n := range notices {
		m := n.(map[string]any)
		ids[m["id"].(string)] = m
	}
	missing, ok := ids["kernel-missing:xray:arm64"]
	if !ok || missing["action"].(map[string]any)["version"] != "25.1.30" {
		t.Fatalf("notices = %v", notices)
	}
	if _, ok := ids["kernel-missing:xray:amd64"]; ok {
		t.Fatal("amd64 already has xray")
	}
	// 下载推荐版本：一个任务下载两个架构，带进度与日志；完成后通知消失。
	job := mustJSON(t, admin, "POST", "/api/v1/admin/kernels/xray/fetch", map[string]any{"version": "25.1.30"}, 202)["job"].(map[string]any)
	done := waitJob(t, admin, job["id"].(string))
	if done["status"] != "done" || len(done["logs"].([]any)) < 2 {
		t.Fatalf("job = %v", done)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if _, _, err := s.kernelCache().Get("xray", "25.1.30", arch); err != nil {
			t.Fatalf("%s not cached: %v", arch, err)
		}
	}
	notices = mustJSON(t, admin, "GET", "/api/v1/admin/notices", nil, 200)["notices"].([]any)
	for _, n := range notices {
		if n.(map[string]any)["id"] == "kernel-missing:xray:arm64" {
			t.Fatal("notice must disappear once downloaded")
		}
	}
	// 忽略：同一问题不再出现。
	mustJSON(t, admin, "POST", "/api/v1/admin/notices/kernel-missing:mihomo:amd64/dismiss", nil, 200)
	for _, n := range mustJSON(t, admin, "GET", "/api/v1/admin/notices", nil, 200)["notices"].([]any) {
		if n.(map[string]any)["id"] == "kernel-missing:mihomo:amd64" {
			t.Fatal("dismissed notice reappeared")
		}
	}
	// 再下载同一版本：已缓存的架构跳过，不再请求。
	before := atomic.LoadInt32(downloads)
	job = mustJSON(t, admin, "POST", "/api/v1/admin/kernels/xray/fetch", map[string]any{"version": "25.1.30"}, 202)["job"].(map[string]any)
	waitJob(t, admin, job["id"].(string))
	if atomic.LoadInt32(downloads) != before {
		t.Fatal("cached arches must not be downloaded again")
	}
	// 删除：默认版本、被锁定或正在运行的不能删；没人用的可以删。
	mustJSON(t, admin, "PUT", "/api/v1/admin/kernels/defaults", map[string]any{"xray": "25.1.30"}, 200)
	if w := admin.do("DELETE", "/api/v1/admin/kernels/xray/versions/25.1.30", nil, nil); w.Code != 409 {
		t.Fatalf("delete default = %d", w.Code)
	}
	if w := admin.do("DELETE", "/api/v1/admin/kernels/xray/versions/26.3.27", nil, nil); w.Code != 200 {
		t.Fatalf("delete unused = %d %s", w.Code, w.Body.String())
	}
	// 设为默认：缺架构时开始下载并提示，不直接生效。
	w := admin.do("PUT", "/api/v1/admin/kernels/defaults", map[string]any{"xray": "26.3.27"}, nil)
	if body := decode(t, w); w.Code != 409 || body["job"] == nil {
		t.Fatalf("default without cache = %d %v", w.Code, body)
	}
	waitJob(t, admin, "kernel:xray:26.3.27")
	mustJSON(t, admin, "PUT", "/api/v1/admin/kernels/defaults", map[string]any{"xray": "26.3.27"}, 200)
}

func TestSelfUpdateEndpointsInDevelopmentBuild(t *testing.T) {
	s, admin, _ := setupGate(t)
	fakeReleases(t, s)
	st := mustJSON(t, admin, "GET", "/api/v1/admin/system/update", nil, 200)
	if st["blocker"] != "开发构建，不支持自更新" || st["latest"].(map[string]any)["version"] != "0.1.20" {
		t.Fatalf("status = %v", st)
	}
	// 改名之前的 0.1.12 不在可选版本里。
	if older := st["older"].([]any); len(older) != 0 {
		t.Fatalf("older = %v", older)
	}
	// 要当前密码；开发构建拒绝写请求。
	if w := admin.do("POST", "/api/v1/admin/system/update", map[string]any{"action": "update", "version": "0.1.20"}, nil); w.Code != 401 {
		t.Fatalf("without password = %d", w.Code)
	}
	if w := admin.do("POST", "/api/v1/admin/system/update", map[string]any{"action": "update", "version": "0.1.20", "password": "Passw0rd"}, nil); w.Code != 409 {
		t.Fatalf("development build = %d %s", w.Code, w.Body.String())
	}
}
