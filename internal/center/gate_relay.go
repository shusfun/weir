package center

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/store/ent/gateaction"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

var (
	relayPickup = 2 * time.Minute
	relayCheck  = 10 * time.Second
)

// githubClientHTTP 是访问 GitHub 与镜像的 HTTP 客户端（测试时用注入的）。
func (s *Server) githubClientHTTP() *http.Client {
	if s.GitHub != nil && s.GitHub.HTTP != nil {
		return s.GitHub.HTTP
	}
	return githubHTTP()
}

// downloadGitHub 返回下载用的 GitHub 客户端与来源顺序。测试注入 Server.GitHub 时用它自己的 Sources
// （没写就只直连注入的主机），不让测试按默认镜像访问外网。
func (s *Server) downloadGitHub(ctx context.Context) (ghrelease.Client, []ghrelease.Source, error) {
	if s.GitHub != nil {
		gh := *s.GitHub
		if len(gh.Sources) == 0 {
			gh.Sources = []ghrelease.Source{{Kind: ghrelease.SourceDirect}}
		}
		return gh, gh.Sources, nil
	}
	set, err := s.githubSources(ctx)
	if err != nil {
		return ghrelease.Client{}, nil, fmt.Errorf("读取 GitHub 加速设置失败：%w", err)
	}
	return ghrelease.Client{HTTP: githubHTTP(), Query: s.queryReleases, Sources: set.Sources}, set.Sources, nil
}

// kernelFetcher 是下载内核用的 Fetcher：按「GitHub 加速」的顺序，gate 来源经 VPS 中转。
func (s *Server) kernelFetcher(ctx context.Context) (kernels.Fetcher, error) {
	f := s.fetcher()
	gh, sources, err := s.downloadGitHub(ctx)
	if err != nil {
		return f, err
	}
	f.Sources = sources
	f.Relay = func(raw, sha string, size, max int64) []fetch.Source {
		return s.relaySources(gh, sources, raw, sha, size, max)
	}
	return f, nil
}

// relaySources 把来源里的 gate 项变成 fetch.Source，按 Before 保持它在设置里相对 HTTP 来源的位置。
// VPS 按 HTTP 来源的顺序下载 raw；没有 HTTP 来源时 VPS 也无处可取，不生成中转。
func (s *Server) relaySources(gh ghrelease.Client, sources []ghrelease.Source, raw, sha string, size, max int64) []fetch.Source {
	httpOnly := gh
	httpOnly.Sources = ghrelease.HTTPOnly(sources)
	if len(httpOnly.Sources) == 0 {
		return nil
	}
	urls, err := httpOnly.URLs(raw)
	if err != nil || len(urls) == 0 {
		return nil
	}
	var out []fetch.Source
	seenHTTP := 0
	for _, src := range sources {
		if src.Kind != ghrelease.SourceGate {
			seenHTTP++
			continue
		}
		before := seenHTTP + 1 // HTTP 来源与 URLs 一一对应：设置里排在它前面的 HTTP 来源有 seenHTTP 个
		gid := int(src.GateID)
		name := "VPS " + strconv.Itoa(gid)
		if g, err := s.Store.Ent.Gate.Get(context.Background(), gid); err == nil {
			name = "VPS " + g.Name
		}
		out = append(out, fetch.Source{Name: name, Unmetered: true, Before: before, Fetch: func(ctx context.Context, dest string, rep fetch.Reporter) error {
			return s.relayFetch(ctx, gid, contracts.AssetFetch{URLs: urls, SHA256: sha, Size: size, MaxSize: max}, dest, rep)
		}})
	}
	return out
}

