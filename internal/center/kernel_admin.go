package center

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/store/ent/setting"
)

// githubHTTP 访问 GitHub 与镜像：不设整体时限（大文件下载由 fetch 的无数据超时控制），只限制等响应头的时间。
func githubHTTP() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: tr}
}

func (s *Server) fetcher() kernels.Fetcher {
	if g := s.GitHub; g != nil {
		return kernels.Fetcher{HTTP: g.HTTP, API: g.API, DownloadHost: g.Host, Cache: s.kernelCache()}
	}
	return kernels.Fetcher{HTTP: githubHTTP(), Mirrors: ghrelease.Mirrors(), Cache: s.kernelCache()}
}

func validKernel(name string) bool {
	for _, n := range kernels.Names {
		if n == name {
			return true
		}
	}
	return false
}

func (s *Server) kernelAdminRoutes(admin *gin.RouterGroup) {
	admin.GET("/kernels", s.listKernels)
	admin.GET("/sub-checks", s.subChecks)
	admin.GET("/kernels/:kernel/releases", s.kernelReleases)
	admin.POST("/kernels/:kernel/fetch", s.fetchKernel)
	admin.DELETE("/kernels/:kernel/versions/:version", s.removeKernelVersion)
	admin.PUT("/kernels/defaults", s.setKernelDefaults)
}

// kernelArches 是某个内核需要的架构：中心本机，加上已接入 Gate 的架构（mihomo 只在中心运行）。
// 中心不是 Linux 时（开发环境）只按 Gate 的架构算，没有 Gate 时用 amd64。
func (s *Server) kernelArches(ctx context.Context, kernel string) []string {
	set := map[string]bool{}
	if runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		set[runtime.GOARCH] = true
	}
	if kernel != "mihomo" && s.Store != nil {
		if arches, err := s.Store.Ent.Gate.Query().Unique(true).Select("arch").Strings(ctx); err == nil {
			for _, a := range arches {
				if a == "amd64" || a == "arm64" {
					set[a] = true
				}
			}
		}
	}
	if len(set) == 0 {
		set["amd64"] = true
	}
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func (s *Server) kernelDefaults(ctx context.Context) map[string]string {
	out := map[string]string{}
	if row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ("kernel-defaults")).Only(ctx); err == nil {
		for k, v := range row.Value {
			if str, ok := v.(string); ok {
				out[k] = str
			}
		}
	}
	return out
}

// kernelUsers 列出正在使用某个内核版本的 VPS：锁定它的、正在运行它的。
func (s *Server) kernelUsers(ctx context.Context, kernel, version string) (pinned, running []gateRef) {
	gates, err := s.Store.Ent.Gate.Query().All(ctx)
	if err != nil {
		return nil, nil
	}
	for _, g := range gates {
		if g.KernelPins[kernel] == version {
			pinned = append(pinned, gateRef{g.ID, g.Name})
		}
		if g.KernelsRunning[kernel] == version {
			running = append(running, gateRef{g.ID, g.Name})
		}
	}
	return pinned, running
}

// refNames 保留旧字段（只有名称）给已有的读取方。
func refNames(refs []gateRef) []string {
	if refs == nil {
		return nil
	}
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.Name
	}
	return out
}

func (s *Server) listKernels(c *gin.Context) {
	ctx := c.Request.Context()
	defaults := s.kernelDefaults(ctx)
	out := gin.H{}
	for _, name := range kernels.Names {
		arches := s.kernelArches(ctx, name)
		cache := s.kernelCache()
		versions := map[string]bool{}
		for _, arch := range []string{"amd64", "arm64"} {
			for _, v := range cache.Versions(name, arch) {
				versions[v] = true
			}
		}
		list := make([]gin.H, 0, len(versions))
		for v := range versions {
			have := gin.H{}
			for _, arch := range arches {
				_, _, err := cache.Get(name, v, arch)
				have[arch] = err == nil
			}
			pinned, running := s.kernelUsers(ctx, name, v)
			item := gin.H{"version": v, "arches": have, "default": defaults[name] == v, "pinned_by": refNames(pinned), "running_on": refNames(running), "pinned_refs": nonNilRefs(pinned), "running_refs": nonNilRefs(running)}
			if name != "mihomo" {
				if d, _, err := s.drivers(ctx).Select(name, v); err == nil {
					item["driver"] = d.Name
				} else {
					item["driver_error"] = err.Error()
				}
			}
			list = append(list, item)
		}
		sort.Slice(list, func(i, j int) bool {
			return kernels.CompareVersions(list[i]["version"].(string), list[j]["version"].(string)) > 0
		})
		out[name] = gin.H{"default": defaults[name], "arches": arches, "versions": list}
	}
	c.JSON(200, gin.H{"kernels": out})
}

