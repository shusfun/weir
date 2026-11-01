package center

// 「GitHub 加速」：整个项目只有一份按优先级排好的下载来源（直连、加速镜像、经某台 VPS 中转），存在资源
// settings/github-sources。内核、Gate 安装包、中心与 Gate 的自更新、客户端安装包（浏览器）、一行安装命令都按它取用。
// 摘要始终直连 GitHub API 读取，来源只提供文件内容，按摘要校验。
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/gateaction"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/packages/contracts"
)

const maxGitHubSources = 12

// sourceSet 是读到的来源列表；Default 为真表示还没有保存过（用的是旧的安装包镜像或内置默认）。
type sourceSet struct {
	Sources []ghrelease.Source
	Default bool
}

func sourceRevision(list []ghrelease.Source) string {
	raw, _ := json.Marshal(list)
	return digest(string(raw))
}

// githubSources 返回当前的来源顺序。没有保存过时：旧设置「安装包加速镜像」（settings/client-mirrors）有内容就用
// 直连 + 这些前缀（它原来只给浏览器用，迁移后各处共用），否则用 ghrelease.DefaultSources()。
// 读取失败如实返回错误，不退回默认来源。
func (s *Server) githubSources(ctx context.Context) (sourceSet, error) {
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ("settings"), resource.KeyEQ("github-sources")).Only(ctx)
	if err == nil {
		var list []ghrelease.Source
		raw, _ := json.Marshal(row.Data["sources"])
		if json.Unmarshal(raw, &list) != nil || len(list) == 0 {
			return sourceSet{}, errors.New("保存的 GitHub 加速来源格式无效")
		}
		return sourceSet{Sources: list}, nil
	}
	if !ent.IsNotFound(err) {
		return sourceSet{}, err
	}
	legacy, err := s.legacyClientMirrors(ctx)
	if err != nil {
		return sourceSet{}, err
	}
	if len(legacy) > 0 {
		list := []ghrelease.Source{{Kind: ghrelease.SourceDirect}}
		for _, m := range legacy {
			list = append(list, ghrelease.Source{Kind: ghrelease.SourceMirror, Prefix: m})
		}
		return sourceSet{Sources: list, Default: true}, nil
	}
	return sourceSet{Sources: ghrelease.DefaultSources(), Default: true}, nil
}