// relayFetch uses durable, gate-bound chunks only for the final fallback.
// The destination is materialized after task completion and local verification.
func (s *Server) relayFetch(ctx context.Context, gid int, req contracts.AssetFetch, dest string, rep fetch.Reporter) error {
	g, err := s.Store.Ent.Gate.Get(ctx, gid)
	if err != nil {
		return errors.New("这台 VPS 已删除")
	}
	if !gateOnline(g) {
		return errors.New("VPS 离线")
	}
	session, err := s.openRelay(ctx, gid, req)
	if err != nil {
		if session.ID != "" && session.ConsumerToken != "" {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			s.tasks.note("asset-relay-close", s.closeRelay(cleanup, session.ID, gid, session.ConsumerToken))
		}
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		closeErr := s.closeRelay(cleanup, session.ID, gid, session.ConsumerToken)
		s.tasks.note("asset-relay-close", closeErr)
		if task, err := s.Store.Tasks.Legacy(cleanup, session.ActionID); err == nil && closeErr == nil {
			_ = s.Store.Tasks.Cancel(cleanup, task.Task.ID)
			_ = s.Store.Notifications.Publish(cleanup, "workers")
		}
	}()
	rep.Stage("等 VPS 领取持久中转任务")
	tick := time.NewTicker(relayCheck)
	defer tick.Stop()
	for {
		done := s.actionDone.wait(session.ActionID)
		current, err := s.renewRelayConsumer(ctx, session)
		if err != nil {
			return err
		}
		session = current
		action, err := s.Store.Ent.GateAction.Get(ctx, session.ActionID)
		if err != nil {
			return err
		}
		if session.Received > 0 {
			rep.Stage("VPS 传回中心")
			rep.Report(session.Received, session.Size, 0)
		} else if session.Progress.Done > 0 {
			rep.Stage("VPS 下载中")
			rep.Report(session.Progress.Done, session.Progress.Total, session.Progress.Speed)
		}
		switch action.Status {
		case gateaction.StatusFailed:
			if action.Error != "" {
				return errors.New(action.Error)
			}
			return errors.New("VPS 报告下载失败")
		case gateaction.StatusExpired:
			return errors.New("VPS 没有领取中转任务")
		case gateaction.StatusDone:
			rep.Stage("校验持久分块与完整摘要")
			return s.materializeRelay(ctx, session, dest)
		}
		if time.Since(session.CreatedAt) > relayPickup {
			task, lookupErr := s.Store.Tasks.Legacy(ctx, session.ActionID)
			if errors.Is(lookupErr, sql.ErrNoRows) {
				if action.Status == gateaction.StatusPending {
					return errors.New("VPS 没有领取中转任务")
				}
			} else if lookupErr != nil {
				return lookupErr
			} else if task.Status == "pending" {
				return errors.New("VPS 没有领取中转任务")
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		case <-tick.C:
			if g, err := s.Store.Ent.Gate.Get(ctx, gid); err == nil && !gateOnline(g) {
				return errors.New("VPS 离线")
			}
		}
	}
}

// Leased workers carry their current attempt on each upload request. Legacy
// callers are accepted only while this session has no new-protocol task.
func (s *Server) relayCaller(c *gin.Context) (relaySession, error) {
	session, err := s.loadRelay(c.Request.Context(), c.Param("id"), gateFrom(c).ID)
	if err != nil {
		return session, err
	}
	return session, s.checkRelayLease(c.Request.Context(), session, c.GetHeader("X-Weir-Task-ID"), c.GetHeader("X-Weir-Task-Attempt"), c.GetHeader("X-Weir-Task-Lease"))
}

func (s *Server) checkRelayLease(ctx context.Context, session relaySession, id, attemptText, token string) error {
	task, err := s.Store.Tasks.Legacy(ctx, session.ActionID)
	if errors.Is(err, sql.ErrNoRows) {
		action, e := s.Store.Ent.GateAction.Get(ctx, session.ActionID)
		if e != nil {
			return e
		}
		if action.GateID != session.GateID || action.Status != gateaction.StatusRunning || !time.Now().Before(action.ExpiresAt) {
			return errors.New("task_lease_lost")
		}
		return nil
	}
	if err != nil {
		return err
	}
	attempt, _ := strconv.Atoi(attemptText)
	if task.Task.ID != id || task.GateID != session.GateID || task.Task.Attempt != attempt || task.Task.LeaseToken != token || task.Status != "leased" || !time.Now().Before(task.Task.LeaseUntil) || !time.Now().Before(task.Task.Deadline) {
		return errors.New("task_lease_lost")
	}
	return nil
}

func relayHTTPFailure(c *gin.Context, err error) {
	switch err.Error() {
	case "upload_not_found":
		failure(c, 404, "upload_not_found")
	case "task_lease_lost":
		failure(c, 409, "task_lease_lost")
	case "upload_too_large":
		failure(c, 413, "upload_too_large")
	case "relay_rate_invalid", "relay_rate_state_invalid":
		failure(c, 503, err.Error())
	default:
		failInternal(c, 503, "relay_storage_unavailable", err)
	}
}

func (s *Server) agentUploadState(c *gin.Context) {
	session, err := s.relayCaller(c)
	if err != nil {
		relayHTTPFailure(c, err)
		return
	}
	c.JSON(200, contracts.UploadState{Received: session.Received})
}

func (s *Server) agentUploadProgress(c *gin.Context) {
	var progress contracts.UploadProgress
	if !bindWorker(c, &progress, 4096) {
		return
	}
	if progress.Done < 0 || progress.Total < 0 || progress.Speed < 0 || progress.Done > relayMaximumBytes || progress.Total > relayMaximumBytes || progress.Total > 0 && progress.Done > progress.Total {
		failure(c, 400, "invalid_upload_progress")
		return
	}
	session, err := s.relayCaller(c)
	if err != nil {
		relayHTTPFailure(c, err)
		return
	}
	// Upstream source labels are external text, not a safe progress message.
	progress.Source = ""
	err = s.Store.Lock.Do(c.Request.Context(), "asset-relay:"+session.ID, func(ctx context.Context) error {
		current, err := s.loadRelay(ctx, session.ID, session.GateID)
		if err != nil {
			return err
		}
		if err = s.checkRelayLease(ctx, current, c.GetHeader("X-Weir-Task-ID"), c.GetHeader("X-Weir-Task-Attempt"), c.GetHeader("X-Weir-Task-Lease")); err != nil {
			return err
		}
		if current.Received == 0 && progress.Done >= current.Progress.Done {
			current.Progress = progress
			if err = s.Store.Ent.Resource.Update().Where(resource.KindEQ(relaySessionKind), resource.KeyEQ(current.ID)).SetData(toMap(current)).Exec(ctx); err != nil {
				return err
			}
			s.actionDone.notify(session.ActionID)
		}
		return nil
	})
	if err != nil {
		relayHTTPFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) agentUploadChunk(c *gin.Context) {
	offset, err := strconv.ParseInt(c.Query("offset"), 10, 64)
	if err != nil || offset < 0 {
		failure(c, 400, "invalid_offset")
		return
	}
	session, err := s.relayCaller(c)
	if err != nil {
		relayHTTPFailure(c, err)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, contracts.UploadChunk+1))
	if err != nil {
		failure(c, 400, "invalid_request")
		return
	}
	if len(body) > contracts.UploadChunk {
		failure(c, 413, "chunk_too_large")
		return
	}
	_, budget, err := s.workerPolicy(c.Request.Context())
	if err != nil {
		relayHTTPFailure(c, err)
		return
	}
	current, err := s.writeRelayChunk(c.Request.Context(), session.ID, session.GateID, offset, body, budget.BandwidthBytesPerSecond, func(ctx context.Context) error {
		return s.checkRelayLease(ctx, session, c.GetHeader("X-Weir-Task-ID"), c.GetHeader("X-Weir-Task-Attempt"), c.GetHeader("X-Weir-Task-Lease"))
	})
	if err != nil {
		if err.Error() == "offset_mismatch" {
			c.AbortWithStatusJSON(409, gin.H{"received": current.Received, "error": gin.H{"code": "offset_mismatch", "message": "offset_mismatch"}})
			return
		}
		relayHTTPFailure(c, err)
		return
	}
	c.JSON(200, contracts.UploadState{Received: current.Received})
}

// centerArch 是中心本机的架构（测速、预下载取发布包用）。
func centerArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}
