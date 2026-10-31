package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/kernels"
	"github.com/shusfun/weir/internal/selfupdate"
	"github.com/shusfun/weir/internal/store/ent/setting"
)

// 管理员通知：中心按当前状态算出要处理的问题，只给管理员看。问题解决后自然消失；
// 忽略按问题 id 记在该管理员名下，同一问题不再提示（问题变了 id 也变，会重新提示）。

// Notice 是一条通知。Action 是页面上的处理按钮。
type Notice struct {
	ID     string         `json:"id"`
	Level  string         `json:"level"` // warn / info
	Title  string         `json:"title"`
	Detail string         `json:"detail,omitempty"`
	Action *NoticeAction  `json:"action,omitempty"`
	Extra  map[string]any `json:"extra,omitempty"`
}

// NoticeAction：kind 为 fetch_kernel（下载推荐版本）、open（跳转页面）、update（打开版本更新）。
type NoticeAction struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Kernel  string `json:"kernel,omitempty"`
	Version string `json:"version,omitempty"`
	To      string `json:"to,omitempty"`
}

func (s *Server) noticeRoutes(admin *gin.RouterGroup) {
	admin.GET("/notices", s.listNotices)
	admin.POST("/notices/:id/dismiss", s.dismissNotice)
	admin.GET("/kernels/:kernel/recommended", s.recommendedHandler)
}

func dismissKey(uid int) string { return fmt.Sprintf("notice-dismissed:%d", uid) }

func (s *Server) dismissed(ctx context.Context, uid int) map[string]bool {
	out := map[string]bool{}
	if row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(dismissKey(uid))).Only(ctx); err == nil {
		for k := range row.Value {
			out[k] = true
		}
	}
	return out
}

func (s *Server) dismissNotice(c *gin.Context) {
	ctx := c.Request.Context()
	uid := c.GetInt("user_id")
	value := map[string]any{}
	if row, err := s.Store.Ent.Setting.Query().Where(setting.KeyEQ(dismissKey(uid))).Only(ctx); err == nil {
		value = row.Value
	}
	id := c.Param("id")
	if len(id) > 200 {
		failure(c, 400, "invalid_id")
		return
	}
	value[id] = time.Now().UTC().Format(time.RFC3339)
	if len(value) > 500 {
		value = map[string]any{id: value[id]}
	}
	if err := s.Store.Ent.Setting.Create().SetKey(dismissKey(uid)).SetValue(value).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		failInternal(c, 500, "save_failed", err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) listNotices(c *gin.Context) {
	ctx := c.Request.Context()
	skip := s.dismissed(ctx, c.GetInt("user_id"))
	all := s.notices(ctx)
	out := []Notice{}
	for _, n := range all {
		if !skip[n.ID] {
			out = append(out, n)
		}
	}
	c.JSON(200, gin.H{"notices": out, "dismissed": len(all) - len(out)})
}

// errChecking 表示访问 GitHub 的查询还在后台进行（没超过它自己的时限），稍后再看。
var errChecking = errors.New("正在向 GitHub 查询，稍后刷新")

// githubOnce 在后台（不受请求时限影响，最多 45 秒）运行一次访问 GitHub 的查询，同名查询同时只有一个；
// 调用方最多等到自己的时限，还没结束就得到 errChecking。查询的结果由各自的缓存保存，下次直接命中。
func (s *Server) githubOnce(ctx context.Context, key string, run func(context.Context) error) error {
	s.ghMu.Lock()
	if s.ghCalls == nil {
		s.ghCalls = map[string]chan error{}
	}
	ch, ok := s.ghCalls[key]
	if !ok {
		ch = make(chan error, 1)
		s.ghCalls[key] = ch
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			err := run(bg)
			s.ghMu.Lock()
			delete(s.ghCalls, key)
			s.ghMu.Unlock()
			ch <- err
			close(ch)
		}()
	}
	s.ghMu.Unlock()
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		return errChecking
	}
}