// legacyClientMirrors 读旧版「安装包加速镜像」的前缀列表（只读，用于迁移）；无效的前缀跳过。
func (s *Server) legacyClientMirrors(ctx context.Context) ([]string, error) {
	row, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ("settings"), resource.KeyEQ("client-mirrors")).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []string
	raw, _ := json.Marshal(row.Data["mirrors"])
	_ = json.Unmarshal(raw, &items)
	out, seen := []string{}, map[string]bool{}
	for _, m := range items {
		if ghrelease.ValidPrefix(m) && !seen[m] && len(out) < maxGitHubSources-1 {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out, nil
}

// validateSources 检查管理员提交的列表，返回错误码。至少要有一个直连或镜像：经 VPS 中转时 VPS 也要按它们下载。
func (s *Server) validateSources(ctx context.Context, list []ghrelease.Source) (string, error) {
	if len(list) == 0 || len(list) > maxGitHubSources {
		return "source_limit", nil
	}
	direct, prefixes, gates, httpCount := false, map[string]bool{}, map[int64]bool{}, 0
	for _, src := range list {
		switch src.Kind {
		case ghrelease.SourceDirect:
			if direct || src.Prefix != "" || src.GateID != 0 {
				return "invalid_source", nil
			}
			direct = true
			httpCount++
		case ghrelease.SourceMirror:
			if !ghrelease.ValidPrefix(src.Prefix) || prefixes[src.Prefix] || src.GateID != 0 {
				return "invalid_mirror", nil
			}
			prefixes[src.Prefix] = true
			httpCount++
		case ghrelease.SourceGate:
			if src.GateID <= 0 || gates[src.GateID] || src.Prefix != "" {
				return "invalid_source_gate", nil
			}
			g, err := s.Store.Ent.Gate.Get(ctx, int(src.GateID))
			if ent.IsNotFound(err) || err == nil && g.Status == gate.StatusRevoked {
				return "invalid_source_gate", nil
			}
			if err != nil {
				return "", err
			}
			gates[src.GateID] = true
		default:
			return "invalid_source", nil
		}
	}
	if httpCount == 0 {
		return "http_source_required", nil
	}
	return "", nil
}

// sourceView 是给管理员看的一项；gate 来源带上 VPS 名称（VPS 已删除时为空，页面显示「未知 VPS」）。
func (s *Server) sourceView(ctx context.Context, list []ghrelease.Source) []gin.H {
	names := s.gateNames(ctx)
	out := make([]gin.H, 0, len(list))
	for _, src := range list {
		item := gin.H{"kind": src.Kind}
		switch src.Kind {
		case ghrelease.SourceMirror:
			item["prefix"] = src.Prefix
		case ghrelease.SourceGate:
			item["gate_id"], item["gate_name"] = src.GateID, names[src.GateID]
		}
		out = append(out, item)
	}
	return out
}

func (s *Server) gateNames(ctx context.Context) map[int64]string {
	out := map[int64]string{}
	if items, err := s.Store.Ent.Gate.Query().Select(gate.FieldID, gate.FieldName).All(ctx); err == nil {
		for _, g := range items {
			out[int64(g.ID)] = g.Name
		}
	}
	return out
}

func (s *Server) githubSourceRoutes(auth, admin *gin.RouterGroup) {
	auth.GET("/github-sources", s.getPublicSources)
	admin.GET("/github-sources", s.getGitHubSources)
	admin.PUT("/github-sources", s.putGitHubSources)
	admin.POST("/github-sources/test", s.asJob("GitHub 加速测速", "已取消测速", s.testGitHubSources))
}

func (s *Server) sourceDocument(ctx context.Context, set sourceSet) gin.H {
	// defaults 是内置默认来源（WEIR_GITHUB_MIRRORS 可改），页面「恢复默认」用它
	return gin.H{"sources": s.sourceView(ctx, set.Sources), "revision": sourceRevision(set.Sources), "default": set.Default,
		"defaults": s.sourceView(ctx, ghrelease.DefaultSources())}
}

func (s *Server) getGitHubSources(c *gin.Context) {
	set, err := s.githubSources(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "github_sources_unavailable", err)
		return
	}
	c.JSON(200, s.sourceDocument(c.Request.Context(), set))
}

// getPublicSources 给浏览器下载客户端用：只有直连与镜像（浏览器不能经 VPS 中转），顺序不变。
func (s *Server) getPublicSources(c *gin.Context) {
	set, err := s.githubSources(c.Request.Context())
	if err != nil {
		failInternal(c, 503, "github_sources_unavailable", err)
		return
	}
	out := []gin.H{}
	for _, src := range ghrelease.HTTPOnly(set.Sources) {
		if src.Kind == ghrelease.SourceMirror {
			out = append(out, gin.H{"kind": src.Kind, "prefix": src.Prefix})
		} else {
			out = append(out, gin.H{"kind": src.Kind})
		}
	}
	c.JSON(200, gin.H{"sources": out})
}

