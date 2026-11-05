package center

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/selfupdate"
)

// 自更新的中心一侧：查 GitHub 有没有新版本、预下载发布包、写请求、读 root 写回的进度。真正的下载与安装在
// internal/selfupdate（以 root 运行）。规则见 design.html「自更新」。

// weirRelease 是可以更新或切回的一个 Weir 版本。
type weirRelease struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
	Notes       string    `json:"notes"`
	URL         string    `json:"url"`
}

const updateCacheKey = "update-check"

// weirReleases 列出 GitHub 上 MinVersion 以后的正式版本（新到旧），结果缓存 20 分钟。
func (s *Server) weirReleases(ctx context.Context, force bool) ([]weirRelease, error) {
	if !force && s.Store != nil {
		if raw, ok, _ := s.Store.KV.Get(ctx, updateCacheKey); ok {
			var out []weirRelease
			if json.Unmarshal([]byte(raw), &out) == nil {
				return out, nil
			}
		}
	}
	if s.Store != nil && !force {
		if msg, ok, _ := s.Store.KV.Get(ctx, updateCacheKey+":error"); ok {
			return nil, errors.New(msg)
		}
	}
	list, err := s.weirGitHub().List(ctx, weirRepo(), 50)
	if err != nil {
		if s.Store != nil {
			_ = s.Store.KV.Set(ctx, updateCacheKey+":error", err.Error(), 10*time.Minute)
		}
		return nil, err
	}
	out := []weirRelease{}
	for _, r := range list {
		v := strings.TrimPrefix(r.Tag, "v")
		if r.Draft || r.Prerelease || !strings.HasPrefix(r.Tag, "v") {
			continue
		}
		if _, ok := buildinfo.ParseScheme(v); !ok || selfupdate.Compare(v, selfupdate.MinVersion) < 0 {
			continue
		}
		out = append(out, weirRelease{Version: v, PublishedAt: r.Published, Notes: r.Body, URL: r.HTMLURL})
	}
	sort.Slice(out, func(i, j int) bool { return selfupdate.Compare(out[i].Version, out[j].Version) > 0 })
	if s.Store != nil {
		if b, err := json.Marshal(out); err == nil {
			_ = s.Store.KV.Set(ctx, updateCacheKey, string(b), 20*time.Minute)
		}
	}
	return out, nil
}

// selfUpdateBlocker 返回不能在面板里自更新的原因；能更新时为空。
func selfUpdateBlocker() string {
	if !buildinfo.Release() {
		return "开发构建，不支持自更新"
	}
	role, _ := os.ReadFile(selfupdate.RoleFile)
	if r := strings.TrimSpace(string(role)); r != "standalone" && r != "center" {
		return "这台中心不是用安装脚本装的，请用安装脚本升级"
	}
	if st, err := os.Stat(selfupdate.Dir); err != nil || !st.IsDir() {
		return "缺少自更新目录，重新运行一次安装脚本即可"
	}
	probe := filepath.Join(selfupdate.Dir, ".probe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return "中心没有自更新目录的写权限，重新运行一次安装脚本即可"
	}
	_ = os.Remove(probe)
	return ""
}

func (s *Server) selfUpdateRoutes(admin *gin.RouterGroup) {
	admin.GET("/system/update", s.getSelfUpdate)
	admin.GET("/system/update/status", s.getSelfUpdateStatus)
	admin.POST("/system/update", s.postSelfUpdate)
}

// 更新服务多久没接手、root 多久没结束算卡住。root 的上限与 weir-update.service 的 TimeoutStartSec 一致，多留 1 分钟写结果。
var (
	updatePickup  = 60 * time.Second
	updateTimeout = 31 * time.Minute
)

// 一次更新经过的阶段。
const (
	updatePrefetch    = "prefetch"
	updateWaiting     = "waiting"
	updateDownloading = "downloading"
	updateInstalling  = "installing"
	updateDone        = "done"
	updateFailed      = "failed"
	updateCancelled   = "cancelled"
)

