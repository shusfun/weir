package center

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/packages/contracts"
)

func shortInline(t *testing.T) {
	before := actionInline
	actionInline = 50 * time.Millisecond
	t.Cleanup(func() { actionInline = before })
}

// 超过等待期的操作改成后台任务：返回 202 和任务，浏览器断开后照常做完，结果是处理函数原本的响应。
func TestSlowActionContinuesAsJobAfterDisconnect(t *testing.T) {
	s, admin, g := setupGate(t)
	release, started := make(chan struct{}), make(chan struct{}, 1)
	g.serveActions(t, s, map[string]func(contracts.Action) contracts.ActionResult{
		contracts.ActionPanelConfigure: func(a contracts.Action) contracts.ActionResult {
			started <- struct{}{}
			<-release
			return contracts.ActionResult{OK: true, Result: map[string]any{"version": "v2.0.16"}}
		},
	})
	path := "/api/v1/admin/gates/" + itoa(int(g.id)) + "/panel"
	// 字段校验仍然同步返回
	mustJSON(t, admin, "PUT", path, map[string]any{"port": 0, "api_key": "panel-api-key-123", "password": "Passw0rd"}, 400)
	if jobs := s.jobs.List("action"); len(jobs) != 0 {
		t.Fatalf("a request answered inline must not leave a job: %v", jobs)
	}
	// Gate 开始执行后浏览器断开（反代超时也一样）：请求马上返回 202，操作照常进行
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("PUT", "https://example.com"+path, strings.NewReader(`{"port":10086,"api_key":"panel-api-key-123","password":"Passw0rd"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin.cookie)
	go func() { <-started; cancel() }()
	w := httptest.NewRecorder()
	admin.router.ServeHTTP(w, req)
	if w.Code != 202 {
		t.Fatalf("slow action = %d %s", w.Code, w.Body.String())
	}
	job := decode(t, w)["job"].(map[string]any)
	id := job["id"].(string)
	if job["kind"] != "action" || job["status"] != "running" || job["cancellable"] != false || !strings.Contains(job["title"].(string), "1Panel") {
		t.Fatalf("job = %v", job)
	}
	// 已经发给 Gate：不再接受取消
	if w := admin.do("POST", "/api/v1/me/jobs/"+id+"/cancel", nil, nil); w.Code != 409 || !strings.Contains(w.Body.String(), "job_committed") {
		t.Fatalf("cancel committed job = %d %s", w.Code, w.Body.String())
	}
	step := mustJSON(t, admin, "GET", "/api/v1/me/jobs/"+id, nil, 200)["job"].(map[string]any)
	if !strings.Contains(step["step"].(string), "1Panel") || step["step_max"] != float64(60) || step["step_at"] == nil {
		t.Fatalf("step = %v", step)
	}
	close(release)
	done := waitJob(t, admin, id)
	res, _ := done["result"].(map[string]any)
	body, _ := res["body"].(map[string]any)
	if done["status"] != "done" || res["status"] != float64(200) || body["version"] != "v2.0.16" {
		t.Fatalf("finished job = %v", done)
	}
	if n, _ := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("gate.panel.set")).Count(t.Context()); n != 1 {
		t.Fatalf("audit after background finish = %d", n)
	}
}

// 任务按发起人过滤：普通用户只能看、只能取消自己的；系统任务只有管理员能看；同步返回的操作在等待期内谁也看不到。
func TestJobVisibilityAndCancel(t *testing.T) {
	var m jobManager
	owner, other, admin := jobViewer{uid: 5}, jobViewer{uid: 6}, jobViewer{uid: 1, admin: true}
	block := make(chan struct{})
	j := m.StartWith(jobSpec{ID: "action:a", Kind: "action", Title: "t", Owner: 5, Hidden: true, CancelMsg: "已取消，没有保存任何改动"}, func(ctx context.Context, run JobRun) (map[string]any, error) {
		select {
		case <-block:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if _, ok := m.GetFor(owner, j.ID); ok {
		t.Fatal("hidden job visible before publish")
	}
	if list, _ := m.changedSince(admin, 0); len(list) != 0 || len(m.List("")) != 0 {
		t.Fatal("hidden job listed before publish")
	}
	m.Publish(j.ID)
	if _, ok := m.GetFor(owner, j.ID); !ok {
		t.Fatal("owner cannot see own job")
	}
	if _, ok := m.GetFor(admin, j.ID); !ok {
		t.Fatal("admin cannot see job")
	}
	if _, ok := m.GetFor(other, j.ID); ok {
		t.Fatal("another user sees the job")
	}
	if err := m.Cancel(other, j.ID); err != errJobNotFound {
		t.Fatalf("other cancel = %v", err)
	}
	if err := m.Cancel(owner, j.ID); err != nil {
		t.Fatalf("owner cancel = %v", err)
	}
	got, _ := m.Wait(context.Background(), j.ID)
	if got.Status != jobCancelled || got.Error != "已取消，没有保存任何改动" {
		t.Fatalf("cancelled = %+v", got)
	}
	sys := m.Start("kernel:x", "kernel", "Xray", func(context.Context, JobRun) (map[string]any, error) { return nil, nil })
	if _, ok := m.GetFor(owner, sys.ID); ok {
		t.Fatal("system job visible to a normal user")
	}
	// 取消晚了一步、操作已经做完：记为完成
	late := m.StartWith(jobSpec{ID: "action:b", Kind: "action", Owner: 5}, func(ctx context.Context, run JobRun) (map[string]any, error) {
		<-block
		return map[string]any{"status": 200}, nil
	})
	if err := m.Cancel(owner, late.ID); err != nil {
		t.Fatal(err)
	}
	close(block)
	if got, _ := m.Wait(context.Background(), late.ID); got.Status != jobDone {
		t.Fatalf("late cancel = %+v", got)
	}
	// panic 记成失败，不让中心崩溃
	p := m.Start("p", "action", "p", func(context.Context, JobRun) (map[string]any, error) { panic("boom") })
	if got, _ := m.Wait(context.Background(), p.ID); got.Status != jobFailed || !strings.Contains(got.Error, "中心内部错误") {
		t.Fatalf("panic = %+v", got)
	}
}

// 中心重启后任务的记录不在了：查任务得到 404 job_not_found，页面据此说明并停止轮询。
func TestJobNotFoundAfterRestart(t *testing.T) {
	_, admin := newInstalledServer(t)
	w := admin.do("GET", "/api/v1/me/jobs/action:gone", nil, nil)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "job_not_found") {
		t.Fatalf("missing job = %d %s", w.Code, w.Body.String())
	}
}

// readEvents 读事件流，直到收到 want 事件；返回它的数据。
func readEvents(t *testing.T, sc *bufio.Scanner, want string) string {
	t.Helper()
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: ") && event == want:
			return strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatalf("stream ended before %q: %v", want, sc.Err())
	return ""
}

// 事件流：hello、配对与草稿的提醒（只给管理员）、任务的变化；带上次的编号重连时不重置。
func TestConsoleEventStream(t *testing.T) {
	s, admin := newInstalledServer(t)
	srv := httptest.NewServer(admin.router)
	t.Cleanup(func() { s.StopStreams(); srv.Close() })
	open := func(last string) (*bufio.Scanner, func()) {
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/me/events", nil)
		req.AddCookie(admin.cookie)
		if last != "" {
			req.Header.Set("Last-Event-ID", last)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 || resp.Header.Get("X-Accel-Buffering") != "no" {
			t.Fatalf("events = %v %v", resp, err)
		}
		return bufio.NewScanner(resp.Body), func() { cancel(); resp.Body.Close() }
	}
	sc, stop := open("")
	if hello := readEvents(t, sc, "hello"); !strings.Contains(hello, `"reset":true`) || !strings.Contains(hello, `"admin":true`) {
		t.Fatalf("hello = %s", hello)
	}
	s.notifyConsole("pairing", "abc")
	if n := readEvents(t, sc, "nudge"); !strings.Contains(n, `"kind":"pairing"`) || !strings.Contains(n, `"key":"abc"`) {
		t.Fatalf("nudge = %s", n)
	}
	release := make(chan struct{})
	s.jobs.StartWith(jobSpec{ID: "action:ev", Kind: "action", Title: "测试任务", Owner: 1}, func(ctx context.Context, run JobRun) (map[string]any, error) {
		run.StepFor("正在等", time.Minute)
		<-release
		return nil, nil
	})
	if j := readEvents(t, sc, "job"); !strings.Contains(j, `"id":"action:ev"`) {
		t.Fatalf("job = %s", j)
	}
	close(release)
	for {
		if j := readEvents(t, sc, "job"); strings.Contains(j, `"status":"done"`) {
			break
		}
	}
	stop()
	// 带上次的编号重连：不重置，只补发之后的提醒
	s.notifyConsole("drafts", "")
	_, seq, _ := s.events.since(^uint64(0))
	_, rev := s.jobs.changedSince(jobViewer{admin: true}, 0)
	sc, stop = open(s.events.bootID() + "." + itoa(int(rev)) + "." + itoa(int(seq-1)))
	defer stop()
	if hello := readEvents(t, sc, "hello"); !strings.Contains(hello, `"reset":false`) {
		t.Fatalf("resumed hello = %s", hello)
	}
	if n := readEvents(t, sc, "nudge"); !strings.Contains(n, `"kind":"drafts"`) {
		t.Fatalf("missed nudge = %s", n)
	}
	// 中心停止时长连接立即结束
	s.StopStreams()
	for sc.Scan() {
	}
}

// 正在失败的周期任务进管理员通知，写原因与开始时间；恢复后消失，再失败时是一条新通知。
func TestFailingPeriodicTaskBecomesNotice(t *testing.T) {
	var tl taskLog
	tl.note("check-alerts", errors.New("query https://user:pw@example.com/x failed"))
	tl.note("refresh-sources", errors.New("shown on the source itself"))
	list := tl.notices()
	if len(list) != 1 || !strings.HasPrefix(list[0].ID, "task-failed:check-alerts:") || !strings.Contains(list[0].Title, "离线与额度提醒") {
		t.Fatalf("notices = %+v", list)
	}
	if strings.Contains(list[0].Detail, "pw@") || !strings.Contains(list[0].Extra["reason"].(string), "开始失败") {
		t.Fatalf("notice detail = %+v", list[0])
	}
	first := list[0].ID
	tl.note("check-alerts", errors.New("another reason"))
	if list := tl.notices(); len(list) != 1 || list[0].ID != first || list[0].Detail != "another reason" {
		t.Fatalf("same failure streak keeps its start: %+v", list)
	}
	tl.note("check-alerts", nil)
	if list := tl.notices(); len(list) != 0 {
		t.Fatalf("recovered task still noticed: %+v", list)
	}
}

// 「复制安装命令」的配对进度：等待执行 → 已配对、等第一次心跳 → 已上线；凭证本身不出现在地址里。
func TestPairingStatusByDigest(t *testing.T) {
	_, admin := newInstalledServer(t)
	inv := mustJSON(t, admin, "POST", "/api/v1/admin/gates/invite", map[string]string{"name": "HK-2"}, 201)
	token, sum := inv["token"].(string), inv["pairing"].(string)
	if sum != digest(token) {
		t.Fatalf("pairing = %v", inv)
	}
	path := "/api/v1/admin/gates/pairing/" + sum
	if st := mustJSON(t, admin, "GET", path, nil, 200); st["state"] != "waiting" {
		t.Fatalf("before pairing = %v", st)
	}
	// 凭证本身不是查询键：拿凭证查不到任何配对
	if st := mustJSON(t, admin, "GET", "/api/v1/admin/gates/pairing/"+token, nil, 200); st["state"] != "expired" {
		t.Fatalf("query by token = %v", st)
	}
	g := pairWithToken(t, admin, token)
	if st := mustJSON(t, admin, "GET", path, nil, 200); st["state"] != "paired" || st["gate_id"] != float64(g.id) {
		t.Fatalf("after pairing = %v", st)
	}
	heartbeat(t, g, contracts.Heartbeat{})
	if st := mustJSON(t, admin, "GET", path, nil, 200); st["state"] != "online" {
		t.Fatalf("after heartbeat = %v", st)
	}
	if st := mustJSON(t, admin, "GET", "/api/v1/admin/gates/pairing/"+digest("never-issued"), nil, 200); st["state"] != "expired" {
		t.Fatalf("unknown = %v", st)
	}
}

// Gate 自更新失败的原因经心跳记到中心，VPS 卡片显示；VPS 详情带上中心版本，页面据此标出没跟上的。
func TestGateSelfUpdateFailureShown(t *testing.T) {
	_, admin, g := setupGate(t)
	heartbeat(t, g, contracts.Heartbeat{SelfUpdate: &contracts.SelfUpdateStatus{Version: "0.1.32", Error: "从中心下载程序失败：HTTP 404", At: time.Now()}})
	v := mustJSON(t, admin, "GET", "/api/v1/admin/gates/"+itoa(int(g.id)), nil, 200)
	su, _ := v["self_update"].(map[string]any)
	if su["version"] != "0.1.32" || !strings.Contains(su["error"].(string), "404") || v["center_version"] == nil || v["version"] != "test" {
		t.Fatalf("gate = %v", v)
	}
	heartbeat(t, g, contracts.Heartbeat{})
	if v := mustJSON(t, admin, "GET", "/api/v1/admin/gates/"+itoa(int(g.id)), nil, 200); v["self_update"] != nil {
		t.Fatalf("self_update not cleared: %v", v["self_update"])
	}
}

// SSH 连接超时写明是 TCP 连不通：ping 得通不代表 SSH 端口连得上。
func TestSSHTimeoutReason(t *testing.T) {
	msg := sshReason(errors.New("dial tcp 203.0.113.9:22: i/o timeout"))
	if !strings.Contains(msg, "TCP") || !strings.Contains(msg, "ping") || strings.Contains(msg, "连不上这台 VPS 的 SSH 端口") {
		t.Fatalf("reason = %s", msg)
	}
}
