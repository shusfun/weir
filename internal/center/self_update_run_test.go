package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/selfupdate"
	"github.com/shusfun/weir/packages/contracts"
)

func updateDir(t *testing.T) {
	t.Helper()
	old := selfupdate.Dir
	selfupdate.Dir = t.TempDir()
	t.Cleanup(func() { selfupdate.Dir = old })
}

func writeRootStatus(t *testing.T, st selfupdate.Status) {
	t.Helper()
	b, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(selfupdate.Dir, "status.json"), b, 0o640); err != nil {
		t.Fatal(err)
	}
}

// 预下载进行中：即使 status.json 里是上一次已结束的记录，也显示这次的进度（页面据此继续轮询）。
func TestUpdateRunPrefetchHidesFinishedStatus(t *testing.T) {
	updateDir(t)
	done := time.Now().Add(-time.Hour)
	writeRootStatus(t, selfupdate.Status{ID: "web-old", State: "done", Version: "0.1.1", Started: done, Finished: &done})
	s := &Server{}
	release := make(chan struct{})
	entered := make(chan struct{})
	s.jobs.Start(selfUpdateJob, "self-update", "x", func(ctx context.Context, run JobRun) (map[string]any, error) {
		run.SetOutput(map[string]any{"version": "0.1.2"})
		run.Step("中心预下载发布包")
		run.Progress(fetch.Progress{Done: 1, Total: 2, Source: "VPS 东京"})
		close(entered)
		<-release
		return nil, errors.New("写不了请求")
	})
	<-entered
	r := s.currentUpdateRun(time.Now())
	if r == nil || r.Phase != updatePrefetch || r.Status != jobRunning || r.Version != "0.1.2" || r.Source != "VPS 东京" || !r.Cancellable {
		t.Fatalf("prefetch run = %+v", r)
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	s.jobs.Wait(ctx, selfUpdateJob)
	if r := s.currentUpdateRun(time.Now()); r.Phase != updateFailed || r.Error != "写不了请求" {
		t.Fatalf("failed run = %+v", r)
	}
}

// 交接之后：更新服务没接手前是「等接手」，超过时限如实算失败并能重新发起；接手后日志接在预下载后面，
// 中心重启（任务记录没了）后仍然在。
func TestUpdateRunHandoffAndRestart(t *testing.T) {
	updateDir(t)
	s := &Server{}
	now := time.Now().UTC()
	h := selfupdate.Handoff{ID: "web-1", Action: "update", From: "0.1.1", Version: "0.1.2", Source: "VPS 东京", Started: now.Add(-20 * time.Second), At: now,
		Logs: []selfupdate.HandoffLog{{At: now, Text: "从 VPS 东京 下载"}}}
	if err := selfupdate.WriteHandoff(h); err != nil {
		t.Fatal(err)
	}
	r := s.currentUpdateRun(now)
	if r.Phase != updateWaiting || r.Status != jobRunning || r.Source != "VPS 东京" || len(r.Logs) != 1 {
		t.Fatalf("waiting = %+v", r)
	}
	late := s.currentUpdateRun(now.Add(updatePickup + time.Second))
	if late.Phase != updateFailed || late.Status != jobFailed || !late.Stalled || !strings.Contains(late.Error, "weir-update.path") || late.Finished == nil {
		t.Fatalf("stalled = %+v", late)
	}
	writeRootStatus(t, selfupdate.Status{ID: "web-1", Action: "update", From: "0.1.1", Version: "0.1.2", State: "installing", Started: now,
		Logs: []string{now.Format(time.RFC3339) + " 使用中心预下载的发布包"}})
	r = s.currentUpdateRun(now.Add(time.Minute))
	if r.Phase != updateInstalling || r.Status != jobRunning || len(r.Logs) != 2 || r.Logs[0].Text != "从 VPS 东京 下载" || r.Logs[1].Text != "使用中心预下载的发布包" || r.Source != "VPS 东京" {
		t.Fatalf("installing = %+v", r)
	}
	finished := now.Add(time.Minute)
	writeRootStatus(t, selfupdate.Status{ID: "web-1", Action: "update", From: "0.1.1", Version: "0.1.2", State: "done", Started: now, Finished: &finished})
	if r = s.currentUpdateRun(now.Add(2 * time.Minute)); r.Phase != updateDone || r.Status != jobDone || r.Source != "VPS 东京" {
		t.Fatalf("done = %+v", r)
	}
}

// root 卡住：不再一直显示进行中。
func TestUpdateRunRootTimeout(t *testing.T) {
	updateDir(t)
	s := &Server{}
	now := time.Now().UTC()
	writeRootStatus(t, selfupdate.Status{ID: "web-1", Action: "update", State: "downloading", Started: now.Add(-updateTimeout - time.Minute), Logs: []string{}})
	if r := s.currentUpdateRun(now); r.Status != jobFailed || !strings.Contains(r.Error, "30 分钟") {
		t.Fatalf("timeout = %+v", r)
	}
}

// 状态接口只读本机：GitHub 连不上也立即返回。
func TestUpdateStatusDoesNotTouchGitHub(t *testing.T) {
	s, admin, _ := setupGate(t)
	updateDir(t)
	hit := false
	s.GitHub = fakeGitHubThatFails(&hit)
	body := mustJSON(t, admin, "GET", "/api/v1/admin/system/update/status", nil, 200)
	if hit || body["current"] == nil || body["run"] != nil {
		t.Fatalf("status = %v hit=%v", body, hit)
	}
}

func TestCleanPrefetchKeepsCurrent(t *testing.T) {
	updateDir(t)
	for _, n := range []string{"weir-0.1.1-linux-amd64.tar.gz.part", "weir-0.1.2-linux-amd64.tar.gz", "weir-0.1.2-linux-amd64.tar.gz.part", "status.json"} {
		_ = os.WriteFile(filepath.Join(selfupdate.Dir, n), nil, 0o640)
	}
	selfupdate.CleanPrefetch("weir-0.1.2-linux-amd64.tar.gz")
	list, _ := os.ReadDir(selfupdate.Dir)
	if len(list) != 3 {
		t.Fatalf("left = %v", list)
	}
	selfupdate.CleanPrefetch("")
	if list, _ = os.ReadDir(selfupdate.Dir); len(list) != 1 {
		t.Fatalf("left = %v", list)
	}
}

// 内核自动设默认：唯一的版本才设；已有默认不改；有多个版本由管理员选。
func TestEnsureKernelDefault(t *testing.T) {
	s, _, _ := setupGate(t)
	ctx := context.Background()
	if s.ensureKernelDefault(ctx, "mihomo") {
		t.Fatal("no version should not set default")
	}
	if _, err := s.kernelCache().Put("mihomo", "1.19.27", "amd64", []byte("m")); err != nil {
		t.Fatal(err)
	}
	for _, a := range s.kernelArches(ctx, "mihomo") {
		if _, err := s.kernelCache().Put("mihomo", "1.19.27", a, []byte("m")); err != nil {
			t.Fatal(err)
		}
	}
	if !s.ensureKernelDefault(ctx, "mihomo") || s.explicitDefault(ctx, "mihomo") != "1.19.27" {
		t.Fatalf("default = %q", s.explicitDefault(ctx, "mihomo"))
	}
	if _, err := s.kernelCache().Put("mihomo", "1.20.0", "amd64", []byte("n")); err != nil {
		t.Fatal(err)
	}
	if s.ensureKernelDefault(ctx, "mihomo") || s.explicitDefault(ctx, "mihomo") != "1.19.27" {
		t.Fatal("existing default must stay")
	}
	// 有多个版本、没有默认：不替管理员选
	if _, err := s.kernelCache().Put("sing-box", "1.1.0", "amd64", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.kernelCache().Put("sing-box", "1.2.0", "amd64", []byte("b")); err != nil {
		t.Fatal(err)
	}
	if s.ensureKernelDefault(ctx, "sing-box") {
		t.Fatal("several versions must not auto default")
	}
	// 没有适用驱动：不设
	if _, err := s.kernelCache().Put("xray", "0.0.1", "amd64", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if s.ensureKernelDefault(ctx, "xray") {
		t.Fatal("no driver must not auto default")
	}
}

// 中转的两段进度：VPS 下载阶段经上报接口，传回阶段按收到的字节；上报只接受发起的那台 Gate，开始上传后的旧上报丢弃。
func TestRelayProgressBothStages(t *testing.T) {
	s, admin, g := setupGate(t)
	other := pairWithToken(t, admin, decode(t, admin.do("POST", "/api/v1/admin/gates/invite", map[string]string{"name": "JP"}, nil))["token"].(string))
	other.heartbeat("test")
	content := []byte(strings.Repeat("r", 1000))
	dest := filepath.Join(t.TempDir(), "asset.part")
	events := make(chan string, 64)
	rep := fetch.Reporter{
		Step:     func(step string) { events <- "step:" + step },
		Progress: func(done, total, speed int64) { events <- "p:" + itoa(int(done)) + "/" + itoa(int(total)) },
	}
	done := make(chan error, 1)
	go func() {
		done <- s.relayFetch(context.Background(), int(g.id), contractsAsset(content), dest, rep)
	}()
	a := g.nextAction("asset.fetch")
	id, _ := a.Payload["upload_id"].(string)
	path := "/api/v1/agent/uploads/" + id
	if code, _ := other.call("POST", path+"/progress", map[string]any{"done": 1, "total": 2}); code != 404 {
		t.Fatalf("other gate progress = %d", code)
	}
	if code, body := g.call("POST", path+"/progress", map[string]any{"done": 500, "total": 1000, "source": "gh-proxy.com"}); code != 200 {
		t.Fatalf("progress = %d %s", code, body)
	}
	if code, body := g.rawCall("PUT", path+"?offset=0", content); code != 200 {
		t.Fatalf("chunk = %d %s", code, body)
	}
	if code, _ := g.call("POST", path+"/progress", map[string]any{"done": 600, "total": 1000}); code != 200 {
		t.Fatalf("late progress = %d", code)
	}
	g.finish(a, true, "", map[string]any{"size": len(content)})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	close(events)
	var got []string
	for e := range events {
		got = append(got, e)
	}
	all := strings.Join(got, "|")
	for _, want := range []string{"下载中（gh-proxy.com）", "p:500/1000", "传回中心", "p:1000/1000"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in %s", want, all)
		}
	}
	if strings.Contains(all, "p:600/1000") {
		t.Fatalf("late progress not dropped: %s", all)
	}
}

func contractsAsset(content []byte) contracts.AssetFetch {
	s := sha256.Sum256(content)
	return contracts.AssetFetch{URLs: []string{"https://github.com/a/b/releases/download/v1/x"}, SHA256: hex.EncodeToString(s[:]), Size: int64(len(content)), MaxSize: 1 << 20}
}

// fakeGitHubThatFails：任何访问都记下来并失败。
func fakeGitHubThatFails(hit *bool) *ghrelease.Client {
	return &ghrelease.Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		*hit = true
		return nil, errors.New("unreachable")
	})}}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// 「下载并设为默认」：成功后设为默认（即使已有别的默认）；下载失败时默认不变。