// updateRun 把一次更新合成一条记录：形状与后台任务相同（页面用同一个组件显示），多了阶段与版本。
// ID 是可取消的任务编号，只有预下载阶段才可取消。
type updateRun struct {
	Job
	Phase   string `json:"phase"`
	Action  string `json:"action"`
	From    string `json:"from"`
	Version string `json:"version"`
	// Stalled：更新服务一直没接手，下次发起更新时清掉那份请求。
	Stalled bool `json:"-"`
}

func updateTitle(action, from, version string) string {
	if version == "" {
		version = "上一版本"
	}
	if action == "rollback" {
		return "切回 " + from + " → " + version
	}
	return "更新 " + from + " → " + version
}

func newUpdateRun(phase, status, action, from, version string, started time.Time) *updateRun {
	return &updateRun{
		Job:   Job{ID: selfUpdateJob, Kind: "self-update", Title: updateTitle(action, from, version), Status: status, Started: started, Logs: []JobLog{}},
		Phase: phase, Action: action, From: from, Version: version,
	}
}

// statusLogs 把 root 写的「RFC3339 文字」日志解析成任务日志。
func statusLogs(lines []string) []JobLog {
	out := make([]JobLog, 0, len(lines))
	for _, l := range lines {
		ts, text, ok := strings.Cut(l, " ")
		at, err := time.Parse(time.RFC3339, ts)
		if !ok || err != nil {
			out = append(out, JobLog{Text: l})
			continue
		}
		out = append(out, JobLog{At: at, Text: text})
	}
	return out
}

func handoffLogs(h selfupdate.Handoff) []JobLog {
	out := make([]JobLog, 0, len(h.Logs))
	for _, l := range h.Logs {
		out = append(out, JobLog{At: l.At, Text: l.Text})
	}
	return out
}

// currentUpdateRun 读最近一次更新的记录：只读本机，不访问 GitHub，页面轮询靠它不被网络拖住。
func (s *Server) currentUpdateRun(now time.Time) *updateRun {
	st, hasStatus := selfupdate.ReadStatus()
	h, hasHandoff := selfupdate.ReadHandoff()
	if j, ok := s.jobs.Get(selfUpdateJob); ok {
		switch {
		case j.Status == jobRunning:
			version, _ := j.Output["version"].(string)
			r := newUpdateRun(updatePrefetch, jobRunning, "update", buildinfo.Version, version, j.Started)
			r.Step, r.Progress, r.Source, r.Logs, r.Cancellable, r.StepAt = j.Step, j.Progress, j.Source, j.Logs, j.Cancellable, j.StepAt
			return r
		case j.Status != jobDone && (!hasStatus || j.Started.After(st.Started)) && (!hasHandoff || j.Started.After(h.At)):
			// 预下载失败不会到这里（交给 root 继续）；只有取消或写不了请求
			version, _ := j.Output["version"].(string)
			phase := updateFailed
			if j.Status == jobCancelled {
				phase = updateCancelled
			}
			r := newUpdateRun(phase, j.Status, "update", buildinfo.Version, version, j.Started)
			r.Logs, r.Error, r.Finished, r.Source = j.Logs, j.Error, j.Finished, j.Source
			return r
		}
	}
	if hasHandoff && (!hasStatus || st.ID != h.ID && h.At.After(st.Started)) {
		r := newUpdateRun(updateWaiting, jobRunning, h.Action, h.From, h.Version, h.Started)
		r.Logs, r.Source = handoffLogs(h), h.Source
		r.Step = "等更新服务接手"
		if now.Sub(h.At) > updatePickup {
			r.Phase, r.Status, r.Step, r.Stalled = updateFailed, jobFailed, "", true
			r.Error = "更新服务超过 60 秒没有接手：请在服务器上检查 weir-update.path 是否在运行（systemctl status weir-update.path）"
			at := h.At.Add(updatePickup)
			r.Finished = &at
		}
		return r
	}
	if !hasStatus {
		return nil
	}
	r := newUpdateRun(st.State, jobRunning, st.Action, st.From, st.Version, st.Started)
	if hasHandoff && h.ID == st.ID {
		r.Logs, r.Source, r.Started = handoffLogs(h), h.Source, h.Started
	}
	r.Logs = append(r.Logs, statusLogs(st.Logs)...)
	if len(r.Logs) > jobMaxLogs+300 {
		r.Logs = r.Logs[len(r.Logs)-jobMaxLogs-300:]
	}
	r.Progress, r.Error, r.Finished = st.Progress, st.Error, st.Finished
	if st.Progress != nil && st.Progress.Source != "" {
		r.Source = st.Progress.Source
	}
	switch st.State {
	case "downloading":
		r.Step = "更新服务下载并校验发布包"
	case "installing":
		r.Step = "正在安装，服务会重启"
	case "done":
		r.Status = jobDone
	case "failed":
		r.Status = jobFailed
	}
	if st.Finished == nil && now.Sub(st.Started) > updateTimeout {
		at := st.Started.Add(updateTimeout)
		r.Phase, r.Status, r.Step, r.Finished, r.Progress = updateFailed, jobFailed, "", &at, nil
		r.Error = "更新服务超过 30 分钟没有结束：请在服务器上查看 journalctl -u weir-update.service"
	}
	return r
}