// notices 计算当前要处理的问题。
func (s *Server) notices(ctx context.Context) []Notice {
	var out []Notice
	d := s.drivers(ctx)
	// 通知列表要快：访问 GitHub 的查询在后台进行，这里最多等 10 秒；没等到就写「正在查询」，不当成失败。
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, kernel := range kernels.Names {
		for _, arch := range s.kernelArches(ctx, kernel) {
			if len(s.kernelCache().Versions(kernel, arch)) > 0 {
				continue
			}
			n := Notice{ID: "kernel-missing:" + kernel + ":" + arch, Level: "warn", Title: fmt.Sprintf("%s 还没有 %s 版本", kernelTitle(kernel), arch)}
			switch kernel {
			case "mihomo":
				n.Detail = "中心用它检查生成的 Clash 订阅；没有时订阅检查显示「未知」。"
			default:
				n.Detail = "建节点和节点实测都要用它；这个架构的 VPS 在下载完成前无法生效。"
			}
			var rec Recommendation
			err := s.githubOnce(ctx, "recommend:"+kernel, func(bg context.Context) error {
				var e error
				rec, e = s.recommended(bg, kernel)
				return e
			})
			if err == nil && rec.Version != "" {
				n.Action = &NoticeAction{Kind: "fetch_kernel", Label: "下载推荐版本 " + rec.Version, Kernel: kernel, Version: rec.Version}
				n.Extra = map[string]any{"reason": rec.Reason}
			} else {
				n.Action = &NoticeAction{Kind: "open", Label: "去内核页", To: "/kernels"}
				if err == errChecking {
					n.Extra = map[string]any{"reason": "正在查询推荐版本，稍后刷新"}
				} else if err != nil {
					n.Extra = map[string]any{"reason": "无法读取推荐版本：" + err.Error()}
				} else if rec.Reason != "" {
					n.Extra = map[string]any{"reason": rec.Reason}
				}
			}
			out = append(out, n)
		}
		if kernel == "mihomo" {
			continue
		}
		seen := map[string]bool{}
		for _, arch := range []string{"amd64", "arm64"} {
			for _, v := range s.kernelCache().Versions(kernel, arch) {
				if seen[v] {
					continue
				}
				seen[v] = true
				if _, _, err := d.Select(kernel, v); err != nil {
					out = append(out, Notice{ID: "driver-missing:" + kernel + ":" + v, Level: "warn", Title: fmt.Sprintf("%s %s 没有适用的驱动", kernelTitle(kernel), v),
						Detail: "这个版本不能设为默认，也不能给 VPS 用；可以在驱动页新增，或让 dsh 起草一份。", Action: &NoticeAction{Kind: "open", Label: "去驱动页", To: "/kernels?tab=drivers"}})
				}
			}
		}
	}
	if st, ok := selfupdate.ReadStatus(); ok && st.State == "failed" && st.Finished != nil && time.Since(*st.Finished) < 7*24*time.Hour {
		out = append(out, Notice{ID: "update-failed:" + st.ID, Level: "warn", Title: "上次更新没有成功", Detail: st.Error, Action: &NoticeAction{Kind: "update", Label: "查看"}})
	}
	var list []weirRelease
	err := s.githubOnce(ctx, "weir-releases", func(bg context.Context) error {
		var e error
		list, e = s.weirReleases(bg, false)
		return e
	})
	if err == nil && len(list) > 0 && buildinfo.Release() && selfupdate.Compare(list[0].Version, buildinfo.Version) > 0 {
		out = append(out, Notice{ID: "weir-update:" + list[0].Version, Level: "info", Title: "Weir 有新版本 " + list[0].Version, Detail: "当前 " + buildinfo.Version + "。", Action: &NoticeAction{Kind: "update", Label: "查看并更新"}})
	} else if err != nil && err != errChecking {
		out = append(out, Notice{ID: "github-unreachable:" + time.Now().UTC().Format("2006-01-02"), Level: "warn", Title: "连不上 GitHub", Detail: "无法检查新版本，内核下载会依次尝试镜像。" + err.Error()})
	}
	out = append(out, s.tasks.notices()...)
	if free, ok := diskFree(s.dataDir()); ok && free < 1<<30 {
		out = append(out, Notice{ID: fmt.Sprintf("disk-low:%d", free>>28), Level: "warn", Title: "数据目录剩余空间不足 1 GB", Detail: fmt.Sprintf("%s 还剩 %.1f GB；内核缓存与备份需要空间。", s.dataDir(), float64(free)/(1<<30))})
	}
	return out
}

// Recommendation 是推荐版本与理由。
type Recommendation struct {
	Version string `json:"version"`
	Reason  string `json:"reason"`
	Latest  string `json:"latest"`
	// LatestDriver 是最新版有没有适用驱动（没有时为空）。
	LatestDriver string `json:"latest_driver"`
}

const recommendAge = 14 * 24 * time.Hour

