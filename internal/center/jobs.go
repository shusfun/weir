package center

import (
	"context"
	"errors"
	"log"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/store/ent/user"
)

// 后台任务：内核与 dsh 的下载、SSH 安装 Gate，以及可能超过反代时限的操作（见 action_jobs.go）。
// 任务只在中心进程内存里，中心重启后清空，页面查到 404 时如实说明；下载到一半的 .part 文件留在磁盘上，
// 再次下载时续传。同一个 id 正在运行时不重复启动。每次变化都唤醒事件流（events.go），推给能看见它的页面。

const (
	jobKeep    = time.Hour
	jobMaxLogs = 200
)

// Job 是一个后台任务的快照。
type Job struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`  // kernel / dsh / gate-release / gate-ssh / action
	Title    string          `json:"title"` // 给人看的名称，如「Xray 26.3.27」
	Status   string          `json:"status"`
	Progress *fetch.Progress `json:"progress,omitempty"`
	Step     string          `json:"step,omitempty"` // 当前步骤，如「下载 arm64」
	// StepAt / StepMax：当前步骤开始的时间与最多等多久（秒，0 表示说不准），页面据此写「已等 N 秒，最多 M 秒」。
	StepAt  *time.Time `json:"step_at,omitempty"`
	StepMax int        `json:"step_max,omitempty"`
	// Cancellable 为假：已经开始写入，取消会留下写了一半的数据，不再接受取消。
	Cancellable bool           `json:"cancellable"`
	Logs        []JobLog       `json:"logs"`
	Error       string         `json:"error,omitempty"`
	Result      map[string]any `json:"result,omitempty"`
	Started     time.Time      `json:"started_at"`
	Finished    *time.Time     `json:"finished_at,omitempty"`
	// Trace 是发起任务的那次请求的编号，与请求日志、审计对得上。
	Trace string `json:"trace,omitempty"`
	// Owner 是发起任务的用户（0 是系统任务）：页面据此只提示自己发起、又没在等的任务的结果。
	Owner int `json:"owner,omitempty"`
}

// JobLog 是一行任务日志。
type JobLog struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

const (
	jobRunning   = "running"
	jobDone      = "done"
	jobFailed    = "failed"
	jobCancelled = "cancelled"
)

type jobEntry struct {
	job Job
	// owner 是发起任务的用户；0 是系统任务（下载、安装），只有管理员能看。
	owner int
	// hidden：同步返回了结果的操作（action_jobs.go）在等待期内不进列表与事件流。
	hidden    bool
	cancelled bool
	cancelMsg string
	rev       uint64
	cancel    context.CancelFunc
	done      chan struct{}
}

type jobManager struct {
	mu     sync.Mutex
	jobs   map[string]*jobEntry
	rev    uint64
	notify chan struct{}
}

// jobSpec 描述要启动的任务。Owner 为 0 时只有管理员能看；CancelMsg 是取消后显示的说明。
type jobSpec struct {
	ID, Kind, Title string
	Owner           int
	Trace           string
	Hidden          bool
	CancelMsg       string
}

// JobRun 是任务函数可用的操作：报告进度、步骤与日志。
type JobRun struct {
	m *jobManager
	e *jobEntry
}

func (r JobRun) update(f func(*Job)) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	f(&r.e.job)
	r.m.touchLocked(r.e)
}

func (r JobRun) Log(text string) {
	r.update(func(j *Job) {
		j.Logs = append(j.Logs, JobLog{At: time.Now().UTC(), Text: text})
		if len(j.Logs) > jobMaxLogs {
			j.Logs = j.Logs[len(j.Logs)-jobMaxLogs:]
		}
	})
}

func (r JobRun) Progress(p fetch.Progress) {
	r.update(func(j *Job) { j.Progress = &p })
}

func (r JobRun) Step(step string) { r.StepFor(step, 0) }

// StepFor 开始一个最多要等 max 的步骤（0 表示说不准）。
func (r JobRun) StepFor(step string, max time.Duration) {
	now := time.Now().UTC()
	r.update(func(j *Job) {
		j.Step, j.Progress, j.StepAt, j.StepMax = step, nil, &now, int(max/time.Second)
	})
	r.Log(step)
}

func (r JobRun) SetTitle(title string) { r.update(func(j *Job) { j.Title = title }) }

// Commit 在第一次写入之前调用：之后不再接受取消（取消会留下写了一半的数据）。
// 已经被取消时返回 false，调用方应停下、不再写入。
func (r JobRun) Commit() bool {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	if r.e.cancelled {
		return false
	}
	if r.e.job.Cancellable {
		r.e.job.Cancellable = false
		r.m.touchLocked(r.e)
	}
	return true
}

// Download 用任务的进度与日志执行一次下载。
func (r JobRun) Download(ctx context.Context, req fetch.Request) error {
	req.OnProgress, req.Log = r.Progress, r.Log
	return fetch.Download(ctx, req)
}

// touchLocked 记下一次变化并唤醒事件流；调用方持有 m.mu。
func (m *jobManager) touchLocked(e *jobEntry) {
	m.rev++
	e.rev = m.rev
	if m.notify != nil {
		close(m.notify)
		m.notify = nil
	}
}

// wait 返回下一次任务变化时关闭的通道。
func (m *jobManager) wait() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.notify == nil {
		m.notify = make(chan struct{})
	}
	return m.notify
}

// Start 启动系统任务（只有管理员能看）；同 id 的任务正在运行时返回它，不重复启动。
func (m *jobManager) Start(id, kind, title string, run func(context.Context, JobRun) (map[string]any, error)) Job {
	return m.StartWith(jobSpec{ID: id, Kind: kind, Title: title}, run)
}

func (m *jobManager) StartWith(spec jobSpec, run func(context.Context, JobRun) (map[string]any, error)) Job {
	m.mu.Lock()
	if m.jobs == nil {
		m.jobs = map[string]*jobEntry{}
	}
	m.gc()
	if e, ok := m.jobs[spec.ID]; ok && e.job.Status == jobRunning {
		j := e.job
		m.mu.Unlock()
		return j
	}
	ctx, cancel := context.WithCancel(context.Background())
	msg := spec.CancelMsg
	if msg == "" {
		msg = "已取消；已下载的部分保留，再次下载时续传"
	}
	e := &jobEntry{
		job:   Job{ID: spec.ID, Kind: spec.Kind, Title: spec.Title, Status: jobRunning, Cancellable: true, Started: time.Now().UTC(), Logs: []JobLog{}, Trace: spec.Trace, Owner: spec.Owner},
		owner: spec.Owner, hidden: spec.Hidden, cancelMsg: msg, cancel: cancel, done: make(chan struct{}),
	}
	m.jobs[spec.ID] = e
	m.touchLocked(e)
	snap := e.job
	m.mu.Unlock()
	go func() {
		defer close(e.done)
		defer cancel()
		var res map[string]any
		var err error
		func() {
			// 任务在请求之外的 goroutine 里运行，没有 gin 的恢复中间件：panic 在这里接住，记成失败
			defer func() {
				if p := recover(); p != nil {
					log.Printf("ERROR panic job=%s kind=%s trace=%s err=%v\n%s", spec.ID, spec.Kind, spec.Trace, p, debug.Stack())
					err = errors.New("中心内部错误，见中心日志（任务 " + spec.ID + "）")
				}
			}()
			res, err = run(ctx, JobRun{m: m, e: e})
		}()
		now := time.Now().UTC()
		m.mu.Lock()
		defer m.mu.Unlock()
		j := &e.job
		j.Finished, j.Result, j.Step, j.StepAt, j.StepMax, j.Progress = &now, res, "", nil, 0, nil
		switch {
		case err == nil:
			// 取消晚了一步、操作已经做完：如实记为完成
			j.Status = jobDone
		case e.cancelled:
			j.Status, j.Error = jobCancelled, e.cancelMsg
		default:
			j.Status, j.Error = jobFailed, err.Error()
		}
		j.Cancellable = false
		m.touchLocked(e)
	}()
	return snap
}

// Wait 等待任务结束或 ctx 结束，返回当时的快照（用于需要同步结果的调用，如为异构 Gate 准备程序）。
func (m *jobManager) Wait(ctx context.Context, id string) (Job, bool) {
	m.mu.Lock()
	e, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return Job{}, false
	}
	select {
	case <-e.done:
	case <-ctx.Done():
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return snapshot(e), true
}

func snapshot(e *jobEntry) Job {
	j := e.job
	j.Logs = append([]JobLog(nil), e.job.Logs...)
	return j
}

// Get 不区分查看者（内部调用用）。
func (m *jobManager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.jobs[id]; ok {
		return snapshot(e), true
	}
	return Job{}, false
}

// jobViewer 是查看任务的人：管理员看全部，普通用户只看自己发起的。
type jobViewer struct {
	uid   int
	admin bool
}

func viewerOf(c *gin.Context) jobViewer {
	return jobViewer{uid: c.GetInt("user_id"), admin: c.GetString("role") == string(user.RoleAdmin)}
}

func (v jobViewer) sees(e *jobEntry) bool {
	return !e.hidden && (v.admin || (e.owner != 0 && e.owner == v.uid))
}

// GetFor 返回查看者能看到的任务；看不到的与不存在的一样返回 false。
func (m *jobManager) GetFor(v jobViewer, id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.jobs[id]; ok && v.sees(e) {
		return snapshot(e), true
	}
	return Job{}, false
}

var (
	errJobNotFound   = errors.New("job_not_found")
	errJobNotRunning = errors.New("job_not_running")
	errJobCommitted  = errors.New("job_committed")
)

// Cancel 取消查看者能看到的、还没开始写入的任务。
func (m *jobManager) Cancel(v jobViewer, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.jobs[id]
	switch {
	case !ok || !v.sees(e):
		return errJobNotFound
	case e.job.Status != jobRunning || e.cancelled:
		return errJobNotRunning
	case !e.job.Cancellable:
		return errJobCommitted
	}
	e.cancelled = true
	e.cancel()
	m.touchLocked(e)
	return nil
}

// Publish 让等待期内没有结束的操作进入列表与事件流（页面改为跟着任务看结果）。
func (m *jobManager) Publish(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	e.hidden = false
	m.touchLocked(e)
	return snapshot(e), true
}

// Forget 删除已经同步返回了结果的操作。
func (m *jobManager) Forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.jobs[id]; ok && e.job.Status != jobRunning {
		delete(m.jobs, id)
	}
}

// List 返回正在运行与最近一小时结束的任务（管理员用）：运行中的在前，其余新到旧。不含日志。
func (m *jobManager) List(kind string) []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gc()
	out := []Job{}
	for _, e := range m.jobs {
		if e.hidden || (kind != "" && e.job.Kind != kind) {
			continue
		}
		j := e.job
		j.Logs = nil
		out = append(out, j)
	}
	sort.Slice(out, func(i, k int) bool {
		if (out[i].Status == jobRunning) != (out[k].Status == jobRunning) {
			return out[i].Status == jobRunning
		}
		return out[i].Started.After(out[k].Started)
	})
	return out
}

// changedSince 返回查看者能看到、在 rev 之后有变化的任务（带日志），以及当前的版本号。
func (m *jobManager) changedSince(v jobViewer, rev uint64) ([]Job, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gc()
	var out []Job
	for _, e := range m.jobs {
		if e.rev > rev && v.sees(e) {
			out = append(out, snapshot(e))
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Started.Before(out[k].Started) })
	return out, m.rev
}

// Shutdown 在中心停止时取消还能取消的任务，最多等 wait；已经开始写入的任务不取消，等它写完。
func (m *jobManager) Shutdown(wait time.Duration) {
	m.mu.Lock()
	var running []*jobEntry
	for _, e := range m.jobs {
		if e.job.Status != jobRunning {
			continue
		}
		running = append(running, e)
		if e.job.Cancellable && !e.cancelled {
			e.cancelled, e.cancelMsg = true, "中心停止，任务已中止"
			e.cancel()
		}
	}
	m.mu.Unlock()
	deadline := time.After(wait)
	for _, e := range running {
		select {
		case <-e.done:
		case <-deadline:
			return
		}
	}
}

func (m *jobManager) gc() {
	for id, e := range m.jobs {
		if e.job.Finished != nil && time.Since(*e.job.Finished) > jobKeep {
			delete(m.jobs, id)
		}
	}
}

// 任务列表只给管理员；单个任务的详情与取消按查看者过滤：普通用户只能看、只能取消自己发起的（如链式代理的实测）。
func (s *Server) jobRoutes(auth, admin *gin.RouterGroup) {
	admin.GET("/jobs", func(c *gin.Context) { c.JSON(200, gin.H{"jobs": s.jobs.List(c.Query("kind"))}) })
	for _, g := range []*gin.RouterGroup{admin, auth.Group("/me")} {
		g.GET("/jobs/:id", s.getJob)
		g.POST("/jobs/:id/cancel", s.cancelJob)
	}
}

func (s *Server) getJob(c *gin.Context) {
	j, ok := s.jobs.GetFor(viewerOf(c), c.Param("id"))
	if !ok {
		failure(c, 404, "job_not_found")
		return
	}
	c.JSON(200, gin.H{"job": j})
}

func (s *Server) cancelJob(c *gin.Context) {
	switch err := s.jobs.Cancel(viewerOf(c), c.Param("id")); err {
	case nil:
		s.audit(c, "job.cancel", c.Param("id"), "ok")
		c.JSON(200, gin.H{"ok": true})
	case errJobNotFound:
		failure(c, 404, "job_not_found")
	default:
		failure(c, 409, err.Error())
	}
}