// updateBase 是不访问 GitHub 就能知道的部分。
func (s *Server) updateBase() gin.H {
	return gin.H{"current": buildinfo.Version, "blocker": selfUpdateBlocker(), "previous": selfupdate.Previous()}
}

// getSelfUpdateStatus 给页面轮询：本机版本与这次更新的进度。
func (s *Server) getSelfUpdateStatus(c *gin.Context) {
	out := s.updateBase()
	if r := s.currentUpdateRun(time.Now()); r != nil {
		out["run"] = r
	}
	c.JSON(200, out)
}

// getSelfUpdate 读 GitHub 上的版本（有缓存，force=1 时强制重读）。
func (s *Server) getSelfUpdate(c *gin.Context) {
	out := s.updateBase()
	list, err := s.weirReleases(c.Request.Context(), c.Query("force") == "1")
	if err != nil {
		out["check_error"] = err.Error()
		c.JSON(200, out)
		return
	}
	var older []weirRelease
	for _, r := range list {
		if buildinfo.Release() && selfupdate.Compare(r.Version, buildinfo.Version) < 0 && len(older) < 3 {
			older = append(older, r)
		}
	}
	if len(list) > 0 {
		out["latest"] = list[0]
		out["has_update"] = buildinfo.Release() && selfupdate.Compare(list[0].Version, buildinfo.Version) > 0
	}
	if older == nil {
		older = []weirRelease{}
	}
	out["older"] = older
	c.JSON(200, out)
}

// releaseKnown 判断版本在不在 GitHub 的正式版本里：先看缓存，查不到再读一次 GitHub。提交更新不必等网络，
// root 执行时还会自己核对版本与摘要。
func (s *Server) releaseKnown(ctx context.Context, version string) (bool, error) {
	has := func(list []weirRelease) bool {
		for _, r := range list {
			if r.Version == version {
				return true
			}
		}
		return false
	}
	if list, err := s.weirReleases(ctx, false); err == nil && has(list) {
		return true, nil
	}
	list, err := s.weirReleases(ctx, true)
	if err != nil {
		return false, err
	}
	return has(list), nil
}