func (s *Server) kernelReleases(c *gin.Context) {
	name := c.Param("kernel")
	if !validKernel(name) {
		failure(c, 404, "kernel_not_found")
		return
	}
	list, err := s.fetcher().Releases(c.Request.Context(), name)
	if err != nil {
		c.AbortWithStatusJSON(502, gin.H{"error": gin.H{"code": "upstream_unavailable", "message": "无法获取上游版本：" + err.Error()}})
		return
	}
	if len(list) > 20 {
		list = list[:20]
	}
	ctx := c.Request.Context()
	d := s.drivers(ctx)
	rec, _ := s.recommended(ctx, name)
	out := make([]gin.H, 0, len(list))
	for _, r := range list {
		item := gin.H{"version": r.Version, "published_at": r.PublishedAt, "notes": r.Notes, "downloads": r.Downloads, "recommended": r.Version == rec.Version,
			"young": time.Since(r.PublishedAt) < recommendAge}
		if name != "mihomo" {
			if drv, _, err := d.Select(name, r.Version); err == nil {
				item["driver"] = drv.Name
				if why := s.incompatible(ctx, drv, name, r.Version); why != "" {
					item["incompatible"] = why
				}
			}
		}
		out = append(out, item)
	}
	c.JSON(200, gin.H{"releases": out, "arches": s.kernelArches(ctx, name), "recommended": rec})
}

// kernelJobID 是下载某内核版本的任务 id；同一版本同时只有一个任务。
func kernelJobID(kernel, version string) string { return "kernel:" + kernel + ":" + version }

// startKernelFetch 在后台下载一个版本需要的全部架构（已缓存的跳过），完成后让 Gate 重新编译配置。
func (s *Server) startKernelFetch(ctx context.Context, kernel, version string, actor int, ip string) Job {
	arches := s.kernelArches(ctx, kernel)
	title := kernelTitle(kernel) + " " + version
	return s.jobs.Start(kernelJobID(kernel, version), "kernel", title, func(bg context.Context, run JobRun) (map[string]any, error) {
		f := s.fetcher()
		f.Progress, f.Log = run.Progress, run.Log
		sums := map[string]any{}
		var err error
		for _, arch := range arches {
			if _, sum, e := f.Cache.Get(kernel, version, arch); e == nil {
				run.Log(arch + " 已下载，跳过")
				sums[arch] = sum
				continue
			}
			run.Step("下载 " + arch)
			var sum string
			if sum, err = f.Fetch(bg, kernel, version, arch); err != nil {
				break
			}
			sums[arch] = sum
			run.Log(arch + " 已校验并缓存，SHA-256 " + sum[:16] + "…")
		}
		res := "ok"
		if err != nil {
			res = "failed"
		}
		if s.Store != nil {
			_, _ = s.Store.Ent.Audit.Create().SetActor(actor).SetAction("kernel.fetch").SetTarget(kernel + " " + version + " " + strings.Join(arches, ",")).SetResult(res).SetIP(ip).Save(context.Background())
			if err == nil {
				if ids, e := s.Store.Ent.Gate.Query().IDs(context.Background()); e == nil {
					_ = s.bumpGates(context.Background(), ids)
				}
			}
		}
		return map[string]any{"sha256": sums, "arches": arches}, err
	})
}

// autoKernelFetch 是编译 Gate 配置时发现缺内核而自动发起的下载；上次自动下载失败不到 10 分钟时不重试，
// 避免每次心跳都去请求 GitHub。管理员在内核页手动下载不受此限制。
func (s *Server) autoKernelFetch(ctx context.Context, kernel, version string) {
	if j, ok := s.jobs.Get(kernelJobID(kernel, version)); ok && j.Status == jobFailed && j.Finished != nil && time.Since(*j.Finished) < 10*time.Minute {
		return
	}
	s.startKernelFetch(ctx, kernel, version, 0, "")
}