func TestFetchWithSetDefault(t *testing.T) {
	s, admin, _ := setupGate(t)
	fakeReleases(t, s)
	ctx := context.Background()
	mustJSON(t, admin, "PUT", "/api/v1/admin/kernels/defaults", map[string]any{"xray": "26.3.27"}, 200)
	job := mustJSON(t, admin, "POST", "/api/v1/admin/kernels/xray/fetch", map[string]any{"version": "25.1.30", "set_default": true}, 202)["job"].(map[string]any)
	waitJob(t, admin, job["id"].(string))
	if got := s.explicitDefault(ctx, "xray"); got != "25.1.30" {
		t.Fatalf("default after fetch = %q", got)
	}
	// 没有这个版本：下载失败，默认不变，也不留下「成功后设默认」的标记
	job = mustJSON(t, admin, "POST", "/api/v1/admin/kernels/xray/fetch", map[string]any{"version": "9.9.9", "set_default": true}, 202)["job"].(map[string]any)
	waitJob(t, admin, job["id"].(string))
	if got := s.explicitDefault(ctx, "xray"); got != "25.1.30" {
		t.Fatalf("default after failure = %q", got)
	}
	if _, left := s.kernelDefaultAfter.Load("kernel:xray:9.9.9"); left {
		t.Fatal("pending default left behind")
	}
}