// postSelfUpdate 写入更新请求：更新到某个正式版本（含最近 3 个旧版本），或切回本机保留的上一版本。
func (s *Server) postSelfUpdate(c *gin.Context) {
	var req struct {
		Action  string `json:"action"`
		Version string `json:"version"`
	}
	if !bind(c, &req) {
		return
	}
	if why := selfUpdateBlocker(); why != "" {
		failDetail(c, 409, "self_update_unavailable", why, nil)
		return
	}
	if r := s.currentUpdateRun(time.Now()); r != nil {
		if r.Status == jobRunning {
			failDetail(c, 409, "self_update_running", "已有一次更新正在进行", nil)
			return
		}
		if r.Stalled {
			if err := selfupdate.ClearRequest(); err != nil {
				failDetail(c, 409, "self_update_rejected", err.Error(), nil)
				return
			}
		}
	}
	version := strings.TrimPrefix(strings.TrimSpace(req.Version), "v")
	switch req.Action {
	case "update":
		found, err := s.releaseKnown(c.Request.Context(), version)
		if err != nil {
			failDetail(c, 502, "upstream_unavailable", "无法读取 GitHub 发布："+err.Error(), nil)
			return
		}
		if !found || version == buildinfo.Version {
			failDetail(c, 400, "invalid_version", "GitHub 上没有这个可用的正式版本", nil)
			return
		}
	case "rollback":
		if selfupdate.Previous() == "" {
			failDetail(c, 409, "no_previous", "本机没有可切回的上一版本", nil)
			return
		}
		version = ""
	default:
		failure(c, 400, "invalid_action")
		return
	}
	var rnd [8]byte
	_, _ = rand.Read(rnd[:])
	id := "web-" + hex.EncodeToString(rnd[:])
	target := version
	if target == "" {
		target = "上一版本 " + selfupdate.Previous()
	}
	if req.Action == "rollback" {
		now := time.Now().UTC()
		prev := selfupdate.Previous()
		if err := selfupdate.WriteHandoff(selfupdate.Handoff{ID: id, Action: "rollback", From: buildinfo.Version, Version: prev, Started: now, At: now}); err != nil {
			failDetail(c, 409, "self_update_rejected", err.Error(), nil)
			return
		}
		if err := selfupdate.WriteRequest(selfupdate.Request{ID: id, Action: req.Action}); err != nil {
			failDetail(c, 409, "self_update_rejected", err.Error(), nil)
			return
		}
		s.audit(c, "system.rollback", buildinfo.Version+" → "+target, "ok")
		c.JSON(202, gin.H{"id": id})
		return
	}
	set, err := s.githubSources(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "github_sources_unavailable", err)
		return
	}
	sources := ghrelease.HTTPOnly(set.Sources)
	if err := (selfupdate.Request{ID: id, Action: "update", Version: version, Sources: sources}).Valid(); err != nil {
		failDetail(c, 409, "self_update_rejected", err.Error(), nil)
		return
	}
	s.audit(c, "system.update", buildinfo.Version+" → "+target, "ok")
	job := s.startSelfUpdate(id, version, sources)
	c.JSON(202, gin.H{"id": id, "job": job})
}

// selfUpdateJob 是中心预下载发布包的任务；同一时间只有一个。
const selfUpdateJob = "self-update"

// startSelfUpdate 先按「GitHub 加速」的来源顺序（含经 VPS 中转）把发布包预下载到自更新目录（进度在任务里），
// 再写请求交给 root。预下载失败不阻止更新：root 会按直连与镜像自己下载（root 不经 VPS），失败原因写进日志。
func (s *Server) startSelfUpdate(id, version string, sources []ghrelease.Source) Job {
	return s.jobs.Start(selfUpdateJob, "self-update", "更新 Weir 到 "+version, func(ctx context.Context, run JobRun) (map[string]any, error) {
		run.SetOutput(map[string]any{"version": version})
		started := time.Now().UTC()
		name := selfupdate.PrefetchName(version, centerArch())
		selfupdate.CleanPrefetch(name)
		run.Step("中心预下载发布包")
		if err := s.fetchWeirPackage(ctx, run, version, centerArch(), filepath.Join(selfupdate.Dir, name)); err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			run.Log("预下载失败，改由更新服务按直连与镜像自己下载（更新服务不经 VPS）：" + err.Error())
			selfupdate.CleanPrefetch("")
		}
		run.Step("交给更新服务")
		h := selfupdate.Handoff{ID: id, Action: "update", From: buildinfo.Version, Version: version, Started: started, At: time.Now().UTC()}
		if j, ok := s.jobs.Get(selfUpdateJob); ok {
			h.Source = j.Source
			for _, l := range j.Logs {
				h.Logs = append(h.Logs, selfupdate.HandoffLog{At: l.At, Text: l.Text})
			}
		}
		// 先写交接记录再写请求：root 一接手，页面就能把这段日志接在前面
		if err := selfupdate.WriteHandoff(h); err != nil {
			return nil, err
		}
		if err := selfupdate.WriteRequest(selfupdate.Request{ID: id, Action: "update", Version: version, Sources: sources}); err != nil {
			return nil, err
		}
		return map[string]any{"id": id}, nil
	})
}
