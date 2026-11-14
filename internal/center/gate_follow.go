package center

// Gate 跟着中心更新：心跳回复里，Gate 版本与中心不同时带上要更新到的版本（ReleaseTarget）；正式构建另给该架构
// 发布包的官方摘要与按「GitHub 加速」顺序的下载地址，Gate 优先从 GitHub 下载，失败时退回中心下载。
// 发布信息按版本缓存在内存里（发布包的摘要不会变），心跳不等 GitHub：还没取到时只给版本号。
import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/packages/contracts"
)

// releaseRetry 是读取发布信息失败后多久再试。
var releaseRetry = 10 * time.Minute

// releaseAssetInfo 是某架构发布包的 GitHub 原地址与官方摘要。
type releaseAssetInfo struct {
	URL    string
	SHA256 string
	Size   int64
}

type releaseCache struct {
	mu      sync.Mutex
	version string
	assets  map[string]releaseAssetInfo // 架构 → 发布包
	err     error
	at      time.Time
	loading chan struct{}
}

// weirReleaseAssets 返回中心同版本发布包的信息。wait 为假时不等 GitHub：没有缓存就在后台去取，先返回空。
// 开发构建没有对应发布，返回空。
func (s *Server) weirReleaseAssets(ctx context.Context, wait bool) (map[string]releaseAssetInfo, error) {
	if !buildinfo.Release() {
		return nil, errors.New("开发构建没有对应的 GitHub 发布")
	}
	c := &s.weirRel
	version := buildinfo.Version
	c.mu.Lock()
	if c.version == version && c.assets != nil {
		out := c.assets
		c.mu.Unlock()
		return out, nil
	}
	if c.version != version {
		c.version, c.assets, c.err, c.at = version, nil, nil, time.Time{}
	}
	if c.loading == nil && (c.err == nil || time.Since(c.at) > releaseRetry) {
		ch := make(chan struct{})
		c.loading = ch
		go func() {
			lctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			assets, err := s.loadReleaseAssets(lctx, version)
			c.mu.Lock()
			if c.version == version {
				c.assets, c.err, c.at = assets, err, time.Now()
			}
			c.loading = nil
			c.mu.Unlock()
			close(ch)
		}()
	}
	ch, last := c.loading, c.err
	c.mu.Unlock()
	if !wait || ch == nil {
		return nil, last
	}
	select {
	case <-ch:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.assets, c.err
}

func (s *Server) loadReleaseAssets(ctx context.Context, version string) (map[string]releaseAssetInfo, error) {
	gh := s.weirGitHub()
	rel, err := gh.Tag(ctx, weirRepo(), "v"+version)
	if err != nil {
		return nil, err
	}
	out := map[string]releaseAssetInfo{}
	for _, arch := range []string{"amd64", "arm64"} {
		if a, ok := rel.Asset(releaseAssetName(version, arch)); ok && a.SHA256() != "" && gh.Allowed(a.URL) == nil {
			out[arch] = releaseAssetInfo{URL: a.URL, SHA256: a.SHA256(), Size: a.Size}
		}
	}
	return out, nil
}

// releaseTarget 是给某台 Gate 的更新目标：正式构建且取到了发布信息时带摘要与地址，否则只有版本号
// （Gate 从中心的 /agent/release/<版本> 下载）。地址按当前的来源顺序生成，去掉 gate 来源。
func (s *Server) releaseTarget(ctx context.Context, arch string, wait bool) *contracts.ReleaseTarget {
	t := &contracts.ReleaseTarget{Version: buildinfo.Version}
	assets, _ := s.weirReleaseAssets(ctx, wait)
	a, ok := assets[arch]
	if !ok {
		return t
	}
	gh, sources, err := s.downloadGitHub(ctx)
	if err != nil {
		return t
	}
	gh.Sources = ghrelease.HTTPOnly(sources)
	if urls, err := gh.URLs(a.URL); err == nil && len(urls) > 0 {
		t.SHA256, t.URLs = a.SHA256, urls
	}
	return t
}

// heartbeatRelease 决定心跳回复里要不要带 Release：版本相同、或单机的本机 Gate（在中心进程里运行，随中心一起更新）不带。
func (s *Server) heartbeatRelease(ctx context.Context, g *ent.Gate, version string) *contracts.ReleaseTarget {
	if s.Config.GateUpdatesPaused || version == buildinfo.Version || colocated(g) {
		return nil
	}
	return s.releaseTarget(ctx, g.Arch, false)
}

// updateGateRelease 手动让一台 VPS 更新到中心的版本（release.replace，不受自动更新失败后的冷却限制）。
// 只下发动作、不等结果：Gate 替换程序后会重启，进度与失败原因随心跳出现在 self_update 里。
func (s *Server) updateGateRelease(c *gin.Context) {
	g, ok := s.gateParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	switch {
	case !gateOnline(g):
		failure(c, 409, "gate_offline")
		return
	case g.Version == buildinfo.Version:
		failure(c, 409, "gate_up_to_date")
		return
	case colocated(g):
		failDetail(c, 409, "gate_colocated", "这台 VPS 与中心在同一台机器上，随中心一起更新", nil)
		return
	}
	wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	t := s.releaseTarget(wctx, g.Arch, true)
	cancel()
	payload := map[string]any{"version": t.Version}
	if len(t.URLs) > 0 {
		payload["sha256"], payload["urls"] = t.SHA256, t.URLs
	}
	a, err := s.enqueueAction(ctx, g.ID, contracts.ActionReleaseReplace, payload, 10*time.Minute)
	if err != nil {
		failInternal(c, 503, "action_failed", err)
		return
	}
	s.audit(c, "gate.update", g.Name+" "+g.Version+" → "+t.Version, "ok")
	c.JSON(202, gin.H{"action_id": a.ID, "release": t})
}
