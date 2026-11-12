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

	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/publicnet"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/setting"
)

// githubHTTP 访问 GitHub 与镜像：不设整体时限（大文件下载由 fetch 的无数据超时控制），只限制等响应头的时间。
func githubHTTP() *http.Client {
	return publicnet.Client(0)
}

// fetcher 是列版本、读缓存用的 Fetcher（只访问 GitHub API）；下载用 kernelFetcher（按「GitHub 加速」的来源顺序）。
func (s *Server) fetcher() kernels.Fetcher {
	if g := s.GitHub; g != nil {
		return kernels.Fetcher{HTTP: g.HTTP, API: g.API, DownloadHost: g.Host, Cache: s.kernelCache()}
	}
	return kernels.Fetcher{Query: s.queryReleases, HTTP: githubHTTP(), Cache: s.kernelCache()}
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

// kernelArches 包含中心本机与已接入 Gate 的架构，mihomo 同时用于中心和 VPS 的临时实测。
// 中心不是 Linux 时（开发环境）只按 Gate 的架构算，没有 Gate 时用 amd64。
func (s *Server) kernelArches(ctx context.Context, kernel string) []string {
	set := map[string]bool{}
	if runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		set[runtime.GOARCH] = true
	}
	if s.Store != nil {
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
		versions, err := s.kernelVersions(ctx, name)
		if err != nil {
			failInternal(c, 503, "kernel_catalog_unavailable", err)
			return
		}
		list := make([]gin.H, 0, len(versions))
		for _, v := range versions {
			have := gin.H{}
			binaries := gin.H{}
			for _, arch := range arches {
				_, _, err := cache.Get(name, v, arch)
				binaries[arch] = err == nil
				have[arch] = s.kernelPrepared(ctx, name, v, arch) == nil
			}
			pinned, running := s.kernelUsers(ctx, name, v)
			item := gin.H{"version": v, "arches": have, "default": defaults[name] == v, "pinned_by": refNames(pinned), "running_on": refNames(running), "pinned_refs": nonNilRefs(pinned), "running_refs": nonNilRefs(running)}
			item["center_binaries"] = binaries
			if validKernel(name) {
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
		_ = c.Error(err)
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
		if validKernel(name) {
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

var mihomoCheckConfig = []byte(`{"mode":"rule","allow-lan":false,"bind-address":"127.0.0.1","proxies":[],"rules":["MATCH,DIRECT"]}`)

// startKernelFetch 在后台下载一个版本需要的全部架构（已缓存的跳过），完成后让 Gate 重新编译配置。
// 下载成功后：管理员要求「下载并设为默认」的设为默认；否则这个内核只有这一个版本且没有默认时自动设为默认。
func (s *Server) startKernelFetch(ctx context.Context, kernel, version string, actor int, ip string) Job {
	arches := s.kernelArches(ctx, kernel)
	title := kernelTitle(kernel) + " " + version
	return s.jobs.Start(kernelJobID(kernel, version), "kernel", title, func(bg context.Context, run JobRun) (map[string]any, error) {
		f, err := s.kernelFetcher(bg)
		if err != nil {
			return nil, err
		}
		if _, _, err = s.drivers(bg).Select(kernel, version); err != nil {
			return nil, err
		}
		f.Progress, f.Step, f.Log = run.Progress, run.Stage, run.Log
		sums := map[string]any{}
		for i, arch := range arches {
			required, e := s.kernelBinaryRequired(bg, kernel, arch)
			if e != nil {
				err = e
				break
			}
			if !required {
				run.Step("固定发行来源与摘要 " + arch)
				artifact, e := f.ResolveArtifact(bg, kernel, version, arch)
				if e == nil {
					e = s.saveKernelArtifact(bg, artifact)
				}
				if e != nil {
					err = e
					break
				}
				sums[arch] = artifact.SHA256
				run.Log(arch + " 发行档案已固定，VPS 自行下载并校验")
				continue
			}
			_, sum, cachedErr := f.Cache.Get(kernel, version, arch)
			if cachedErr == nil {
				run.Log(arch + " 已下载，跳过")
			} else {
				if len(arches) > 1 {
					run.Step(fmt.Sprintf("下载 %s（%d/%d）", arch, i+1, len(arches)))
				} else {
					run.Step("下载 " + arch)
				}
				artifact, e := f.ResolveArtifact(bg, kernel, version, arch)
				if e == nil {
					e = s.saveKernelArtifact(bg, artifact)
				}
				if e != nil {
					err = e
					break
				}
				if sum, err = f.FetchArtifact(bg, artifact); err != nil {
					break
				}
			}
			if kernel == "mihomo" && runtime.GOOS == "linux" && arch == runtime.GOARCH {
				var bin string
				bin, _, err = f.Cache.Get(kernel, version, arch)
				if err == nil {
					driver, _, e := s.drivers(bg).Select(kernel, version)
					err = e
					if err == nil {
						var dir string
						dir, err = os.MkdirTemp("", "weir-kernel-check-")
						if err == nil {
							output, e := runCheck(bg, bin, driver, mihomoCheckConfig, dir, "client")
							_ = os.RemoveAll(dir)
							if e != nil {
								err = fmt.Errorf("mihomo 驱动检查失败：%s", safeCheckOutput(output))
							}
						}
					}
				}
				if err != nil {
					_ = f.Cache.RemoveArch(kernel, version, arch)
					break
				}
			}
			sums[arch] = sum
			run.Log(arch + " 已校验并缓存，SHA-256 " + sum[:16] + "…")
		}
		res := "ok"
		if err != nil {
			res = "failed"
		}
		_, wantDefault := s.kernelDefaultAfter.LoadAndDelete(kernelJobID(kernel, version))
		if s.Store != nil {
			_, _ = s.Store.Ent.Audit.Create().SetActor(actor).SetAction("kernel.fetch").SetTarget(kernel + " " + version + " " + strings.Join(arches, ",")).SetResult(res).SetIP(ip).Save(context.Background())
			if err == nil {
				if ids, e := s.Store.Ent.Gate.Query().IDs(context.Background()); e == nil {
					_ = s.bumpGates(context.Background(), ids)
				}
				if wantDefault {
					if e := s.saveKernelDefault(context.Background(), kernel, version, actor, ip, "下载完成，按管理员要求设为默认"); e != nil {
						run.Log("没能设为默认：" + e.Error())
					} else {
						run.Log("已设为默认版本")
					}
				} else if s.ensureKernelDefault(context.Background(), kernel) {
					run.Log("这是唯一的版本，已自动设为默认")
				}
			}
		}
		if err == nil {
			go s.resumePendingNodeProbes(context.WithoutCancel(bg))
			go s.resumePendingExits(context.WithoutCancel(bg))
		}
		return map[string]any{"sha256": sums, "arches": arches}, err
	})
}

// autoKernelFetch 是编译 Gate 配置时发现缺内核而自动发起的下载；上次自动下载失败不到 10 分钟时不重试，
// 避免每次心跳都去请求 GitHub。管理员在内核页手动下载不受此限制。
func (s *Server) autoKernelFetch(ctx context.Context, kernel, version string) {
	if version == "" {
		id := kernelJobID(kernel, "latest")
		if j, ok := s.jobs.Get(id); ok && j.Status == jobFailed && j.Finished != nil && time.Since(*j.Finished) < 10*time.Minute {
			return
		}
		s.jobs.Start(id, "kernel", "正在查询 "+kernelTitle(kernel)+" 最新稳定版", func(bg context.Context, run JobRun) (map[string]any, error) {
			run.Step("查询最新稳定版")
			f, err := s.kernelFetcher(bg)
			if err != nil {
				return nil, err
			}
			releases, err := f.Releases(bg, kernel)
			if err != nil {
				return nil, err
			}
			if len(releases) == 0 {
				return nil, fmt.Errorf("%s 没有可下载的稳定版", kernel)
			}
			latest := releases[0].Version
			if _, _, err := s.drivers(bg).Select(kernel, latest); err != nil {
				return nil, err
			}
			j := s.startKernelFetch(bg, kernel, latest, 0, "")
			run.Step("正在下载并校验 " + latest)
			result, ok := s.jobs.Wait(bg, j.ID)
			if bg.Err() != nil {
				return nil, bg.Err()
			}
			if !ok || result.Status != jobDone {
				return nil, fmt.Errorf("%s 下载未完成：%s", kernel, result.Error)
			}
			return map[string]any{"version": latest, "job_id": j.ID}, nil
		})
		return
	}

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
		// SetDefault：下载成功后设为默认（内核页的更新建议用）；已下载的版本不用走这里。
		SetDefault bool `json:"set_default"`
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
	if dryRun(c, dryResult{Preview: []string{"开始下载 " + name + " " + version + "（按官方摘要校验）；进度在「内核 → 版本」里看"}}) {
		return
	}
	if req.SetDefault {
		if validKernel(name) {
			if _, _, err := s.drivers(c.Request.Context()).Select(name, version); err != nil {
				c.AbortWithStatusJSON(409, gin.H{"error": gin.H{"code": "driver_missing", "message": err.Error()}})
				return
			}
		}
		s.kernelDefaultAfter.Store(kernelJobID(name, version), true)
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
	if _, err := s.Store.Ent.Resource.Delete().Where(resource.KindEQ(kernelArtifactKind), resource.KeyHasPrefix(name+":"+version+":")).Exec(ctx); err != nil {
		failInternal(c, 503, "kernel_catalog_unavailable", err)
		return
	}
	s.audit(c, "kernel.remove", name+" "+version, "ok")
	s.ensureKernelDefault(ctx, name)
	c.JSON(200, gin.H{"ok": true})
}

// saveKernelDefault 把一个已下载的版本设为默认并让 Gate 重新生成配置（未锁定版本的 VPS 随之切换）。
func (s *Server) saveKernelDefault(ctx context.Context, kernel, version string, actor int, ip, why string) error {
	if validKernel(kernel) {
		if _, _, err := s.drivers(ctx).Select(kernel, version); err != nil {
			return err
		}
	}
	for _, arch := range s.kernelArches(ctx, kernel) {
		if err := s.kernelPrepared(ctx, kernel, version, arch); err != nil {
			return fmt.Errorf("还缺 %s", arch)
		}
	}
	value := map[string]any{}
	for k, v := range s.kernelDefaults(ctx) {
		value[k] = v
	}
	value[kernel] = version
	if err := s.Store.Ent.Setting.Create().SetKey("kernel-defaults").SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		return err
	}
	ids, err := s.Store.Ent.Gate.Query().IDs(ctx)
	if err == nil {
		err = s.bumpGates(ctx, ids)
	}
	_, _ = s.Store.Ent.Audit.Create().SetActor(actor).SetAction("kernel.defaults").SetTarget(kernel + " " + version + "：" + why).SetResult("ok").SetIP(ip).Save(ctx)
	return err
}

// ensureKernelDefault：内核没有默认版本、缓存里只有一个版本、架构齐全且有驱动时，自动把它设为默认，返回是否设了。
// 有多个版本时由管理员选；已有默认绝不改动。
func (s *Server) ensureKernelDefault(ctx context.Context, kernel string) bool {
	if s.explicitDefault(ctx, kernel) != "" {
		return false
	}
	versions, err := s.kernelVersions(ctx, kernel)
	if err != nil || len(versions) != 1 {
		return false
	}
	return s.saveKernelDefault(ctx, kernel, versions[0], 0, "", "唯一的版本，自动设为默认") == nil
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
			if err := s.kernelPrepared(ctx, name, v, arch); err != nil {
				missing = append(missing, arch)
			}
		}
		if len(missing) > 0 {
			job := s.startKernelFetch(ctx, name, v, c.GetInt("user_id"), c.ClientIP())
			c.AbortWithStatusJSON(409, gin.H{"error": gin.H{"code": "kernel_not_cached", "message": fmt.Sprintf("%s %s 还缺 %s，已开始下载，完成后再设为默认", name, v, strings.Join(missing, "、"))}, "job": job})
			return
		}
		if validKernel(name) {
			if _, _, err := s.drivers(ctx).Select(name, v); err != nil {
				c.AbortWithStatusJSON(409, gin.H{"error": gin.H{"code": "driver_missing", "message": err.Error()}})
				return
			}
		}
		value[name] = v
	}
	err := s.Store.Ent.Setting.Create().SetKey("kernel-defaults").SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx)
	if err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	ids, err := s.Store.Ent.Gate.Query().IDs(ctx)
	if err == nil {
		err = s.bumpGates(ctx, ids)
	}
	if err != nil {
		failInternal(c, 500, "save_failed", err)
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
