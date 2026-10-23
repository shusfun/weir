package center

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/imzyb/MiGate/internal/kernels"
	"github.com/imzyb/MiGate/internal/store/ent/setting"
)

// defaultMirrors 与 install-online.sh 一致；MIGATE_GITHUB_MIRRORS=off 关闭，或写逗号分隔的主机名替换。
func githubMirrors() []string {
	raw := strings.TrimSpace(os.Getenv("MIGATE_GITHUB_MIRRORS"))
	if raw == "off" {
		return nil
	}
	hosts := []string{"gh-proxy.com", "ghfast.top", "ghproxy.net", "gh.llkk.cc"}
	if raw != "" {
		hosts = strings.Split(raw, ",")
	}
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h = strings.TrimSpace(h); validHost(h) {
			out = append(out, "https://"+h)
		}
	}
	return out
}

func (s *Server) fetcher() kernels.Fetcher {
	return kernels.Fetcher{HTTP: &http.Client{Timeout: 10 * time.Minute}, Mirrors: githubMirrors(), Cache: s.kernelCache()}
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
	admin.GET("/kernels/:kernel/releases", s.kernelReleases)
	admin.POST("/kernels/:kernel/fetch", s.fetchKernel)
	admin.GET("/kernels/:kernel/fetch", s.fetchStatus)
	admin.PUT("/kernels/defaults", s.setKernelDefaults)
	admin.GET("/kernels/profiles", s.listProfiles)
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

func (s *Server) listKernels(c *gin.Context) {
	defaults := s.kernelDefaults(c.Request.Context())
	out := gin.H{}
	for _, name := range kernels.Names {
		out[name] = gin.H{"default": defaults[name], "cached": gin.H{"amd64": s.kernelCache().Versions(name, "amd64"), "arm64": s.kernelCache().Versions(name, "arm64")}}
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
	c.JSON(200, gin.H{"releases": list})
}

func (s *Server) fetchKernel(c *gin.Context) {
	name := c.Param("kernel")
	var req struct {
		Version string `json:"version"`
		Arch    string `json:"arch"`
	}
	if !validKernel(name) || !bind(c, &req) {
		if !c.IsAborted() {
			failure(c, 404, "kernel_not_found")
		}
		return
	}
	if req.Arch == "" {
		req.Arch = "amd64"
	}
	version := strings.TrimPrefix(req.Version, "v")
	if version == "" || len(version) > 64 || strings.ContainsAny(version, `/\ `) || req.Arch != "amd64" && req.Arch != "arm64" {
		failure(c, 400, "invalid_version")
		return
	}
	// 下载可能超过一分钟（经镜像回退、按官方摘要校验）：后台进行，页面轮询状态。
	key := "kernel-fetch:" + name + ":" + version + ":" + req.Arch
	ctx := c.Request.Context()
	if ok, err := s.Store.KV.SetNX(ctx, key, `{"status":"running"}`, 30*time.Minute); err != nil || !ok {
		if err != nil {
			failure(c, 503, "kernel_fetch_unavailable")
			return
		}
		raw, _, _ := s.Store.KV.Get(ctx, key)
		var st map[string]any
		_ = json.Unmarshal([]byte(raw), &st)
		if st["status"] == "running" {
			c.JSON(202, gin.H{"status": "running", "key": key})
			return
		}
		_ = s.Store.KV.Del(ctx, key) // 上次已结束：允许重新下载
		_ = s.Store.KV.Set(ctx, key, `{"status":"running"}`, 30*time.Minute)
	}
	actor, ip := c.GetInt("user_id"), c.ClientIP()
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		result := gin.H{"status": "done"}
		sum, err := s.fetcher().Fetch(bg, name, version, req.Arch)
		if err != nil {
			result = gin.H{"status": "failed", "error": err.Error()}
		} else {
			result["sha256"] = sum
		}
		b, _ := json.Marshal(result)
		_ = s.Store.KV.Set(bg, key, string(b), time.Hour)
		res := "ok"
		if err != nil {
			res = "failed"
		}
		_, _ = s.Store.Ent.Audit.Create().SetActor(actor).SetAction("kernel.fetch").SetTarget(name + " " + version + " " + req.Arch).SetResult(res).SetIP(ip).Save(bg)
	}()
	c.JSON(202, gin.H{"status": "running", "key": key})
}

// fetchStatus 返回某次内核下载的状态：running / done（含 sha256）/ failed（含原因）/ none。
func (s *Server) fetchStatus(c *gin.Context) {
	name := c.Param("kernel")
	key := "kernel-fetch:" + name + ":" + strings.TrimPrefix(c.Query("version"), "v") + ":" + c.DefaultQuery("arch", "amd64")
	raw, ok, err := s.Store.KV.Get(c.Request.Context(), key)
	if err != nil {
		failure(c, 503, "kernel_fetch_unavailable")
		return
	}
	if !ok {
		c.JSON(200, gin.H{"status": "none"})
		return
	}
	c.Data(200, "application/json", []byte(raw))
}

// setKernelDefaults 设置各内核的默认版本：没有单独锁定版本的 Gate 会按新版本重新下发配置。
func (s *Server) setKernelDefaults(c *gin.Context) {
	var req map[string]string
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	value := map[string]any{}
	for name, v := range req {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if !validKernel(name) || v == "" {
			continue
		}
		if name != "mihomo" {
			if _, _, err := s.kernelCache().Get(name, v, "amd64"); err != nil {
				failure(c, 409, "kernel_not_cached")
				return
			}
			if _, err := s.profiles().Select(name, v); err != nil {
				c.AbortWithStatusJSON(409, gin.H{"error": gin.H{"code": "profile_missing", "message": err.Error()}})
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

func (s *Server) listProfiles(c *gin.Context) {
	list, err := s.profiles().List()
	if err != nil {
		c.AbortWithStatusJSON(500, gin.H{"error": gin.H{"code": "profile_invalid", "message": err.Error()}})
		return
	}
	c.JSON(200, gin.H{"profiles": list})
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
