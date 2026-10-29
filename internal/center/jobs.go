package center

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/fetch"
)

// 后台任务：内核与 dsh 的下载等耗时操作。任务只在中心进程内存里，中心重启后列表清空；
// 下载到一半的 .part 文件留在磁盘上，再次下载时续传。同一个 id 正在运行时不重复启动。

const (
	jobKeep    = time.Hour
	jobMaxLogs = 200
)

// Job 是一个后台任务的快照。
type Job struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`  // kernel / dsh / gate-release
	Title    string          `json:"title"` // 给人看的名称，如「Xray 26.3.27」
	Status   string          `json:"status"`
	Progress *fetch.Progress `json:"progress,omitempty"`
	Step     string          `json:"step,omitempty"` // 当前步骤，如「下载 arm64」
	Logs     []JobLog        `json:"logs"`
	Error    string          `json:"error,omitempty"`
	Result   map[string]any  `json:"result,omitempty"`
	Started  time.Time       `json:"started_at"`
	Finished *time.Time      `json:"finished_at,omitempty"`
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
	job    Job
	cancel context.CancelFunc
	done   chan struct{}
}

type jobManager struct {
	mu   sync.Mutex
	jobs map[string]*jobEntry
}

// JobRun 是任务函数可用的操作：报告进度、步骤与日志。
type JobRun struct {
	m  *jobManager
	id string
}

func (r JobRun) Log(text string) {
	r.m.update(r.id, func(j *Job) {
		j.Logs = append(j.Logs, JobLog{At: time.Now().UTC(), Text: text})
		if len(j.Logs) > jobMaxLogs {
			j.Logs = j.Logs[len(j.Logs)-jobMaxLogs:]
		}
	})
}

func (r JobRun) Progress(p fetch.Progress) {
	r.m.update(r.id, func(j *Job) { j.Progress = &p })
}

func (r JobRun) Step(step string) {
	r.m.update(r.id, func(j *Job) { j.Step, j.Progress = step, nil })
	r.Log(step)
}

// Download 用任务的进度与日志执行一次下载。
func (r JobRun) Download(ctx context.Context, req fetch.Request) error {
	req.OnProgress, req.Log = r.Progress, r.Log
	return fetch.Download(ctx, req)
}

func (m *jobManager) update(id string, f func(*Job)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.jobs[id]; ok {
		f(&e.job)
	}
}

// Start 启动任务；同 id 的任务正在运行时返回它，不重复启动。
func (m *jobManager) Start(id, kind, title string, run func(context.Context, JobRun) (map[string]any, error)) Job {
	m.mu.Lock()
	if m.jobs == nil {
		m.jobs = map[string]*jobEntry{}
	}
	m.gc()
	if e, ok := m.jobs[id]; ok && e.job.Status == jobRunning {
		j := e.job
		m.mu.Unlock()
		return j
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &jobEntry{job: Job{ID: id, Kind: kind, Title: title, Status: jobRunning, Started: time.Now().UTC(), Logs: []JobLog{}}, cancel: cancel, done: make(chan struct{})}
	m.jobs[id] = e
	snap := e.job
	m.mu.Unlock()
	go func() {
		defer close(e.done)
		defer cancel()
		res, err := run(ctx, JobRun{m: m, id: id})
		now := time.Now().UTC()
		m.update(id, func(j *Job) {
			j.Finished, j.Result = &now, res
			switch {
			case err == nil:
				j.Status = jobDone
			case ctx.Err() != nil:
				j.Status, j.Error = jobCancelled, "已取消；已下载的部分保留，再次下载时续传"
			default:
				j.Status, j.Error = jobFailed, err.Error()
			}
		})
	}()
	return snap
}

// Wait 等待任务结束（用于需要同步结果的调用，如为异构 Gate 准备程序）。
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
	return m.Get(id)
}

func (m *jobManager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.jobs[id]; ok {
		j := e.job
		j.Logs = append([]JobLog(nil), e.job.Logs...)
		return j, true
	}
	return Job{}, false
}

func (m *jobManager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.jobs[id]; ok && e.job.Status == jobRunning {
		e.cancel()
		return true
	}
	return false
}

// List 返回正在运行与最近一小时结束的任务：运行中的在前，其余新到旧。不含日志。
func (m *jobManager) List(kind string) []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gc()
	out := []Job{}
	for _, e := range m.jobs {
		if kind != "" && e.job.Kind != kind {
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

func (m *jobManager) gc() {
	for id, e := range m.jobs {
		if e.job.Finished != nil && time.Since(*e.job.Finished) > jobKeep {
			delete(m.jobs, id)
		}
	}
}

func (s *Server) jobRoutes(admin *gin.RouterGroup) {
	admin.GET("/jobs", func(c *gin.Context) { c.JSON(200, gin.H{"jobs": s.jobs.List(c.Query("kind"))}) })
	admin.GET("/jobs/:id", func(c *gin.Context) {
		j, ok := s.jobs.Get(c.Param("id"))
		if !ok {
			failure(c, 404, "job_not_found")
			return
		}
		c.JSON(200, gin.H{"job": j})
	})
	admin.POST("/jobs/:id/cancel", func(c *gin.Context) {
		if !s.jobs.Cancel(c.Param("id")) {
			failure(c, 409, "job_not_running")
			return
		}
		s.audit(c, "job.cancel", c.Param("id"), "ok")
		c.JSON(200, gin.H{"ok": true})
	})
}