// recommended：只在「有适用驱动、样例配置能生成」的版本里选，比较发布满 14 天的版本，
// 按需要的架构的下载量排序，并避开驱动声明不兼容的搭配。
func (s *Server) recommended(ctx context.Context, kernel string) (Recommendation, error) {
	key := "kernel-recommend:" + kernel
	if raw, ok, _ := s.Store.KV.Get(ctx, key); ok {
		var r Recommendation
		if json.Unmarshal([]byte(raw), &r) == nil {
			return r, nil
		}
	}
	if msg, ok, _ := s.Store.KV.Get(ctx, key+":error"); ok {
		return Recommendation{}, errors.New(msg)
	}
	list, err := s.fetcher().Releases(ctx, kernel)
	if err != nil {
		_ = s.Store.KV.Set(ctx, key+":error", err.Error(), 10*time.Minute)
		return Recommendation{}, err
	}
	d := s.drivers(ctx)
	arches := s.kernelArches(ctx, kernel)
	var rec Recommendation
	if len(list) > 0 {
		rec.Latest = list[0].Version
		if drv, _, err := d.Select(kernel, list[0].Version); err == nil || kernel == "mihomo" {
			rec.LatestDriver = drv.Name
		}
	}
	type cand struct {
		v         string
		downloads int64
	}
	var cands []cand
	for _, r := range list {
		if time.Since(r.PublishedAt) < recommendAge {
			continue
		}
		if kernel != "mihomo" {
			drv, full, err := d.Select(kernel, r.Version)
			if err != nil || !sampleRenders(drv, full) || s.incompatible(ctx, drv, kernel, r.Version) != "" {
				continue
			}
		}
		var n int64
		for _, a := range arches {
			n += r.Downloads[a]
		}
		cands = append(cands, cand{r.Version, n})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].downloads > cands[j].downloads })
	if len(cands) == 0 && len(list) > 0 {
		rec.Reason = "没有发布满 14 天、有适用驱动的版本，请在内核页手动选择"
	}
	if len(cands) > 0 {
		rec.Version = cands[0].v
		rec.Reason = fmt.Sprintf("发布满 14 天、有适用驱动的版本里下载量最高（%s：%d 次）", strings.Join(arches, "+"), cands[0].downloads)
		if kernel == "sing-box" {
			rec.Reason += "；下载量取上游 SagerNet/sing-box"
		}
	}
	if b, err := json.Marshal(rec); err == nil {
		_ = s.Store.KV.Set(ctx, key, string(b), time.Hour)
	}
	return rec, nil
}

// incompatible 检查与另一个内核正在用的版本是否有驱动声明的不兼容。
func (s *Server) incompatible(ctx context.Context, drv kernelconf.Driver, kernel, version string) string {
	d := s.drivers(ctx)
	other := "sing-box"
	if kernel == "sing-box" {
		other = "xray"
	}
	ov := s.explicitDefault(ctx, other)
	if ov == "" {
		ov = s.newestAnyArch(other)
	}
	if ov == "" {
		return ""
	}
	for _, p := range []string{"vless", "vmess", "trojan"} {
		if why := drv.Incompatible(other, ov, p, "reality"); why != "" {
			return why
		}
	}
	if od, _, err := d.Select(other, ov); err == nil {
		for _, p := range []string{"vless", "vmess", "trojan"} {
			if why := od.Incompatible(kernel, version, p, "reality"); why != "" {
				return why
			}
		}
	}
	return ""
}

// sampleRenders：驱动的每个方案都能生成样例配置（不调用内核程序）。
func sampleRenders(drv kernelconf.Driver, full string) bool {
	if len(drv.Presets) == 0 {
		return false
	}
	for _, p := range drv.Presets {
		if _, err := samplePreset(drv, full, Preset{Preset: p, Kernel: drv.Kernel}, "/weir-sample-certs"); err != nil {
			return false
		}
	}
	return true
}

func (s *Server) recommendedHandler(c *gin.Context) {
	kernel := c.Param("kernel")
	if !validKernel(kernel) {
		failure(c, 404, "kernel_not_found")
		return
	}
	rec, err := s.recommended(c.Request.Context(), kernel)
	if err != nil {
		failDetail(c, 502, "upstream_unavailable", "无法读取上游版本："+err.Error(), nil)
		return
	}
	c.JSON(200, gin.H{"recommended": rec, "arch": runtime.GOARCH})
}
