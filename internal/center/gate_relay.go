package center

// 经 VPS 中转下载 GitHub 文件：中心连不上 GitHub（或很慢）时，让某台 VPS 下载、按官方摘要校验后分块上传给中心。
// 上传编号只在中心进程内存里，绑定到发起的那台 Gate；中心重启或下载取消后编号作废，Gate 的上传会收到 404 并停止。
// 中心收完后照常按摘要校验（fetch.Download），VPS 只是又一个来源。
import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/store/ent/gateaction"
	"github.com/shusfun/weir/packages/contracts"
)

// relayPickup 是 Gate 领取中转任务的时限；relayCheck 是检查 Gate 是否仍在线的间隔。
var (
	relayPickup = 2 * time.Minute
	relayCheck  = 10 * time.Second
)

type relayUpload struct {
	gateID    int
	path      string
	size      int64 // GitHub 给出的大小；0 表示未知
	maxSize   int64
	received  int64
	closed    bool
	report    fetch.Reporter
	name      string
	uploading bool
	reported  bool
	stage     string
	changed   chan struct{}
}

type relayHub struct {
	mu      sync.Mutex
	uploads map[string]*relayUpload
}

func (h *relayHub) add(id string, u *relayUpload) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.uploads == nil {
		h.uploads = map[string]*relayUpload{}
	}
	h.uploads[id] = u
}

// remove 作废上传编号；之后到达的上传一律 404，不会再写文件。
func (h *relayHub) remove(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if u := h.uploads[id]; u != nil {
		u.closed = true
	}
	delete(h.uploads, id)
}

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
	return ghrelease.Client{HTTP: githubHTTP(), Sources: set.Sources}, set.Sources, nil
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