func kernelTitle(kernel string) string {
	switch kernel {
	case "xray":
		return "Xray"
	case "sing-box":
		return "sing-box"
	case "mihomo":
		return "mihomo"
	}
	return kernel
}

func (s *Server) fetchKernel(c *gin.Context) {
	name := c.Param("kernel")
	var req struct {
		Version string `json:"version"`
	}
	if !validKernel(name) || !bind(c, &req) {
		if !c.IsAborted() {
			failure(c, 404, "kernel_not_found")
		}
		return
	}
	version := strings.TrimPrefix(req.Version, "v")
	if version == "" || len(version) > 64 || strings.ContainsAny(version, `/\ `) {
		failure(c, 400, "invalid_version")
		return
	}
	job := s.startKernelFetch(c.Request.Context(), name, version, c.GetInt("user_id"), c.ClientIP())
	c.JSON(202, gin.H{"job": job})
}

// removeKernelVersion 删除没人用的缓存版本；是默认版本、被锁定或正在运行时拒绝。
func (s *Server) removeKernelVersion(c *gin.Context) {
	name, version := c.Param("kernel"), c.Param("version")
	if !validKernel(name) {
		failure(c, 404, "kernel_not_found")
		return
	}
	ctx := c.Request.Context()
	if s.kernelDefaults(ctx)[name] == version {
		failDetail(c, 409, "kernel_in_use", "这是默认版本，先把默认改成别的版本", nil)
		return
	}
	if pinned, running := s.kernelUsers(ctx, name, version); len(pinned)+len(running) > 0 {
		failDetail(c, 409, "kernel_in_use", "仍有 VPS 在用："+strings.Join(refNames(append(pinned, running...)), "、"), nil)
		return
	}
	if err := s.kernelCache().Remove(name, version); err != nil {
		failure(c, 400, "invalid_version")
		return
	}
	s.audit(c, "kernel.remove", name+" "+version, "ok")
	c.JSON(200, gin.H{"ok": true})
}

// setKernelDefaults 设置各内核的默认版本：没有单独锁定版本的 Gate 会按新版本重新下发配置。
// 需要的架构还没下载齐时自动开始下载，下载完成后再设为默认。
func (s *Server) setKernelDefaults(c *gin.Context) {
	var req map[string]string
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	value := map[string]any{}
	for k, v := range s.kernelDefaults(ctx) {
		value[k] = v
	}
	for name, v := range req {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if !validKernel(name) || v == "" {
			continue
		}
		var missing []string
		for _, arch := range s.kernelArches(ctx, name) {
			if _, _, err := s.kernelCache().Get(name, v, arch); err != nil {
				missing = append(missing, arch)
			}
		}
		if len(missing) > 0 {
			job := s.startKernelFetch(ctx, name, v, c.GetInt("user_id"), c.ClientIP())
			c.AbortWithStatusJSON(409, gin.H{"error": gin.H{"code": "kernel_not_cached", "message": fmt.Sprintf("%s %s 还缺 %s，已开始下载，完成后再设为默认", name, v, strings.Join(missing, "、"))}, "job": job})
			return
		}
		if name != "mihomo" {
			if _, _, err := s.drivers(ctx).Select(name, v); err != nil {
				c.AbortWithStatusJSON(409, gin.H{"error": gin.H{"code": "driver_missing", "message": err.Error()}})
				return
			}
		}
		value[name] = v
	}
	err := s.Store.Ent.Setting.Create().SetKey("kernel-defaults").SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx)
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	ids, err := s.Store.Ent.Gate.Query().IDs(ctx)
	if err == nil {
		err = s.bumpGates(ctx, ids)
	}
	if err != nil {
		failure(c, 500, "save_failed")
		return
	}
	s.audit(c, "kernel.defaults", "", "ok")
	c.JSON(200, gin.H{"defaults": value})
}

// ImportKernel 把本机文件导入内核缓存（无法访问 GitHub 的机器离线导入用）。
func ImportKernel(path string, cfg Config, kernel, version, arch, file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	dir := cfg.DataDir
	if dir == "" {
		dir = filepath.Join(filepath.Dir(path), "data")
	}
	return kernels.Cache{Dir: filepath.Join(dir, "kernels")}.Put(kernel, strings.TrimPrefix(version, "v"), arch, data)
}