// putGitHubSources 保存来源顺序：revision 与保存前读到的一致（防止覆盖别人的修改）。
func (s *Server) putGitHubSources(c *gin.Context) {
	var req struct {
		Sources  []ghrelease.Source `json:"sources"`
		Revision string             `json:"revision"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || req.Sources == nil {
		failure(c, 400, "invalid_request")
		return
	}
	ctx := c.Request.Context()
	if code, err := s.validateSources(ctx, req.Sources); err != nil {
		failInternal(c, 503, "github_sources_unavailable", err)
		return
	} else if code != "" {
		failure(c, 400, code)
		return
	}
	err := s.updateResource(ctx, "settings", "github-sources", func(cur map[string]any) (map[string]any, error) {
		// 没保存过时，比较的是读出来的那份（旧镜像迁移或默认），与 GET 返回的 revision 一致
		set, err := s.githubSources(ctx)
		if err != nil {
			return nil, err
		}
		if sourceRevision(set.Sources) != req.Revision {
			return nil, errResourceConflict
		}
		raw, _ := json.Marshal(map[string]any{"sources": req.Sources})
		var next map[string]any
		return next, json.Unmarshal(raw, &next)
	})
	if errors.Is(err, errResourceConflict) {
		failure(c, 409, "sources_changed")
		return
	}
	if err != nil {
		failInternal(c, 503, "github_sources_save_failed", err)
		return
	}
	names := make([]string, 0, len(req.Sources))
	for _, src := range req.Sources {
		names = append(names, src.Kind+":"+src.Name())
	}
	s.audit(c, "settings.github-sources", strings.Join(names, ","), "ok")
	c.JSON(200, s.sourceDocument(ctx, sourceSet{Sources: req.Sources}))
}

// sourceProbeAsset 是测速用的文件：与中心同版本的 Weir 发布包（开发构建用最新正式版），按中心的架构，取不到时用 amd64。
func (s *Server) sourceProbeAsset(ctx context.Context) (string, string, error) {
	gh := s.weirGitHub()
	var rel ghrelease.Release
	var err error
	if buildinfo.Release() {
		rel, err = gh.Tag(ctx, weirRepo(), "v"+buildinfo.Version)
	} else {
		var list []ghrelease.Release
		if list, err = gh.List(ctx, weirRepo(), 30); err == nil {
			for _, r := range list {
				if !r.Draft && !r.Prerelease && strings.HasPrefix(r.Tag, "v") {
					if _, ok := buildinfo.ParseScheme(r.Tag); ok {
						rel = r
						break
					}
				}
			}
			if rel.Tag == "" {
				err = errors.New("GitHub 上还没有 Weir 正式版本")
			}
		}
	}
	if err != nil {
		return "", "", err
	}
	version := strings.TrimPrefix(rel.Tag, "v")
	for _, arch := range []string{centerArch(), "amd64"} {
		if a, ok := rel.Asset(releaseAssetName(version, arch)); ok {
			if err := gh.Allowed(a.URL); err != nil {
				return "", "", err
			}
			return a.URL, a.Name, nil
		}
	}
	return "", "", fmt.Errorf("v%s 没有可测速的发布包", version)
}

// probeBytes 是每个来源取的字节数；probeTimeout 是单个来源最多等多久。
const (
	probeBytes   = 1 << 20
	probeTimeout = 20 * time.Second
)

type probeItem struct {
	Kind       string `json:"kind"`
	Prefix     string `json:"prefix,omitempty"`
	GateID     int64  `json:"gate_id,omitempty"`
	GateName   string `json:"gate_name,omitempty"`
	Configured bool   `json:"configured"`
	TTFBMs     int64  `json:"ttfb_ms"`
	Bytes      int64  `json:"bytes"`
	Speed      int64  `json:"speed"`
	Error      string `json:"error,omitempty"`
}

// testGitHubSources 逐个测速（串行，避免互相抢带宽）：直连与镜像由中心测；VPS 测它自己直连 GitHub 的速度，
// 即经它中转时的下载速度。已配置的按列表顺序，在线但未加入的 VPS 排在最后，供管理员挑选。
func (s *Server) testGitHubSources(c *gin.Context) {
	ctx := c.Request.Context()
	set, err := s.githubSources(ctx)
	if err != nil {
		failInternal(c, 503, "github_sources_unavailable", err)
		return
	}
	jobStep(ctx, "读取测速用的发布包", 30*time.Second)
	raw, name, err := s.sourceProbeAsset(ctx)
	if err != nil {
		failDetail(c, 502, "upstream_unavailable", "无法读取 GitHub 发布："+err.Error(), nil)
		return
	}
	gates, err := s.Store.Ent.Gate.Query().Where(gate.StatusNEQ(gate.StatusRevoked)).Order(ent.Asc(gate.FieldID)).All(ctx)
	if err != nil {
		failInternal(c, 503, "query_failed", err)
		return
	}
	byID := map[int64]*ent.Gate{}
	for _, g := range gates {
		byID[int64(g.ID)] = g
	}
	list := append([]ghrelease.Source(nil), set.Sources...)
	configured := len(list)
	inList := map[int64]bool{}
	for _, src := range list {
		if src.Kind == ghrelease.SourceGate {
			inList[src.GateID] = true
		}
	}
	for _, g := range gates {
		if !inList[int64(g.ID)] && gateOnline(g) {
			list = append(list, ghrelease.Source{Kind: ghrelease.SourceGate, GateID: int64(g.ID)})
		}
	}
	results := make([]probeItem, 0, len(list))
	for i, src := range list {
		item := probeItem{Kind: src.Kind, Prefix: src.Prefix, GateID: src.GateID, Configured: i < configured}
		label := src.Name()
		if src.Kind == ghrelease.SourceGate {
			if g := byID[src.GateID]; g != nil {
				item.GateName, label = g.Name, "VPS "+g.Name
			}
		}
		jobStep(jobPrefix(ctx, fmt.Sprintf("第 %d/%d 个（%s）· ", i+1, len(list), label)), "测速", probeTimeout+30*time.Second)
		var res contracts.SourceProbeResult
		if src.Kind == ghrelease.SourceGate {
			res = s.probeViaGate(ctx, byID[src.GateID], raw)
		} else {
			p := ghrelease.Probe(ctx, s.githubClientHTTP(), src.URL(raw), probeBytes, probeTimeout)
			res = contracts.SourceProbeResult{TTFBMs: p.TTFB.Milliseconds(), Bytes: p.Bytes, Speed: p.Speed}
			if p.Err != nil {
				res.Error = p.Err.Error()
			}
		}
		if ctx.Err() != nil {
			failDetail(c, 409, "job_cancelled", "已取消测速", nil)
			return
		}
		item.TTFBMs, item.Bytes, item.Speed, item.Error = res.TTFBMs, res.Bytes, res.Speed, res.Error
		results = append(results, item)
	}
	c.JSON(200, gin.H{"results": results, "asset": name})
}

// probeViaGate 让 VPS 直连 GitHub 测速；VPS 离线、已删除或没有按时回复都如实写进 Error。
func (s *Server) probeViaGate(ctx context.Context, g *ent.Gate, raw string) contracts.SourceProbeResult {
	switch {
	case g == nil:
		return contracts.SourceProbeResult{Error: "这台 VPS 已删除"}
	case !gateOnline(g):
		return contracts.SourceProbeResult{Error: "VPS 离线"}
	}
	a, err := s.enqueueAction(ctx, g.ID, contracts.ActionSourceProbe, map[string]any{"url": raw, "bytes": probeBytes}, time.Minute)
	if err != nil {
		return contracts.SourceProbeResult{Error: "下发测速失败：" + err.Error()}
	}
	a, err = s.waitAction(ctx, a.ID, probeTimeout+30*time.Second)
	if err != nil {
		return contracts.SourceProbeResult{Error: err.Error()}
	}
	var res contracts.SourceProbeResult
	if a.Result != nil {
		b, _ := json.Marshal(a.Result)
		_ = json.Unmarshal(b, &res)
	}
	switch a.Status {
	case gateaction.StatusDone:
	case gateaction.StatusFailed:
		if res.Error == "" {
			res.Error = a.Error
		}
		if res.Error == "" {
			res.Error = "VPS 报告测速失败"
		}
	case gateaction.StatusExpired:
		res.Error = "VPS 没有领取测速任务"
	default:
		res.Error = "VPS 没有按时回复测速结果"
	}
	return res
}