// relayFetch 让 Gate gid 下载 req.URLs 并上传到 dest。Gate 离线、没有领取、报告失败时立即返回其原因；
// VPS 下载与传回中心两段的进度都经 rep 报给下载任务。
func (s *Server) relayFetch(ctx context.Context, gid int, req contracts.AssetFetch, dest string, rep fetch.Reporter) error {
	g, err := s.Store.Ent.Gate.Get(ctx, gid)
	if err != nil {
		return errors.New("这台 VPS 已删除")
	}
	if !gateOnline(g) {
		return errors.New("VPS 离线")
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_ = f.Close()
	req.UploadID = randomHex(16)
	if req.MaxSize <= 0 {
		req.MaxSize = 256 << 20
	}
	name := "VPS " + g.Name
	up := &relayUpload{gateID: gid, path: dest, size: req.Size, maxSize: req.MaxSize, report: rep, name: name, changed: make(chan struct{}, 1)}
	s.relays.add(req.UploadID, up)
	defer s.relays.remove(req.UploadID)
	payload := map[string]any{"upload_id": req.UploadID, "urls": req.URLs, "sha256": req.SHA256, "size": req.Size, "max_size": req.MaxSize}
	a, err := s.enqueueAction(ctx, gid, contracts.ActionAssetFetch, payload, relayPickup)
	if err != nil {
		return fmt.Errorf("下发中转任务失败：%w", err)
	}
	rep.Stage("等 " + name + " 领取任务")
	tick := time.NewTicker(relayCheck)
	defer tick.Stop()
	started := time.Time{}
	for {
		done := s.actionDone.wait(a.ID)
		cur, err := s.Store.Ent.GateAction.Get(ctx, a.ID)
		if err != nil {
			return err
		}
		s.relays.mu.Lock()
		silent := !up.reported && !up.uploading
		s.relays.mu.Unlock()
		// 新版 Gate 一领取就上报；领取后迟迟没有上报的是不报进度的旧版 Gate，如实写明，不让进度条像卡死
		if cur.Status == gateaction.StatusRunning && silent {
			if started.IsZero() {
				started = time.Now()
				rep.Stage(name + " 下载中")
			} else if time.Since(started) >= relayCheck {
				rep.Stage(name + " 下载中（这台 VPS 不报进度）")
			}
		}
		switch cur.Status {
		case gateaction.StatusFailed:
			if cur.Error != "" {
				return errors.New(cur.Error)
			}
			return errors.New("VPS 报告下载失败")
		case gateaction.StatusExpired:
			return errors.New("VPS 没有领取中转任务")
		case gateaction.StatusDone:
			s.relays.mu.Lock()
			got := up.received
			s.relays.mu.Unlock()
			if req.Size > 0 && got != req.Size {
				return fmt.Errorf("VPS 报告完成，但只上传了 %s / %s", fetch.Size(got), fetch.Size(req.Size))
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		case <-up.changed:
		case <-tick.C:
			if g, err := s.Store.Ent.Gate.Get(ctx, gid); err == nil && !gateOnline(g) {
				return errors.New("VPS 离线")
			}
			// 待领取超过时限的动作在 Gate 下次取动作时才标记过期：这里按时间直接判断
			if cur.Status == gateaction.StatusPending && time.Now().After(cur.ExpiresAt) {
				return errors.New("VPS 没有领取中转任务")
			}
		}
	}
}

// relayUploadFor 找到 Gate 自己的上传；别的 Gate 的编号一律当作不存在。调用方持有 relays.mu。
func (s *Server) relayUploadFor(c *gin.Context) *relayUpload {
	u := s.relays.uploads[c.Param("id")]
	if u == nil || u.closed || u.gateID != gateFrom(c).ID {
		return nil
	}
	return u
}

// agentUploadState 返回已收到的字节数，Gate 断线后据此续传。
func (s *Server) agentUploadState(c *gin.Context) {
	s.relays.mu.Lock()
	defer s.relays.mu.Unlock()
	u := s.relayUploadFor(c)
	if u == nil {
		failure(c, 404, "upload_not_found")
		return
	}
	c.JSON(200, contracts.UploadState{Received: u.received})
}

// agentUploadProgress 接收 Gate 下载阶段的进度（Gate 下载完才开始上传，这段中心自己看不到）。只用于显示，
// 不影响下载；开始上传后到达的旧进度丢弃，避免进度条往回跳。
func (s *Server) agentUploadProgress(c *gin.Context) {
	var p contracts.UploadProgress
	if !bind(c, &p) {
		return
	}
	s.relays.mu.Lock()
	defer s.relays.mu.Unlock()
	u := s.relayUploadFor(c)
	if u == nil {
		failure(c, 404, "upload_not_found")
		return
	}
	if !u.uploading {
		stage := u.name + " 下载中"
		if p.Source != "" {
			stage += "（" + p.Source + "）"
		}
		if stage != u.stage {
			u.stage = stage
			u.report.Stage(stage)
		}
		u.reported = true
		u.report.Report(p.Done, p.Total, p.Speed)
	}
	c.JSON(200, gin.H{"ok": true})
}

// agentUploadChunk 写入一块：offset 必须等于已收到的字节数（否则 409 并回已收到的字节数），
// 单块不超过 UploadChunk，总量不超过 MaxSize。
func (s *Server) agentUploadChunk(c *gin.Context) {
	offset, err := strconv.ParseInt(c.Query("offset"), 10, 64)
	if err != nil || offset < 0 {
		failure(c, 400, "invalid_offset")
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
	s.relays.mu.Lock()
	defer s.relays.mu.Unlock()
	u := s.relayUploadFor(c)
	if u == nil {
		failure(c, 404, "upload_not_found")
		return
	}
	if offset != u.received {
		c.AbortWithStatusJSON(409, gin.H{"received": u.received, "error": gin.H{"code": "offset_mismatch", "message": "offset_mismatch"}})
		return
	}
	end := offset + int64(len(body))
	if end > u.maxSize || u.size > 0 && end > u.size {
		failure(c, 413, "upload_too_large")
		return
	}
	f, err := os.OpenFile(u.path, os.O_WRONLY, 0o644)
	if err == nil {
		_, err = f.WriteAt(body, offset)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		failInternal(c, 500, "upload_write_failed", err)
		return
	}
	u.received = end
	if !u.uploading {
		u.uploading = true
		u.report.Stage(u.name + " 传回中心")
	}
	u.report.Report(end, u.size, 0)
	select {
	case u.changed <- struct{}{}:
	default:
	}
	c.JSON(200, contracts.UploadState{Received: u.received})
}

// centerArch 是中心本机的架构（测速、预下载取发布包用）。
func centerArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}
