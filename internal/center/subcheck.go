package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 订阅检查：中心生成的 Clash 订阅交给 mihomo -t、sing-box 订阅交给 sing-box check，用对应内核检查能否被客户端加载。
// 检查不阻塞订阅输出：结果按内容摘要缓存，最近一次结果按订阅记下，在控制台提示。内核尚未下载或中心不是 Linux 时为「未知」。

type SubCheck struct {
	Status  string    `json:"status"` // passed / failed / unknown
	Kernel  string    `json:"kernel"`
	Version string    `json:"version,omitempty"`
	Error   string    `json:"error,omitempty"`
	At      time.Time `json:"at"`
	// 以下由记录时填写：哪个订阅、哪种格式。
	Ref    string `json:"ref,omitempty"`
	Name   string `json:"name,omitempty"`
	Target string `json:"target,omitempty"`
}

// checkKernel 返回某个订阅格式由哪个内核检查；其他格式不检查。
func checkKernel(target string) string {
	switch strings.ToLower(target) {
	case "clash", "mihomo", "clash-meta", "clashmeta", "meta", "stash":
		return "mihomo"
	case "singbox", "sing-box":
		return "sing-box"
	}
	return ""
}

// subCheckBinary 找到中心本机可运行的检查内核：使用默认版本（未设置时用缓存里最新的版本）。测试中替换。
var subCheckBinary = defaultSubCheckBinary

func defaultSubCheckBinary(s *Server, ctx context.Context, kernel string) (string, string, error) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return "", "", errors.New("中心不是 Linux amd64 / arm64，无法运行内核检查")
	}
	v := s.defaultKernelVersion(ctx, kernel, runtime.GOARCH)
	if v == "" {
		return "", "", errors.New(kernel + " 尚未下载，请先在内核页面拉取")
	}
	bin, _, err := s.kernelCache().Get(kernel, v, runtime.GOARCH)
	return bin, v, err
}

// subCheckSem 限制同时运行的检查数。
var subCheckSem = make(chan struct{}, 2)

// checkSubscription 检查一份生成的订阅；同一内容与内核版本 24 小时内只检查一次。
func (s *Server) checkSubscription(ctx context.Context, target string, body []byte) SubCheck {
	kernel := checkKernel(target)
	res := SubCheck{Status: "unknown", Kernel: kernel, At: time.Now().UTC()}
	if kernel == "" {
		res.Error = "这种格式不做内核检查"
		return res
	}
	bin, version, err := subCheckBinary(s, ctx, kernel)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Version = version
	sum := sha256.Sum256(body)
	key := "subcheck:" + kernel + ":" + version + ":" + hex.EncodeToString(sum[:])
	if raw, ok, _ := s.Store.KV.Get(ctx, key); ok && json.Unmarshal([]byte(raw), &res) == nil {
		return res
	}
	select {
	case subCheckSem <- struct{}{}:
		defer func() { <-subCheckSem }()
	case <-ctx.Done():
		res.Error = "检查被取消"
		return res
	}
	out, err := runSubCheck(ctx, s.dataDir(), kernel, bin, body)
	res.At = time.Now().UTC()
	switch {
	case err == nil:
		res.Status = "passed"
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		res.Error = "检查超时"
	default:
		res.Status, res.Error = "failed", tail(out, 2000)
		if res.Error == "" {
			res.Error = err.Error()
		}
	}
	if res.Status != "unknown" {
		if b, err := json.Marshal(res); err == nil {
			_ = s.Store.KV.Set(ctx, key, string(b), 24*time.Hour)
		}
	}
	return res
}

// runSubCheck 把订阅写到临时目录，用 mihomo -t / sing-box check 检查，限时 30 秒。
// mihomo 的工作目录固定在中心数据目录下（GEOIP 等数据只下载一次）。
func runSubCheck(ctx context.Context, dataDir, kernel, bin string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tmp, err := os.MkdirTemp("", "migate-subcheck-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	var cmd *exec.Cmd
	switch kernel {
	case "mihomo":
		file := filepath.Join(tmp, "config.yaml")
		if err = os.WriteFile(file, body, 0600); err != nil {
			return nil, err
		}
		home := filepath.Join(dataDir, "subcheck", "mihomo")
		if err = os.MkdirAll(home, 0700); err != nil {
			return nil, err
		}
		cmd = exec.CommandContext(ctx, bin, "-t", "-d", home, "-f", file)
	default:
		file := filepath.Join(tmp, "config.json")
		if err = os.WriteFile(file, body, 0600); err != nil {
			return nil, err
		}
		cmd = exec.CommandContext(ctx, bin, "check", "-c", file)
	}
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

func tail(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		s = "…" + s[len(s)-n:]
	}
	return s
}

// recordSubCheck 在后台检查一份已输出的订阅，并记下这个订阅最近一次的结果（保留 30 天）。
func (s *Server) recordSubCheck(ref, name, target string, body []byte) {
	if checkKernel(target) == "" {
		return
	}
	st := s.Store
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		s.state.RLock()
		ready := s.Store == st && st != nil
		s.state.RUnlock()
		if !ready {
			return
		}
		res := s.checkSubscription(ctx, target, body)
		res.Ref, res.Name, res.Target = ref, name, checkKernel(target)
		if b, err := json.Marshal(res); err == nil {
			_ = st.KV.Set(ctx, "subcheck-last:"+ref+":"+res.Target, string(b), 30*24*time.Hour)
		}
	}()
}

// subChecks 列出各订阅最近一次检查结果（失败的在前）与检查内核是否可用。
func (s *Server) subChecks(c *gin.Context) {
	ctx := c.Request.Context()
	keys, err := s.Store.KV.Keys(ctx, "subcheck-last:", 500)
	if err != nil {
		failure(c, 503, "query_failed")
		return
	}
	items := []SubCheck{}
	for _, k := range keys {
		raw, ok, err := s.Store.KV.Get(ctx, k)
		var v SubCheck
		if err == nil && ok && json.Unmarshal([]byte(raw), &v) == nil {
			items = append(items, v)
		}
	}
	rank := map[string]int{"failed": 0, "unknown": 1, "passed": 2}
	sort.SliceStable(items, func(i, j int) bool {
		if rank[items[i].Status] != rank[items[j].Status] {
			return rank[items[i].Status] < rank[items[j].Status]
		}
		return items[i].At.After(items[j].At)
	})
	kernels := gin.H{}
	for _, k := range []string{"mihomo", "sing-box"} {
		_, v, err := subCheckBinary(s, ctx, k)
		if err != nil {
			kernels[k] = gin.H{"available": false, "error": err.Error()}
		} else {
			kernels[k] = gin.H{"available": true, "version": v}
		}
	}
	c.JSON(200, gin.H{"kernels": kernels, "items": items})
}
