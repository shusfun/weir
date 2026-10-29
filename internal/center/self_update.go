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
	"github.com/shusfun/weir/internal/selfupdate"
)

// 自更新的中心一侧：查 GitHub 有没有新版本、写请求、读 root 写回的进度。真正的下载与安装在
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
	admin.POST("/system/update", s.postSelfUpdate)
}

func (s *Server) getSelfUpdate(c *gin.Context) {
	out := gin.H{"current": buildinfo.Version, "blocker": selfUpdateBlocker(), "previous": selfupdate.Previous()}
	if st, ok := selfupdate.ReadStatus(); ok {
		out["status"] = st
	}
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

// postSelfUpdate 写入更新请求：更新到某个正式版本（含最近 3 个旧版本），或切回本机保留的上一版本。
// 要求当前密码与二次验证码；root 一侧还会再校验一遍。
func (s *Server) postSelfUpdate(c *gin.Context) {
	var req struct {
		Action   string `json:"action"`
		Version  string `json:"version"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !bind(c, &req) || !s.reauth(c, req.Password, req.Code) {
		return
	}
	if why := selfUpdateBlocker(); why != "" {
		failDetail(c, 409, "self_update_unavailable", why, nil)
		return
	}
	if st, ok := selfupdate.ReadStatus(); ok && st.Finished == nil && time.Since(st.Started) < 30*time.Minute {
		failDetail(c, 409, "self_update_running", "已有一次更新正在进行", nil)
		return
	}
	version := strings.TrimPrefix(strings.TrimSpace(req.Version), "v")
	switch req.Action {
	case "update":
		list, err := s.weirReleases(c.Request.Context(), true)
		if err != nil {
			failDetail(c, 502, "upstream_unavailable", "无法读取 GitHub 发布："+err.Error(), nil)
			return
		}
		found := false
		for _, r := range list {
			found = found || r.Version == version
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
	if err := selfupdate.WriteRequest(selfupdate.Request{ID: id, Action: req.Action, Version: version}); err != nil {
		failDetail(c, 409, "self_update_rejected", err.Error(), nil)
		return
	}
	target := version
	if target == "" {
		target = "上一版本 " + selfupdate.Previous()
	}
	s.audit(c, "system."+req.Action, buildinfo.Version+" → "+target, "ok")
	c.JSON(202, gin.H{"id": id})
}
